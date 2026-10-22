package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

func TestNonLocalItemsAreReadOnly(t *testing.T) {
	e := newEnv(t)
	tr := flacTrack(t, e)
	ctx := context.Background()

	// default origin is local and is exposed in the API
	var got library.Track
	_, b := e.do("GET", "/api/tracks/"+tr.ID, "", e.token)
	json.Unmarshal(b, &got)
	if got.Origin != library.OriginLocal {
		t.Fatalf("default origin %q", got.Origin)
	}
	var tp library.Page[library.Track]
	_, b = e.do("GET", "/api/tracks?limit=50", "", e.token)
	json.Unmarshal(b, &tp)
	var local library.Track
	for _, x := range tp.Items {
		if x.ID != tr.ID {
			local = x
			break
		}
	}
	if local.ID == "" {
		t.Fatal("need a second track")
	}

	// mark the flac track, its album and an artist as coming from an online source
	if err := e.sc.SetOrigin(ctx, "track", tr.ID, "netease"); err != nil {
		t.Fatal(err)
	}
	if err := e.sc.SetOrigin(ctx, "album", tr.AlbumID, "netease"); err != nil {
		t.Fatal(err)
	}
	ar := tr.Artists[0].ID
	if err := e.sc.SetOrigin(ctx, "artist", ar, "qq"); err != nil {
		t.Fatal(err)
	}

	expect409 := func(method, path, body string, hdr ...string) {
		t.Helper()
		res, b := e.do(method, path, body, e.token, hdr...)
		if res.StatusCode != 409 || !strings.Contains(string(b), `"not_local_source"`) {
			t.Fatalf("%s %s: want 409 not_local_source, got %d %s", method, path, res.StatusCode, b)
		}
	}
	expect409("PUT", "/api/overrides/track/"+tr.ID, `{"title":"x"}`)
	expect409("DELETE", "/api/overrides/track/"+tr.ID, "")
	expect409("PUT", "/api/overrides/album/"+tr.AlbumID, `{"title":"x"}`)
	expect409("PUT", "/api/overrides/artist/"+ar, `{"name":"x"}`)
	expect409("PUT", "/api/tracks/"+tr.ID+"/lyrics", `{"lrc":"[00:01.00]a"}`)
	expect409("DELETE", "/api/tracks/"+tr.ID+"/lyrics", "")
	expect409("GET", "/api/tracks/"+tr.ID+"/lyrics/search", "")
	expect409("GET", "/api/tracks/"+tr.ID+"/match", "")
	expect409("GET", "/api/albums/"+tr.AlbumID+"/match", "")
	expect409("PUT", "/api/albums/"+tr.AlbumID+"/cover", "\x89PNG\r\n\x1a\n", "Content-Type", "image/png")
	expect409("DELETE", "/api/albums/"+tr.AlbumID+"/cover", "")
	expect409("POST", "/api/albums/merge", `{"target":"`+tr.AlbumID+`","sources":["al_other"]}`)
	expect409("POST", "/api/albums/split", `{"source":"`+tr.AlbumID+`","track_ids":["`+tr.ID+`"],"title":"x"}`)

	// unknown ids still 404, local items still editable
	if res, _ := e.do("PUT", "/api/overrides/track/tr_missing", `{"title":"x"}`, e.token); res.StatusCode != 404 {
		t.Fatalf("missing: %d", res.StatusCode)
	}
	if res, b := e.do("GET", "/api/tracks/"+local.ID+"/match", "", e.token); res.StatusCode != 200 {
		t.Fatalf("local match: %d %s", res.StatusCode, b)
	}

	// batch skips and reports non-local ids, applies the rest
	res, b := e.do("POST", "/api/overrides/batch", `{"type":"track","ids":["`+tr.ID+`","`+local.ID+`"],"patch":{"genre":"Jazz"}}`, e.token)
	if res.StatusCode != 200 {
		t.Fatalf("batch %d %s", res.StatusCode, b)
	}
	var br struct {
		Updated int      `json:"updated"`
		Skipped []string `json:"skipped_not_local"`
		Results []struct {
			ID, Error, Origin string
			OK                bool
		} `json:"results"`
	}
	json.Unmarshal(b, &br)
	if br.Updated != 1 || len(br.Skipped) != 1 || br.Skipped[0] != tr.ID || br.Results[0].Error != "not_local_source" || br.Results[0].Origin != "netease" {
		t.Fatalf("batch result %s", b)
	}
	_, b = e.do("GET", "/api/tracks/"+tr.ID, "", e.token)
	json.Unmarshal(b, &got)
	if got.Genre == "Jazz" || got.Origin != "netease" {
		t.Fatalf("non-local track modified: %s", b)
	}
}
