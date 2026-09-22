package partnerlinks

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/NikitaVasin/pocket_mfo/plugins/variants"
)

func TestConversationsAreServerOwnedWithoutVariants(t *testing.T) {
	for _, locked := range []bool{false, true} {
		t.Run(fmt.Sprint("schemaLock=", locked), func(t *testing.T) {
			x := setup(t, locked)
			token := tokenFrom(x.issue(t))
			row, _, err := loadConversation(x.app, token)
			must(t, err)
			c := row.Collection()
			if !c.System || c.Fields.GetByName(variants.SetField) != nil {
				t.Fatal("conversations must be a server-owned collection without variants")
			}
			before, _ := json.Marshal(row.PublicExport())
			counts := map[string]int{}
			for _, name := range []string{"pv_configs", "pv_sets", "pv_states", "pv_history"} {
				rows, err := x.app.FindAllRecords(name)
				must(t, err)
				counts[name] = len(rows)
			}
			for _, name := range []string{c.Name, c.Id} {
				cfg := variants.Config{Collection: name, AuthCollection: x.user.Collection().Id, Variables: true, Default: variants.Variant{Key: "default"}}
				if _, err := variants.Publish(x.app, cfg); err == nil {
					t.Fatal("Go Publish enabled variants on conversions")
				}
				body, _ := json.Marshal(cfg)
				for _, auth := range []string{x.auth, x.admin} {
					w := x.request("PUT", "/api/variants/admin/collections/"+name, auth, string(body), "application/json")
					if w.Code < 400 {
						t.Fatal("HTTP enabled variants")
					}
					base := "/api/collections/" + name
					for _, request := range []struct{ method, path, body string }{
						{"POST", base + "/records", `{"status":"lead"}`},
						{"PATCH", base + "/records/" + row.Id, `{"status":"rejected"}`},
						{"DELETE", base + "/records/" + row.Id, ``},
						{"DELETE", base + "/truncate", ``},
					} {
						w = x.request(request.method, request.path, auth, request.body, "application/json")
						if w.Code != 403 {
							t.Fatalf("%s %s: %d %s", request.method, request.path, w.Code, w.Body)
						}
						if request.path == base+"/truncate" {
							continue
						}
						batch, _ := json.Marshal(map[string]any{"requests": []any{map[string]any{"method": request.method, "url": request.path, "body": map[string]string{"status": "rejected"}}}})
						if w = x.request("POST", "/api/batch", auth, string(batch), "application/json"); w.Code < 400 {
							t.Fatal("batch wrote a conversion")
						}
					}
				}
				if w := x.request("PATCH", "/api/collections/"+name, x.admin, `{"system":false,"updateRule":""}`, "application/json"); w.Code < 400 {
					t.Fatal("service protection removed")
				}
			}
			after, err := x.app.FindRecordById(c.Id, row.Id)
			must(t, err)
			actual, _ := json.Marshal(after.PublicExport())
			if string(before) != string(actual) {
				t.Fatal("denied operations changed the record")
			}
			rows, err := x.app.FindAllRecords(c.Id)
			must(t, err)
			if len(rows) != 1 {
				t.Fatal("denied operations changed record count")
			}
			if _, err = variants.Load(x.app, c.Id); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("variant configuration persisted: %v", err)
			}
			for name, count := range counts {
				rows, err := x.app.FindAllRecords(name)
				must(t, err)
				if len(rows) != count {
					t.Fatalf("variant side effects in %s", name)
				}
			}
			updated, err := x.app.FindCollectionByNameOrId(c.Id)
			must(t, err)
			if !updated.System || updated.Fields.GetByName(variants.SetField) != nil || *updated.ViewRule != conversationRule {
				t.Fatal("denied operation changed schema")
			}
			if code := postConversion(x, token, "new", "server-order"); code != 200 {
				t.Fatalf("server cannot update conversion: %d", code)
			}
		})
	}
}
