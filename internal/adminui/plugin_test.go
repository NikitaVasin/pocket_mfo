package adminui

import (
	"io/fs"
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

func TestRegistrationAddsOneSharedExtension(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	for range 8 {
		Register(app)
	}
	event := &core.ServeEvent{App: app}
	if err := app.OnServe().Trigger(event); err != nil {
		t.Fatal(err)
	}
	if len(event.UIExtensions) != 1 || event.UIExtensions[0].Name != "pocket-mfo-admin" {
		t.Fatalf("expected one shared extension, got %v", event.UIExtensions)
	}
	if data, err := fs.ReadFile(event.UIExtensions[0].FS, "main.js"); err != nil || len(data) == 0 {
		t.Fatal("missing embedded navigation", err)
	}
}
