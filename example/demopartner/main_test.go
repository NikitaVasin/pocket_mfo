package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func testCabinet(t *testing.T) *cabinet {
	t.Helper()
	c, e := openCabinet(filepath.Join(t.TempDir(), "data.json"))
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func call(c *cabinet, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c.handler().ServeHTTP(w, r)
	return w
}
func TestClickPersistenceAndConcurrentDuplicate(t *testing.T) {
	c := testCabinet(t)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := call(c, "GET", "/click?subid=abc123&url=https://evil.test", "")
			if w.Code != 302 || w.Header().Get("Location") != "https://www.google.com/" {
				t.Error("wrong redirect")
			}
			if w.Header().Get("Referrer-Policy") != "no-referrer" {
				t.Error("leaks referrer")
			}
		}()
	}
	wg.Wait()
	if len(c.state.Records) != 1 {
		t.Fatal("duplicate attribution")
	}
	for _, p := range []string{"/click", "/click?subid=%3Cscript%3E"} {
		if call(c, "GET", p, "").Code != 400 {
			t.Fatal("invalid click accepted")
		}
	}
	if call(c, "HEAD", "/click?subid=second", "").Code != 405 {
		t.Fatal("HEAD creates event")
	}
	if len(c.state.Records) != 1 {
		t.Fatal("invalid request changed records")
	}
	reopened, e := openCabinet(c.file)
	if e != nil || len(reopened.state.Records) != 1 || reopened.state.Records[0].Token != "abc123" {
		t.Fatal("lost state")
	}
	if strings.Contains(call(c, "GET", "/api/conversions", "").Body.String(), "abc123") {
		t.Fatal("token exposed")
	}
}
func TestPostbackFormatsAndSecrets(t *testing.T) {
	for _, method := range []string{"GET", "POST JSON", "POST form"} {
		for _, location := range []string{"header", "query", "body"} {
			if method == "GET" && location == "body" {
				continue
			}
			t.Run(method+location, func(t *testing.T) {
				cfg := settings{URL: "https://receiver.test/post?keep=1", Method: method, SecretLocation: location, SecretName: "auth", Secret: "a&b=c"}
				row := conversion{ID: "order", Token: "click", Status: "hold", Amount: "123.45", Currency: "RUB", EventID: "event", EventTimestamp: 123}
				req, e := postbackRequest(cfg, row)
				if e != nil {
					t.Fatal(e)
				}
				fields := map[string]string{}
				switch method {
				case "GET":
					for k, v := range req.URL.Query() {
						fields[k] = v[0]
					}
				case "POST JSON":
					if e = json.NewDecoder(req.Body).Decode(&fields); e != nil {
						t.Fatal(e)
					}
				case "POST form":
					b, _ := io.ReadAll(req.Body)
					values, _ := url.ParseQuery(string(b))
					for k, v := range values {
						fields[k] = v[0]
					}
				}
				for k, want := range map[string]string{"subid": "click", "status": "hold", "lead_id": "order", "event_id": "event", "timestamp": "123", "amount": "123.45", "currency": "RUB"} {
					if fields[k] != want {
						t.Fatalf("missing %s", k)
					}
				}
				secret := ""
				switch location {
				case "query":
					secret = req.URL.Query().Get("auth")
				case "header":
					secret = req.Header.Get("auth")
				case "body":
					secret = fields["auth"]
				}
				if secret != cfg.Secret || req.URL.Query().Get("keep") != "1" {
					t.Fatal("bad encoding")
				}
			})
		}
	}
}
func TestDeliveryFailureRetryAndStatuses(t *testing.T) {
	c := testCabinet(t)
	var received []map[string]string
	responseCode := 502
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("secret") != "public-demo-postback-secret" {
			t.Error("wrong secret")
		}
		var fields map[string]string
		_ = json.NewDecoder(r.Body).Decode(&fields)
		received = append(received, fields)
		w.WriteHeader(responseCode)
	}))
	defer sink.Close()
	c.state.Settings.URL = sink.URL
	call(c, "GET", "/click?subid=abc", "")
	id := c.state.Records[0].ID
	if call(c, "POST", "/api/conversions/"+id+"/retry", "{}").Code != 400 {
		t.Fatal("pending sent")
	}
	for _, status := range []string{"lead", "hold", "approved", "rejected"} {
		w := call(c, "POST", "/api/conversions/"+id+"/status", `{"status":"`+status+`","amount":"100.25","currency":"RUB"}`)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		if c.state.Records[0].Status != status || c.state.Records[0].ResponseCode != 502 {
			t.Fatal("local state must survive remote failure")
		}
	}
	previous := received[len(received)-1]
	responseCode = 200
	call(c, "POST", "/api/conversions/"+id+"/retry", "{}")
	last := received[len(received)-1]
	if previous["event_id"] != last["event_id"] || previous["timestamp"] != last["timestamp"] || c.state.Records[0].Delivery != "Доставлен" {
		t.Fatal("retry changed event")
	}
	before := len(received)
	for _, body := range []string{`{"status":"unknown","amount":"100","currency":"RUB"}`, `{"status":"lead","amount":"-1","currency":"RUB"}`, `{"status":"lead","amount":"100","currency":"rub"}`} {
		if call(c, "POST", "/api/conversions/"+id+"/status", body).Code != 400 {
			t.Fatal("invalid accepted")
		}
	}
	if len(received) != before {
		t.Fatal("invalid request delivered")
	}
}
func TestSettingsProtectionAndAtomicFailure(t *testing.T) {
	c := testCabinet(t)
	body := `{"url":"http://localhost:8090/post","method":"POST JSON","secretLocation":"body","secretName":"auth","secret":"new-secret"}`
	w := call(c, "PUT", "/api/settings", body)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if strings.Contains(call(c, "GET", "/api/settings", "").Body.String(), "new-secret") {
		t.Fatal("secret exposed")
	}
	if call(c, "PUT", "/api/settings", strings.Replace(body, `"new-secret"`, `""`, 1)).Code != 200 || c.state.Settings.Secret != "new-secret" {
		t.Fatal("blank destroyed secret")
	}
	r := httptest.NewRequest("PUT", "/api/settings", strings.NewReader(body))
	r.Header.Set("Origin", "https://foreign.test")
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	c.handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin mutation")
	}
	old := c.state.Settings
	c.file = filepath.Join(t.TempDir(), "directory")
	_ = os.Mkdir(c.file, 0700)
	if call(c, "PUT", "/api/settings", strings.Replace(body, "new-secret", "replacement", 1)).Code != 500 || c.state.Settings != old {
		t.Fatal("failed persistence changed memory")
	}
	if call(c, "GET", "/click?subid=not-saved", "").Code != 500 || len(c.state.Records) != 0 {
		t.Fatal("redirected unsaved click")
	}
}
