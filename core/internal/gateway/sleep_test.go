package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSleepTimerAPI(t *testing.T) {
	e := newEnv(t)
	_, alice, bob := e.accounts()
	_, b := e.do("GET", "/api/devices/me", "", alice)
	var me struct{ ID string }
	json.Unmarshal(b, &me)
	res, body := e.do("POST", "/api/players/"+me.ID+"/sleep", `{"end_of_track":true,"fade_seconds":20}`, alice)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"end_of_track":true`) || !strings.Contains(string(body), `"fade_ms":20000`) {
		t.Fatalf("arm %d %s", res.StatusCode, body)
	}
	res, body = e.do("POST", "/api/players/"+me.ID+"/sleep", `{"minutes":30}`, alice)
	if !strings.Contains(string(body), `"until"`) || !strings.Contains(string(body), `"fade_ms":30000`) {
		t.Fatalf("minutes %s", body)
	}
	if res, _ := e.do("POST", "/api/players/"+me.ID+"/sleep", `{"minutes":-1}`, alice); res.StatusCode != 400 {
		t.Fatalf("bad minutes %d", res.StatusCode)
	}
	if res, _ := e.do("POST", "/api/players/"+me.ID+"/sleep", `{"minutes":0}`, bob); res.StatusCode != 404 {
		t.Fatalf("bob cleared alice's timer: %d", res.StatusCode)
	}
	_, body = e.do("POST", "/api/players/"+me.ID+"/sleep", `{}`, alice)
	if strings.Contains(string(body), `"sleep"`) {
		t.Fatalf("not cleared %s", body)
	}
}
