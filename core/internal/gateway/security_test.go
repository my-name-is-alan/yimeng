package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/auth"
	"github.com/my-name-is-alan/yimeng/core/internal/secalert"
)

func TestSecurityAlerts(t *testing.T) {
	e := newEnv(t)
	admin, alice, bob := e.accounts()
	// failed logins for alice: 3rd failure raises one alert
	for i := 0; i < 4; i++ {
		e.do("POST", "/api/auth/login", `{"username":"alice","password":"wrong-pass-1"}`, "")
	}
	list := func(tok string) (items []map[string]any, unread int) {
		_, b := e.do("GET", "/api/security/alerts", "", tok)
		var r struct {
			Items  []map[string]any `json:"items"`
			Unread int              `json:"unread"`
		}
		json.Unmarshal(b, &r)
		return r.Items, r.Unread
	}
	kinds := func(items []map[string]any) string {
		var k []string
		for _, it := range items {
			k = append(k, it["kind"].(string))
		}
		return strings.Join(k, ",")
	}
	ai, au := list(alice)
	if !strings.Contains(kinds(ai), "login_failed") || au == 0 {
		t.Fatalf("alice alerts %v", ai)
	}
	if strings.Count(kinds(ai), "login_failed") != 1 {
		t.Fatalf("failed-login alert not deduped: %s", kinds(ai))
	}
	if bi, _ := list(bob); strings.Contains(kinds(bi), "login_failed") {
		t.Fatalf("bob sees alice's alert: %v", bi)
	}
	if ad, _ := list(admin); !strings.Contains(kinds(ad), "login_failed") {
		t.Fatalf("admin misses alert: %v", ad)
	}
	// pairing a device for bob
	e.pairDevice("Living room")
	if ad, _ := list(admin); !strings.Contains(kinds(ad), "device_paired") {
		t.Fatalf("pairing not alerted: %v", ad)
	}
	// mark read
	if res, _ := e.do("POST", "/api/security/alerts/read", `{}`, alice); res.StatusCode != 204 {
		t.Fatal("read")
	}
	if _, u := list(alice); u != 0 {
		t.Fatalf("unread after read-all %d", u)
	}
	if _, u := list(admin); u == 0 {
		t.Fatal("alice's read-all cleared admin's other alerts")
	}
}

func TestBackupPolicyAndRestoreEndpoints(t *testing.T) {
	e := newEnv(t)
	admin, alice, _ := e.accounts()
	if res, _ := e.do("GET", "/api/system/backups/policy", "", alice); res.StatusCode != 403 {
		t.Fatalf("non-admin policy: %d", res.StatusCode)
	}
	if res, _ := e.do("PUT", "/api/system/backups/policy", `{"keep":0}`, admin); res.StatusCode != 400 {
		t.Fatal("keep 0 accepted")
	}
	res, b := e.do("PUT", "/api/system/backups/policy", `{"enabled":true,"keep":5}`, admin)
	if res.StatusCode != 200 || !strings.Contains(string(b), `"keep":5`) {
		t.Fatalf("put policy %d %s", res.StatusCode, b)
	}
	res, b = e.do("POST", "/api/system/backups", "", admin)
	var m struct{ Name string }
	json.Unmarshal(b, &m)
	if res.StatusCode/100 != 2 || m.Name == "" {
		t.Fatalf("create %d %s", res.StatusCode, b)
	}
	if res, _ := e.do("POST", "/api/system/backups/"+m.Name+"/restore", `{"confirm":"`+m.Name+`"}`, alice); res.StatusCode != 403 {
		t.Fatal("non-admin restore")
	}
	if res, _ := e.do("POST", "/api/system/backups/"+m.Name+"/restore", `{}`, admin); res.StatusCode != 400 {
		t.Fatal("restore without confirm")
	}
	if res, _ := e.do("POST", "/api/system/backups/nope.db/restore", `{"confirm":"nope.db"}`, admin); res.StatusCode != 404 {
		t.Fatalf("missing: %d", res.StatusCode)
	}
	res, b = e.do("POST", "/api/system/backups/"+m.Name+"/restore", `{"confirm":"`+m.Name+`"}`, admin)
	if res.StatusCode != 202 || !strings.Contains(string(b), "restart_required") {
		t.Fatalf("restore %d %s", res.StatusCode, b)
	}
}

func TestSecurityAlertEventScope(t *testing.T) {
	ev := secalert.Event{UserID: "u-alice", Alert: secalert.Alert{Kind: secalert.KindNewIPLogin}}
	if _, ok := eventFor(auth.Device{UserID: "u-bob"}, ev); ok {
		t.Fatal("bob received alice's alert")
	}
	if _, ok := eventFor(auth.Device{UserID: "u-alice"}, ev); !ok {
		t.Fatal("alice missed her alert")
	}
	if _, ok := eventFor(auth.Device{Admin: true}, ev); !ok {
		t.Fatal("admin missed alert")
	}
	if _, ok := eventFor(auth.Device{}, secalert.Event{Alert: secalert.Alert{Kind: secalert.KindLockout}}); ok {
		t.Fatal("unowned alert leaked to non-admin")
	}
}
