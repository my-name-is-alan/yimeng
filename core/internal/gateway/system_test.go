package gateway

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/backup"
)

// pairAnother pairs a second (non-admin) device and returns its token.
func (e *env) pairAnother(name string) string {
	e.t.Helper()
	code, _, _ := e.auth.NewCode()
	res, body := e.do("POST", "/api/pair", `{"code":"`+code+`","name":"`+name+`"}`, "")
	if res.StatusCode != 201 {
		e.t.Fatalf("pair: %d %s", res.StatusCode, body)
	}
	var pr struct {
		Token  string
		Device struct{ Admin bool }
	}
	json.Unmarshal(body, &pr)
	return pr.Token
}

func TestBackupAPI(t *testing.T) {
	e := newEnv(t)
	other := e.pairAnother("phone")
	for _, ep := range [][2]string{{"GET", "/api/system/backups"}, {"POST", "/api/system/backups"},
		{"GET", "/api/system/export"}, {"POST", "/api/system/restore"}} {
		if res, _ := e.do(ep[0], ep[1], "", other); res.StatusCode != http.StatusForbidden {
			t.Errorf("non-admin %v: %d", ep, res.StatusCode)
		}
	}
	res, body := e.do("POST", "/api/system/backups", "", e.token)
	if res.StatusCode != 201 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	var in backup.Info
	json.Unmarshal(body, &in)
	res, body = e.do("GET", "/api/system/backups", "", e.token)
	if res.StatusCode != 200 || !strings.Contains(string(body), in.Name) {
		t.Fatalf("%s", body)
	}
	res, body = e.do("GET", "/api/system/backups/"+in.Name, "", e.token)
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "application/zip" || len(body) < 100 {
		t.Fatalf("download %d", res.StatusCode)
	}
	for _, bad := range []string{"..%2Flib.db", "x.ymbak", "yimeng-20260101-000000.ymbak"} {
		if res, _ := e.do("GET", "/api/system/backups/"+bad, "", e.token); res.StatusCode != 404 {
			t.Errorf("%s: %d", bad, res.StatusCode)
		}
	}
	res, exp := e.do("GET", "/api/system/export", "", e.token)
	if res.StatusCode != 200 || len(exp) < 100 {
		t.Fatalf("export %d", res.StatusCode)
	}
	if res, _ := e.do("POST", "/api/system/restore", "garbage", e.token); res.StatusCode != 400 {
		t.Fatalf("garbage restore %d", res.StatusCode)
	}
	if res, body := e.do("POST", "/api/system/restore", string(exp), e.token); res.StatusCode != 202 ||
		!strings.Contains(string(body), "restart_required") {
		t.Fatalf("restore %d %s", res.StatusCode, body)
	}
	if res, _ := e.do("DELETE", "/api/system/backups/"+in.Name, "", e.token); res.StatusCode != 204 {
		t.Fatal("delete")
	}
}
