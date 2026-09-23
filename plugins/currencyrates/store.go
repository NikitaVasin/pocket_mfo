package currencyrates

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

func install(app core.App) error {
	return app.RunInTransaction(func(tx core.App) error {
		c, err := tx.FindCollectionByNameOrId(Collection)
		if err == nil {
			if c.Id != collectionID || !c.System || !c.IsBase() {
				return fmt.Errorf("currencyrates: collection %s already exists with an incompatible schema", Collection)
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		c = core.NewBaseCollection(Collection, collectionID)
		c.System = true
		c.ListRule = types.Pointer("")
		c.ViewRule = types.Pointer("")
		c.Fields.Add(
			&core.DateField{Name: "date", Required: true, Help: "Дата действия курса ЦБ РФ (UTC midnight)."},
			&core.TextField{Name: "currency", Required: true, Pattern: `^[A-Z]{3}$`, Presentable: true},
			&core.TextField{Name: "numCode", Required: true, Pattern: `^[0-9]{3}$`},
			&core.TextField{Name: "cbrId", Required: true},
			&core.TextField{Name: "name", Required: true},
			&core.NumberField{Name: "nominal", Required: true, OnlyInt: true, Min: types.Pointer(1.0)},
			&core.NumberField{Name: "value", Required: true, Min: types.Pointer(0.0), Help: "Рублей за номинал валюты."},
			&core.NumberField{Name: "rate", Required: true, Min: types.Pointer(0.0), Help: "Рублей за одну единицу валюты: value / nominal."},
		)
		c.AddIndex("idx_currency_rates_date_currency", true, "date, currency", "")
		return tx.SaveWithContext(internalContext(context.Background()), c)
	})
}

func store(ctx context.Context, app core.App, snapshot dailyRates, now time.Time) error {
	if snapshot.Date.Before(retentionCutoff(now)) || snapshot.Date.After(calendarDate(now)) {
		return fmt.Errorf("currencyrates: effective date outside the retention/current date range")
	}
	return app.RunInTransaction(func(tx core.App) error {
		c, err := tx.FindCollectionByNameOrId(Collection)
		if err != nil {
			return err
		}
		date := types.DateTime{}
		if err := date.Scan(snapshot.Date); err != nil {
			return err
		}
		for _, item := range snapshot.Rates {
			if err := ctx.Err(); err != nil {
				return err
			}
			r, err := tx.FindFirstRecordByFilter(c, "date = {:date} && currency = {:currency}", dbx.Params{"date": date.String(), "currency": item.Currency})
			if errors.Is(err, sql.ErrNoRows) {
				r = core.NewRecord(c)
			} else if err != nil {
				return err
			}
			r.Set("date", date)
			r.Set("currency", item.Currency)
			r.Set("numCode", item.NumCode)
			r.Set("cbrId", item.CBRID)
			r.Set("name", item.Name)
			r.Set("nominal", item.Nominal)
			r.Set("value", item.Value)
			r.Set("rate", item.Value/float64(item.Nominal))
			if err := tx.SaveWithContext(internalContext(ctx), r); err != nil {
				return fmt.Errorf("currencyrates: save %s: %w", item.Currency, err)
			}
		}
		return nil
	})
}

// Cleanup deletes rates whose effective date is older than five calendar years.
// Rates exactly on the boundary remain. The operation is atomic.
func Cleanup(app core.App) (int, error) {
	return cleanupAt(app, time.Now())
}

func cleanupAt(app core.App, now time.Time) (int, error) {
	count := 0
	err := app.RunInTransaction(func(tx core.App) error {
		for {
			records, err := tx.FindRecordsByFilter(Collection, "date < {:cutoff}", "date,id", 500, 0,
				dbx.Params{"cutoff": retentionCutoff(now).Format("2006-01-02 15:04:05.000Z")})
			if err != nil {
				return err
			}
			for _, r := range records {
				if err := tx.DeleteWithContext(internalContext(context.Background()), r); err != nil {
					return err
				}
				count++
			}
			if len(records) < 500 {
				return nil
			}
		}
	})
	if err != nil {
		return 0, fmt.Errorf("currencyrates: cleanup: %w", err)
	}
	return count, nil
}
