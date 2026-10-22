package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMergedPlaylistImport(t *testing.T) {
	e := switchEnv(t)
	tr := flacTrack(t, e)
	_, alice, bob := e.accounts()
	// a 译梦 playlist mixing a local and an online track
	res, body := e.do("POST", "/api/playlists", `{"name":"mix","track_ids":["`+tr.ID+`","gw:full"]}`, alice)
	if res.StatusCode != 201 {
		t.Fatalf("mixed create %d %s", res.StatusCode, body)
	}
	var pl struct{ ID string }
	json.Unmarshal(body, &pl)
	// one-click import of a platform playlist into it; unavailable kept + counted
	res, body = e.do("POST", "/api/online/gw/playlists/p1/clone", `{"into":"`+pl.ID+`","prefer_local":false}`, alice)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"unavailable":1`) {
		t.Fatalf("import into %d %s", res.StatusCode, body)
	}
	if res, _ := e.do("POST", "/api/online/gw/playlists/p1/clone", `{"into":"`+pl.ID+`"}`, bob); res.StatusCode != 404 {
		t.Fatalf("bob imported into alice's playlist: %d", res.StatusCode)
	}
	_, body = e.do("GET", "/api/playlists/"+pl.ID+"/tracks?limit=50", "", alice)
	var page struct {
		Items []struct {
			ID          string `json:"id"`
			Title       string `json:"title"`
			Entitlement string `json:"entitlement"`
			Playable    bool   `json:"playable"`
		} `json:"items"`
	}
	json.Unmarshal(body, &page)
	got := map[string]string{}
	for _, it := range page.Items {
		got[it.ID] = it.Entitlement
		if it.ID == "gw:none" && (it.Playable || it.Title == "") {
			t.Fatalf("taken-down snapshot %+v", it)
		}
	}
	if len(page.Items) != 5 || got[tr.ID] != "" || got["gw:none"] != "unavailable" || got["gw:prev"] != "preview" || got["gw:full"] != "full" {
		t.Fatalf("items %s", body)
	}
	// the preview item auto-switches when streamed (D50)
	res, body = e.do("GET", "/api/stream/gw:prev", "", alice)
	if res.StatusCode != 200 || res.Header.Get("X-Yimeng-Fallback") != "gx" {
		t.Fatalf("stream %d %s", res.StatusCode, body)
	}
}
