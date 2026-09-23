package currencyrates

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NikitaVasin/pocket_mfo/plugins/schemalock"
	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/hook"
	"golang.org/x/text/encoding/charmap"
)

const sampleXML = `<?xml version="1.0" encoding="UTF-8"?>
<ValCurs Date="23.09.2026" name="Foreign Currency Market">
<Valute ID="R01235"><NumCode>840</NumCode><CharCode>USD</CharCode><Nominal>1</Nominal><Name>Доллар США</Name><Value>93,1234</Value></Valute>
<Valute ID="R01820"><NumCode>392</NumCode><CharCode>JPY</CharCode><Nominal>100</Nominal><Name>Японских иен</Name><Value>62,5000</Value></Valute>
</ValCurs>`

var testNow = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T, body *string, locked bool) (*pocketbase.PocketBase, *Plugin) {
	t.Helper()
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.UserAgent() != "PocketMFO-CurrencyRates (+https://github.com/NikitaVasin/pocket_mfo)" {
			return nil, fmt.Errorf("missing importer User-Agent required by CBR")
		}
		if r.URL.Host != "www.cbr.ru" || r.URL.Path != "/scripts/XML_daily.asp" || r.URL.Query().Get("date_req") == "" {
			return nil, fmt.Errorf("unexpected URL: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(*body)), Header: make(http.Header)}, nil
	})}
	p := register(app, client)
	if Register(app) != p {
		t.Fatal("registration is not idempotent")
	}
	if locked {
		schemalock.Register(app)
	}
	must(t, app.Bootstrap())
	t.Cleanup(func() { app.Cron().Stop(); _ = app.ClearBootstrap() })
	return app, p
}

func rows(t *testing.T, app core.App) []*core.Record {
	t.Helper()
	r, err := app.FindAllRecords(Collection)
	must(t, err)
	return r
}

func TestImportNominalsIdempotencyAndEffectiveDate(t *testing.T) {
	body := sampleXML
	app, p := fixture(t, &body, true)
	must(t, p.updateAt(context.Background(), testNow))
	first := rows(t, app)
	if len(first) != 2 {
		t.Fatalf("got %d rows", len(first))
	}
	ids := map[string]string{}
	for _, r := range first {
		ids[r.GetString("currency")] = r.Id
		if r.GetDateTime("date").Time().Format("2006-01-02") != "2026-09-23" {
			t.Fatal("incorrect effective date")
		}
		if r.GetString("currency") == "JPY" && (r.GetFloat("rate") != 0.625 || r.GetInt("nominal") != 100 || r.GetFloat("value") != 62.5) {
			t.Fatal("nominal not applied")
		}
	}
	// A weekend response reuses the CBR effective date, not the request date.
	body = strings.ReplaceAll(body, "93,1234", "94,0000")
	must(t, p.updateAt(context.Background(), testNow.AddDate(0, 0, 1)))
	if got := rows(t, app); len(got) != 2 {
		t.Fatal("duplicate effective date")
	} else {
		for _, r := range got {
			if r.Id != ids[r.GetString("currency")] {
				t.Fatal("record ID changed")
			}
			if r.GetString("currency") == "USD" && r.GetFloat("rate") != 94 {
				t.Fatal("correction not imported")
			}
		}
	}
	body = strings.ReplaceAll(body, "23.09.2026", "24.09.2026")
	must(t, p.updateAt(context.Background(), testNow.AddDate(0, 0, 1)))
	if len(rows(t, app)) != 4 {
		t.Fatal("previous history lost")
	}
	must(t, install(app))
	if len(rows(t, app)) != 4 {
		t.Fatal("reinstall lost data")
	}
}

func TestParserValidatesWholeDocumentAndWindows1251(t *testing.T) {
	cp1251, err := charmap.Windows1251.NewEncoder().Bytes([]byte(strings.ReplaceAll(sampleXML, "UTF-8", "windows-1251")))
	must(t, err)
	parsed, err := parse(cp1251)
	must(t, err)
	if parsed.Rates[0].Name != "Доллар США" {
		t.Fatal("incorrect Cyrillic decoding")
	}
	for name, body := range map[string]string{
		"html":            "<html>unavailable</html>",
		"empty":           `<ValCurs Date="23.09.2026"/>`,
		"date":            strings.ReplaceAll(sampleXML, "23.09.2026", "31.02.2026"),
		"duplicate":       strings.ReplaceAll(sampleXML, "JPY", "USD"),
		"nominal":         strings.ReplaceAll(sampleXML, "<Nominal>100</Nominal>", "<Nominal>0</Nominal>"),
		"negative":        strings.ReplaceAll(sampleXML, "62,5000", "-62,5000"),
		"nan":             strings.ReplaceAll(sampleXML, "62,5000", "NaN"),
		"infinite":        strings.ReplaceAll(sampleXML, "62,5000", "Inf"),
		"code":            strings.ReplaceAll(sampleXML, "JPY", "jpy"),
		"truncated":       strings.TrimSuffix(sampleXML, "</ValCurs>"),
		"trailing":        sampleXML + "<broken",
		"second document": sampleXML + sampleXML,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parse([]byte(body)); err == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
}

func TestFetchFailureModesAndMoscowRequestDate(t *testing.T) {
	for _, kind := range []string{"status", "oversize", "network", "cancellation"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Query().Get("date_req") != "24/09/2026" {
					t.Error("request does not use Moscow calendar date")
				}
				if kind == "network" {
					return nil, errors.New("offline")
				}
				if kind == "cancellation" {
					<-r.Context().Done()
					return nil, r.Context().Err()
				}
				status, body := 503, "unavailable"
				if kind == "oversize" {
					status, body = 200, strings.Repeat("x", maxResponseSize+1)
				}
				return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			if kind == "cancellation" {
				cancel()
			}
			if _, err := fetch(ctx, client, calendarDate(testNow.Add(10*time.Hour))); err == nil {
				t.Fatal("fetch failure ignored")
			}
		})
	}
}

func TestImportRollsBackAndRejectsMalformedFutureAndExpiredData(t *testing.T) {
	body := sampleXML
	app, p := fixture(t, &body, false)
	must(t, p.updateAt(context.Background(), testNow))
	app.OnRecordUpdate(Collection).Bind(&hook.Handler[*core.RecordEvent]{Id: "fail", Func: func(e *core.RecordEvent) error {
		if e.Record.GetString("currency") == "JPY" {
			return errors.New("injected failure")
		}
		return e.Next()
	}})
	body = strings.ReplaceAll(sampleXML, "93,1234", "99,0000")
	if err := p.updateAt(context.Background(), testNow); err == nil {
		t.Fatal("failure ignored")
	}
	app.OnRecordUpdate().Unbind("fail")
	for _, r := range rows(t, app) {
		if r.GetString("currency") == "USD" && r.GetFloat("value") != 93.1234 {
			t.Fatal("partial import committed")
		}
	}
	for _, invalid := range []string{
		strings.ReplaceAll(sampleXML, "62,5000", "NaN"),
		strings.ReplaceAll(sampleXML, "23.09.2026", "24.09.2026"),
		strings.ReplaceAll(sampleXML, "23.09.2026", "22.09.2021"),
	} {
		body = invalid
		if err := p.updateAt(context.Background(), testNow); err == nil {
			t.Fatal("invalid import accepted")
		}
		if len(rows(t, app)) != 2 {
			t.Fatal("invalid response changed history")
		}
	}
}

func TestRetentionBoundaryOfflineCleanupAndRollback(t *testing.T) {
	body := sampleXML
	app, p := fixture(t, &body, false)
	for _, day := range []string{"22.09.2021", "23.09.2021", "24.09.2021"} {
		snapshot, err := parse([]byte(strings.ReplaceAll(sampleXML, "23.09.2026", day)))
		must(t, err)
		must(t, store(context.Background(), app, snapshot, snapshot.Date))
	}
	deletes := 0
	app.OnRecordDelete(Collection).Bind(&hook.Handler[*core.RecordEvent]{Id: "fail", Func: func(e *core.RecordEvent) error {
		deletes++
		if deletes == 2 {
			return errors.New("injected delete failure")
		}
		return e.Next()
	}})
	if n, err := cleanupAt(app, testNow); err == nil || n != 0 {
		t.Fatal("cleanup failure ignored")
	}
	if len(rows(t, app)) != 6 {
		t.Fatal("cleanup partially committed")
	}
	app.OnRecordDelete().Unbind("fail")
	body = "source is down"
	if err := p.updateAt(context.Background(), testNow); err == nil {
		t.Fatal("outage not reported")
	}
	if len(rows(t, app)) != 4 {
		t.Fatal("outage prevented cleanup or boundary removed")
	}
	for _, r := range rows(t, app) {
		if r.GetDateTime("date").Time().Before(retentionCutoff(testNow)) {
			t.Fatal("expired row remains")
		}
	}
	if got := retentionCutoff(time.Date(2028, 2, 29, 12, 0, 0, 0, time.UTC)).Format("2006-01-02"); got != "2023-02-28" {
		t.Fatalf("leap boundary: %s", got)
	}
}

func TestServeScheduleAndReadOnlyAPI(t *testing.T) {
	for _, locked := range []bool{false, true} {
		t.Run(fmt.Sprint("schemalock=", locked), func(t *testing.T) {
			body := strings.ReplaceAll(sampleXML, "23.09.2026", calendarDate(time.Now()).Format("02.01.2006"))
			app, _ := fixture(t, &body, locked)
			tokens := []string{""}
			for _, name := range []string{"members", core.CollectionNameSuperusers} {
				c, err := app.FindCollectionByNameOrId(name)
				if name == "members" {
					c = core.NewAuthCollection(name)
					err = app.Save(c)
				}
				must(t, err)
				r := core.NewRecord(c)
				r.SetEmail("reader@example.test")
				r.SetPassword("test-password-123")
				must(t, app.Save(r))
				token, err := r.NewAuthToken()
				must(t, err)
				tokens = append(tokens, token)
			}
			router, err := apis.NewRouter(app)
			must(t, err)
			must(t, app.OnServe().Trigger(&core.ServeEvent{App: app, Router: router}, func(*core.ServeEvent) error { return nil }))
			if len(rows(t, app)) != 2 {
				t.Fatal("startup did not import rates")
			}
			jobs := app.Cron().Jobs()
			found := false
			for _, job := range jobs {
				if job.Id() == jobID {
					found = true
					job.Run()
				}
			}
			if !found || len(rows(t, app)) != 2 {
				t.Fatal("cron missing or duplicates imported")
			}
			handler, err := router.BuildMux()
			must(t, err)
			id := rows(t, app)[0].Id
			for _, token := range tokens {
				for _, tc := range []struct {
					method, path, body string
					allowed            bool
				}{
					{"GET", "/records", "", true},
					{"GET", "/records/" + id, "", true},
					{"POST", "/records", `{"currency":"EUR"}`, false},
					{"PATCH", "/records/" + id, `{"rate":1}`, false},
					{"DELETE", "/records/" + id, "", false},
					{"DELETE", "/truncate", "", false},
					{"PATCH", "", `{"name":"renamed"}`, false},
					{"DELETE", "", "", false},
				} {
					req := httptest.NewRequest(tc.method, "/api/collections/"+Collection+tc.path, strings.NewReader(tc.body))
					req.Header.Set("Authorization", token)
					req.Header.Set("Content-Type", "application/json")
					w := httptest.NewRecorder()
					handler.ServeHTTP(w, req)
					if (w.Code >= 200 && w.Code < 300) != tc.allowed {
						t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body.String())
					}
				}
			}
			if len(rows(t, app)) != 2 {
				t.Fatal("denied request changed data")
			}
			for _, r := range rows(t, app) {
				if r.GetFloat("rate") == 1 {
					t.Fatal("denied update changed rate")
				}
			}
			// An outage during a later startup does not prevent serving retained rates.
			body = "unavailable"
			must(t, app.OnServe().Trigger(&core.ServeEvent{App: app, Router: router}, func(*core.ServeEvent) error { return nil }))
			if len(rows(t, app)) != 2 {
				t.Fatal("outage lost rates")
			}
		})
	}
}

func TestInstallDoesNotTakeOverExistingCollection(t *testing.T) {
	app := pocketbase.NewWithConfig(pocketbase.Config{DefaultDataDir: t.TempDir()})
	must(t, app.Bootstrap())
	t.Cleanup(func() { _ = app.ClearBootstrap() })
	c := core.NewBaseCollection(Collection)
	must(t, app.Save(c))
	if err := install(app); err == nil {
		t.Fatal("existing collection taken over")
	}
	after, err := app.FindCollectionByNameOrId(Collection)
	must(t, err)
	if after.Id != c.Id || after.System || after.Fields.GetByName("rate") != nil {
		t.Fatal("conflicting schema modified")
	}
}
