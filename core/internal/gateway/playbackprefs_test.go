package gateway

import (
	"strings"
	"testing"
)

func TestPlaybackPrefs(t *testing.T) {
	e := newEnv(t)
	_, alice, bob := e.accounts()
	_, b := e.do("GET", "/api/me/playback", "", alice)
	if !strings.Contains(string(b), `"replaygain":"track"`) || !strings.Contains(string(b), `"gapless":true`) {
		t.Fatalf("defaults %s", b)
	}
	if res, b := e.do("PUT", "/api/me/playback", `{"replaygain":"album","preamp_db":-3,"gapless":false}`, alice); res.StatusCode != 200 || !strings.Contains(string(b), `"replaygain":"album"`) {
		t.Fatalf("put %d %s", res.StatusCode, b)
	}
	for _, bad := range []string{`{"replaygain":"loud"}`, `{"preamp_db":40}`} {
		if res, _ := e.do("PUT", "/api/me/playback", bad, alice); res.StatusCode != 400 {
			t.Fatalf("%s accepted", bad)
		}
	}
	_, a := e.do("GET", "/api/me/playback", "", alice)
	_, o := e.do("GET", "/api/me/playback", "", bob)
	if !strings.Contains(string(a), `"preamp_db":-3`) || !strings.Contains(string(o), `"replaygain":"track"`) {
		t.Fatalf("per account: alice %s bob %s", a, o)
	}
}
