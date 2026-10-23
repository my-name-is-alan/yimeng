package subsonic

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/auth"
	"github.com/my-name-is-alan/yimeng/core/internal/infra/db"
	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/migrations"
)

func TestSubsonicPingAuth(t *testing.T) {
	tmp := t.TempDir()
	d, err := db.Open(filepath.Join(tmp, "lib.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	db.Migrate(context.Background(), d, migrations.FS)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := auth.New(d, 5*time.Minute, log)
	code, _, _ := a.NewCode()
	_, tok, err := a.Pair(context.Background(), "1.1.1.1", code, "sub", "test")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(tmp, "music")
	os.MkdirAll(root, 0o755)
	b, _ := os.ReadFile("../adapters/local/testdata/tagged.mp3")
	os.WriteFile(filepath.Join(root, "a.mp3"), b, 0o644)
	sc := library.NewScanner(d, library.Options{Roots: []string{root}})
	if _, err := sc.Start(context.Background(), library.StartRequest{}); err != nil {
		t.Fatal(err)
	}
	sc.Wait()

	h := &Handler{Auth: a, Library: sc, Playlists: sc, Version: "test"}
	mux := http.NewServeMux()
	h.Mount(mux)

	req := httptest.NewRequest("GET", "/rest/ping.view?f=json&u=x&p="+tok, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	var wrap map[string]any
	json.Unmarshal(rec.Body.Bytes(), &wrap)
	sr := wrap["subsonic-response"].(map[string]any)
	if sr["status"] != "ok" {
		t.Fatal(wrap)
	}

	req = httptest.NewRequest("GET", "/rest/ping.view?f=json&u=x&t=dead&s=beef", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	json.Unmarshal(rec.Body.Bytes(), &wrap)
	sr = wrap["subsonic-response"].(map[string]any)
	if sr["status"] != "failed" {
		t.Fatal(wrap)
	}

	req = httptest.NewRequest("GET", "/rest/getAlbumList2.view?f=json&u=x&p="+tok+"&type=newest&size=10", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatal(rec.Body.String())
	}
	json.Unmarshal(rec.Body.Bytes(), &wrap)
	sr = wrap["subsonic-response"].(map[string]any)
	if sr["status"] != "ok" {
		t.Fatal(wrap)
	}
}
