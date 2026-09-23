// Package currencyrates maintains official Bank of Russia exchange rates.
package currencyrates

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
)

// Collection is the read-only collection containing exchange rate history.
const Collection = "currency_rates"

const collectionID = "pmfo_curr_rates"
const jobID = "currencyrates_update"
const requestTimeout = 15 * time.Second

type internalKey struct{}

func internalContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, internalKey{}, true)
}

func isInternal(ctx context.Context) bool {
	return ctx != nil && ctx.Value(internalKey{}) == true
}

// Plugin fetches rates and stores them in its application's database.
type Plugin struct {
	app    core.App
	client *http.Client
	mu     sync.Mutex
}

// Register installs the collection at bootstrap and updates it at server startup
// and daily at 03:00 in the app cron timezone (UTC by default, 06:00 Moscow).
// Call before Bootstrap/Start. Repeated registration reuses the same plugin.
func Register(app core.App) *Plugin {
	return register(app, &http.Client{Timeout: requestTimeout})
}

func register(app core.App, client *http.Client) *Plugin {
	const storeKey = "currencyrates.plugin"
	if existing, ok := app.Store().Get(storeKey).(*Plugin); ok {
		return existing
	}
	p := &Plugin{app: app, client: client}
	app.Store().Set(storeKey, p)
	app.OnBootstrap().Bind(&hook.Handler[*core.BootstrapEvent]{Id: "currencyrates", Func: func(e *core.BootstrapEvent) error {
		if err := e.Next(); err != nil {
			return err
		}
		return install(e.App)
	}})
	protectRecord := func(e *core.RecordEvent) error {
		if e.Record.Collection().Name == Collection && !isInternal(e.Context) {
			return fmt.Errorf("currencyrates: records are managed by the Bank of Russia importer")
		}
		return e.Next()
	}
	app.OnRecordCreate().Bind(&hook.Handler[*core.RecordEvent]{Id: "currencyrates", Func: protectRecord})
	app.OnRecordUpdate().Bind(&hook.Handler[*core.RecordEvent]{Id: "currencyrates", Func: protectRecord})
	app.OnRecordDelete().Bind(&hook.Handler[*core.RecordEvent]{Id: "currencyrates", Func: protectRecord})
	protectCollection := func(e *core.CollectionEvent) error {
		if (e.Collection.Name == Collection || e.Collection.Id == collectionID) && !isInternal(e.Context) {
			return fmt.Errorf("currencyrates: reserved service collection")
		}
		return e.Next()
	}
	app.OnCollectionCreate().Bind(&hook.Handler[*core.CollectionEvent]{Id: "currencyrates", Func: protectCollection})
	app.OnCollectionUpdate().Bind(&hook.Handler[*core.CollectionEvent]{Id: "currencyrates", Func: protectCollection})
	app.OnCollectionDelete().Bind(&hook.Handler[*core.CollectionEvent]{Id: "currencyrates", Func: protectCollection})
	app.OnServe().Bind(&hook.Handler[*core.ServeEvent]{Id: "currencyrates", Func: func(e *core.ServeEvent) error {
		run := func() {
			if err := p.Update(context.Background()); err != nil {
				e.App.Logger().Error("currencyrates: update failed", "error", err)
			}
		}
		if err := e.App.Cron().Add(jobID, "0 3 * * *", run); err != nil {
			return err
		}
		run() // A source outage must not prevent the server from starting.
		return e.Next()
	}})
	return p
}

// Update imports the rates effective today in Moscow. Repeated responses update
// the same currency/date records. Cleanup runs even when the source is down.
// Fetch/validation failures leave retained history unchanged.
func (p *Plugin) Update(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.updateAt(ctx, time.Now())
}

func (p *Plugin) updateAt(ctx context.Context, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	snapshot, fetchErr := fetch(ctx, p.client, calendarDate(now))
	if fetchErr == nil {
		fetchErr = store(ctx, p.app, snapshot, now)
	}
	// Independent of the network timeout, so unavailable CBR cannot extend retention.
	_, cleanupErr := cleanupAt(p.app, now)
	return errors.Join(fetchErr, cleanupErr)
}

var moscow = time.FixedZone("Europe/Moscow", 3*60*60)

// Calendar dates are stored as UTC midnight labels, not Moscow instants.
func calendarDate(now time.Time) time.Time {
	y, m, d := now.In(moscow).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func retentionCutoff(now time.Time) time.Time {
	day := calendarDate(now)
	y, m, d := day.Date()
	// Clamp February 29 to February 28 in a non-leap target year.
	lastDay := time.Date(y-5, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
	return time.Date(y-5, m, min(d, lastDay), 0, 0, 0, 0, time.UTC)
}
