package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

func allTracks(t *testing.T, e *env) []library.Track {
	var tp library.Page[library.Track]
	_, body := e.do("GET", "/api/tracks?limit=50&sort=title", "", e.token)
	json.Unmarshal(body, &tp)
	return tp.Items
}

func TestPlaylistsAPI(t *testing.T) {
	e := newEnv(t)
	flacTrack(t, e)
	tr := allTracks(t, e)
	if len(tr) != 2 {
		t.Fatal(len(tr))
	}
	res, body := e.do("POST", "/api/playlists", `{"name":"Mix","track_ids":["`+tr[0].ID+`","`+tr[1].ID+`"]}`, e.token)
	if res.StatusCode != 201 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	var p library.Playlist
	json.Unmarshal(body, &p)
	if p.TrackCount != 2 || p.Smart {
		t.Fatalf("%s", body)
	}
	for _, b := range []string{`{"name":""}`, `{"name":"x","track_ids":["nope"]}`, `{}`,
		`{"name":"s","rules":{"rules":[{"field":"title; DROP TABLE tracks","op":"is","value":"x"}]}}`,
		`{"name":"s","rules":{"rules":[{"field":"year","op":"gt","value":"abc"}]}}`,
		`{"name":"s","rules":{"sort":"id; --"}}`} {
		if res, _ := e.do("POST", "/api/playlists", b, e.token); res.StatusCode != 400 {
			t.Errorf("%s: %d", b, res.StatusCode)
		}
	}
	ops := []struct {
		body string
		code int
	}{
		{`{"op":"append","track_ids":["` + tr[0].ID + `"]}`, 200},
		{`{"op":"move","from":2,"to":0}`, 200},
		{`{"op":"remove","index":1}`, 200},
		{`{"op":"remove","index":9}`, 400},
		{`{"op":"bogus"}`, 400},
	}
	for _, o := range ops {
		if res, body := e.do("POST", "/api/playlists/"+p.ID+"/items", o.body, e.token); res.StatusCode != o.code {
			t.Errorf("%s: %d %s", o.body, res.StatusCode, body)
		}
	}
	var pp library.PlaylistPage
	_, body = e.do("GET", "/api/playlists/"+p.ID, "", e.token)
	json.Unmarshal(body, &pp)
	if len(pp.Tracks.Items) != 2 || pp.Tracks.Items[0].ID != tr[0].ID || pp.Tracks.Items[1].ID != tr[1].ID {
		t.Fatalf("%s", body)
	}
	// m3u round trip
	res, m3u := e.do("GET", "/api/playlists/"+p.ID+"/m3u?paths=relative", "", e.token)
	if res.StatusCode != 200 || !strings.HasPrefix(string(m3u), "#EXTM3U") {
		t.Fatalf("%s", m3u)
	}
	res, body = e.do("POST", "/api/playlists/import?name=Back", string(m3u)+"\n/elsewhere/missing.mp3\n", e.token)
	var ir library.ImportResult
	json.Unmarshal(body, &ir)
	if res.StatusCode != 201 || ir.Matched != 2 || len(ir.Unmatched) != 1 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	// smart playlist: lossless tracks, plays sort
	res, body = e.do("POST", "/api/playlists", `{"name":"Lossless","rules":{"match":"all","rules":[
		{"field":"lossless","op":"is","value":true},{"field":"added","op":"in_last","value":7}],"sort":"plays","order":"desc"}}`, e.token)
	json.Unmarshal(body, &p)
	if res.StatusCode != 201 || !p.Smart || p.TrackCount != 1 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if res, _ := e.do("POST", "/api/playlists/"+p.ID+"/items", `{"op":"clear"}`, e.token); res.StatusCode != 409 {
		t.Fatal("smart edit")
	}
	if res, _ := e.do("PATCH", "/api/playlists/"+p.ID, `{"rules":{"rules":[{"field":"genre","op":"contains","value":"%"}]}}`, e.token); res.StatusCode != 200 {
		t.Fatal("patch rules")
	}
	_, body = e.do("GET", "/api/playlists", "", e.token)
	if !strings.Contains(string(body), `"Back"`) {
		t.Fatalf("%s", body)
	}
	if res, _ := e.do("DELETE", "/api/playlists/"+p.ID, "", e.token); res.StatusCode != 204 {
		t.Fatal("delete")
	}
	if res, _ := e.do("GET", "/api/playlists/"+p.ID, "", e.token); res.StatusCode != 404 {
		t.Fatal("deleted")
	}
}
