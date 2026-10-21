package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/app"
	"github.com/my-name-is-alan/yimeng/core/internal/infra/db"
	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/migrations"
)

func TestScanEndpoints(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "music")
	os.MkdirAll(root, 0o755)
	b, _ := os.ReadFile("../adapters/local/testdata/tagged.flac")
	os.WriteFile(filepath.Join(root, "a.flac"), b, 0o644)

	d, err := db.Open(filepath.Join(tmp, "lib.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	db.Migrate(context.Background(), d, migrations.FS)
	sc := library.NewScanner(d, library.Options{Roots: []string{root}, CoverDir: filepath.Join(tmp, "c")})
	srv := httptest.NewServer(NewRouter(Deps{Library: &app.Library{Scanner: sc, BG: context.Background()}}))
	defer srv.Close()

	res, _ := http.Get(srv.URL + "/api/library/scan")
	if res.StatusCode != 404 {
		t.Fatalf("latest before any scan: %d", res.StatusCode)
	}
	res, _ = http.Post(srv.URL+"/api/library/scan", "application/json", strings.NewReader(`{"root":"/etc"}`))
	if res.StatusCode != 400 {
		t.Fatalf("bad root: %d", res.StatusCode)
	}
	res, _ = http.Post(srv.URL+"/api/library/scan", "application/json", strings.NewReader(`{"bogus":1}`))
	if res.StatusCode != 400 {
		t.Fatalf("bad body: %d", res.StatusCode)
	}
	res, err = http.Post(srv.URL+"/api/library/scan", "application/json", nil)
	if err != nil || res.StatusCode != 202 {
		t.Fatalf("start: %v %d", err, res.StatusCode)
	}
	var job library.Job
	json.NewDecoder(res.Body).Decode(&job)
	sc.Wait()

	deadline := time.Now().Add(5 * time.Second)
	for {
		res, _ = http.Get(srv.URL + "/api/library/scan/" + job.ID)
		json.NewDecoder(res.Body).Decode(&job)
		if job.Status == "completed" || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if job.Status != "completed" || job.Added != 1 {
		t.Fatalf("%+v", job)
	}
	res, _ = http.Get(srv.URL + "/api/library/scan/nope")
	if res.StatusCode != 404 {
		t.Fatal(res.StatusCode)
	}
	res, _ = http.Get(srv.URL + "/healthz")
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
}
