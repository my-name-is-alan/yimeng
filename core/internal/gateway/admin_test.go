package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

func TestAdminDevicesRootsOverridesAlbums(t *testing.T) {
	e := newEnv(t)
	tr := flacTrack(t, e)
	other := e.pairAnother("phone")

	// non-admin forbidden
	if res, _ := e.do("PUT", "/api/settings/library-roots", `{"roots":["/tmp"]}`, other); res.StatusCode != 403 {
		t.Fatal("roots")
	}
	res, body := e.do("GET", "/api/settings/library-roots", "", e.token)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"roots"`) {
		t.Fatalf("%s", body)
	}
	root := e.sc.Roots()[0]
	if res, body = e.do("PUT", "/api/settings/library-roots", `{"roots":["`+root+`"]}`, e.token); res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if res, _ = e.do("PUT", "/api/settings/library-roots", `{"roots":["relative"]}`, e.token); res.StatusCode != 400 {
		t.Fatal("relative")
	}

	// override title
	if res, body = e.do("PUT", "/api/overrides/track/"+tr.ID, `{"title":"改名曲"}`, e.token); res.StatusCode != 200 || !strings.Contains(string(body), "改名曲") {
		t.Fatalf("override %d %s", res.StatusCode, body)
	}
	if res, body = e.do("GET", "/api/overrides/track/"+tr.ID, "", e.token); res.StatusCode != 200 || !strings.Contains(string(body), "改名曲") {
		t.Fatalf("%s", body)
	}
	if res, _ = e.do("PUT", "/api/overrides/track/"+tr.ID, `{"evil":1}`, e.token); res.StatusCode != 400 {
		t.Fatal("bad field")
	}
	// survives merge
	if err := e.sc.Merge(t.Context()); err != nil {
		t.Fatal(err)
	}
	_, body = e.do("GET", "/api/tracks/"+tr.ID, "", e.token)
	if !strings.Contains(string(body), "改名曲") {
		t.Fatalf("override lost after merge: %s", body)
	}

	// album split + merge
	var tp library.Page[library.Track]
	_, body = e.do("GET", "/api/tracks?limit=50", "", e.token)
	json.Unmarshal(body, &tp)
	var albumTrack library.Track
	for _, it := range tp.Items {
		if it.AlbumID != "" {
			albumTrack = it
			break
		}
	}
	if albumTrack.ID == "" {
		t.Fatalf("need album track: %s", body)
	}
	src := albumTrack.AlbumID
	res, body = e.do("POST", "/api/albums/split", `{"source":"`+src+`","track_ids":["`+albumTrack.ID+`"],"title":"拆出专辑"}`, e.token)
	var neu library.Album
	json.Unmarshal(body, &neu)
	if res.StatusCode != 200 || neu.Title != "拆出专辑" || neu.TrackCount != 1 {
		t.Fatalf("split %d %s", res.StatusCode, body)
	}
	res, body = e.do("POST", "/api/albums/merge", `{"target":"`+src+`","sources":["`+neu.ID+`"]}`, e.token)
	json.Unmarshal(body, &neu)
	if res.StatusCode != 200 || neu.ID != src {
		t.Fatalf("merge %d %s", res.StatusCode, body)
	}
	if res, _ = e.do("GET", "/api/albums/"+neu.ID, "", e.token); res.StatusCode != 200 {
		// src may still exist
	}

	// rename device + demote last admin refused
	var me struct{ ID string }
	_, body = e.do("GET", "/api/devices/me", "", e.token)
	json.Unmarshal(body, &me)
	if res, body = e.do("PATCH", "/api/devices/"+me.ID, `{"name":"客厅"}`, e.token); res.StatusCode != 200 || !strings.Contains(string(body), "客厅") {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if res, _ = e.do("PATCH", "/api/devices/"+me.ID, `{"admin":false}`, e.token); res.StatusCode != 409 {
		t.Fatal("last admin")
	}
	// promote phone then demote self
	var phoneID string
	_, body = e.do("GET", "/api/devices", "", e.token)
	var items []struct{ ID, Name string }
	if body[0] == '[' {
		json.Unmarshal(body, &items)
	} else {
		var wrap struct{ Items []struct{ ID, Name string } }
		json.Unmarshal(body, &wrap)
		items = wrap.Items
	}
	for _, d := range items {
		if d.Name == "phone" || d.Name == "客厅" && d.ID != me.ID {
			phoneID = d.ID
		}
		if d.Name == "phone" {
			phoneID = d.ID
		}
	}
	if phoneID == "" {
		t.Fatalf("phone: %s", body)
	}
	e.do("PATCH", "/api/devices/"+phoneID, `{"admin":true}`, e.token)
	if res, _ = e.do("PATCH", "/api/devices/"+me.ID, `{"admin":false}`, e.token); res.StatusCode != 200 {
		t.Fatal("demote after promote")
	}
}
