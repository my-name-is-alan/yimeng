package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/system"
)

func TestSystemAPI(t *testing.T) {
	e := newEnv(t)
	flacTrack(t, e)
	res, body := e.do("GET", "/api/system", "", e.token)
	if res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	var in system.Info
	if err := json.Unmarshal(body, &in); err != nil {
		t.Fatal(err)
	}
	if in.Version != "test" || in.Library.Tracks < 1 || in.Library.Files < 1 || len(in.Roots) != 1 || !in.Roots[0].OK {
		t.Fatalf("%s", body)
	}
	if in.Features["backup"] != true || in.Diagnostics == nil {
		t.Fatalf("features/diagnostics: %s", body)
	}
	if !strings.Contains(string(body), `"transcode"`) {
		t.Fatalf("missing transcode block: %s", body)
	}
	// any authenticated device can read /api/system (not admin-only); backups stay admin-only
	other := e.pairAnother("phone")
	if res, _ := e.do("GET", "/api/system", "", other); res.StatusCode != 200 {
		t.Fatalf("non-admin system: %d", res.StatusCode)
	}
	if res, _ := e.do("GET", "/api/system/backups", "", other); res.StatusCode != 403 {
		t.Fatalf("non-admin backups: %d", res.StatusCode)
	}
}
