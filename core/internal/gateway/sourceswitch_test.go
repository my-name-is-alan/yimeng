package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

// gxPlugin is a second platform carrying "Song Preview" with full rights
// (same title/artist, duration within 2 s) plus a too-long namesake.
type gxPlugin struct{ *gwPlugin }

var gxTracks = map[string]online.Track{
	"same": {ID: "gx:same", Origin: "gx", Title: "Song Preview", ArtistName: "歌手", DurationMs: 181500, Playable: online.EntFull},
	"long": {ID: "gx:long", Origin: "gx", Title: "Song Preview", ArtistName: "歌手", DurationMs: 240000, Playable: online.EntFull},
}

func (p *gxPlugin) Manifest() online.Manifest {
	m := p.gwPlugin.Manifest()
	m.ID, m.Name = "gx", "第二平台"
	return m
}
func (p *gxPlugin) Search(ctx context.Context, q string, limit int) (online.SearchResult, error) {
	r := online.SearchResult{}
	if strings.Contains(strings.ToLower(q), "song preview") {
		r.Tracks = []online.Track{gxTracks["long"], gxTracks["same"]}
	}
	return r, nil
}
func (p *gxPlugin) Tracks(ctx context.Context, ids []string) ([]online.Track, error) {
	var out []online.Track
	for _, id := range ids {
		if t, ok := gxTracks[id]; ok {
			out = append(out, t)
		}
	}
	return out, nil
}
func (p *gxPlugin) Resolve(ctx context.Context, id string, q online.Quality) (online.Stream, error) {
	if _, ok := gxTracks[id]; !ok {
		return online.Stream{}, online.ErrUnavailable
	}
	return p.gwPlugin.Resolve(ctx, "full", q) // CDN serves "AUDIO-full"
}

func switchEnv(t *testing.T) *env {
	e, _ := newOnlineEnvExtra(t, func(cdn string) online.Factory {
		return func(env online.Env) (online.Plugin, error) {
			return &gxPlugin{&gwPlugin{env: env, cdn: cdn}}, nil
		}
	})
	for _, id := range []string{"gw", "gx"} {
		e.do("PUT", "/api/plugins/"+id, `{"enabled":true}`, e.token)
		e.do("POST", "/api/plugins/"+id+"/login", `{"method":"cookie","cookie":"S=SECRET-SESSION"}`, e.token)
	}
	return e
}

func TestSourceSwitch(t *testing.T) {
	e := switchEnv(t)
	tok := e.token
	// detail of a preview-only track lists the matching alternative only
	_, body := e.do("GET", "/api/tracks/gw:prev", "", tok)
	var d struct {
		Alternatives []online.Alternative `json:"alternatives"`
	}
	json.Unmarshal(body, &d)
	if len(d.Alternatives) != 1 || d.Alternatives[0].TrackID != "gx:same" {
		t.Fatalf("alternatives %s", body)
	}
	// resolve + stream switch to gx with full entitlement
	_, body = e.do("GET", "/api/online/resolve/gw:prev", "", tok)
	if !strings.Contains(string(body), `"fallback":"gx"`) || !strings.Contains(string(body), `"entitlement":"full"`) {
		t.Fatalf("resolve %s", body)
	}
	res, body := e.do("GET", "/api/stream/gw:prev", "", tok)
	if res.StatusCode != 200 || string(body) != "AUDIO-full" || res.Header.Get("X-Yimeng-Fallback") != "gx" {
		t.Fatalf("stream %d %q %v", res.StatusCode, body, res.Header)
	}
	// user turns switching off: the preview segment is played instead
	if res, b := e.do("PUT", "/api/me/source-switch", `{"enabled":false,"priority":["gx","gw"]}`, tok); res.StatusCode != 200 || !strings.Contains(string(b), `"enabled":false`) {
		t.Fatalf("put prefs %d %s", res.StatusCode, b)
	}
	_, body = e.do("GET", "/api/online/resolve/gw:prev", "", tok)
	if strings.Contains(string(body), `"fallback"`) || !strings.Contains(string(body), `"entitlement":"preview"`) {
		t.Fatalf("resolve with switching off %s", body)
	}
	if res, _ := e.do("PUT", "/api/me/source-switch", `{"priority":["bogus"]}`, tok); res.StatusCode != 400 {
		t.Fatalf("unknown platform accepted: %d", res.StatusCode)
	}
}

// The preference is per account: one listener's opt-out doesn't affect others.
func TestSourceSwitchPerAccount(t *testing.T) {
	e := switchEnv(t)
	_, alice, bob := e.accounts()
	e.do("PUT", "/api/me/source-switch", `{"enabled":false}`, alice)
	_, a := e.do("GET", "/api/me/source-switch", "", alice)
	_, b := e.do("GET", "/api/me/source-switch", "", bob)
	if !strings.Contains(string(a), `"enabled":false`) || !strings.Contains(string(b), `"enabled":true`) {
		t.Fatalf("prefs leak: alice %s bob %s", a, b)
	}
	_, body := e.do("GET", "/api/online/resolve/gw:prev", "", bob)
	if !strings.Contains(string(body), `"fallback":"gx"`) {
		t.Fatalf("bob should switch: %s", body)
	}
	_, body = e.do("GET", "/api/online/resolve/gw:prev", "", alice)
	if strings.Contains(string(body), `"fallback"`) {
		t.Fatalf("alice opted out: %s", body)
	}
}
