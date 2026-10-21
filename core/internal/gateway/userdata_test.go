package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

func TestHistoryFavoritesRatings(t *testing.T) {
	e := newEnv(t)
	tr := flacTrack(t, e)
	for _, b := range []string{`{"track_id":"nope"}`, `{}`, `{"track_id":"` + tr.ID + `","played_at":"2999-01-01T00:00:00Z"}`} {
		if res, _ := e.do("POST", "/api/scrobble", b, e.token); res.StatusCode/100 != 4 {
			t.Errorf("%s: %d", b, res.StatusCode)
		}
	}
	for i := 0; i < 2; i++ {
		if res, body := e.do("POST", "/api/scrobble", `{"track_id":"`+tr.ID+`","completed":true}`, e.token); res.StatusCode != 204 {
			t.Fatalf("%d %s", res.StatusCode, body)
		}
	}
	res, body := e.do("GET", "/api/history?limit=10", "", e.token)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"total":2`) {
		t.Fatalf("%s", body)
	}
	if _, body = e.do("GET", "/api/history/top?type=track&period=7d", "", e.token); !strings.Contains(string(body), `"plays":2`) {
		t.Fatalf("%s", body)
	}
	if res, _ := e.do("GET", "/api/history/top?type=bogus", "", e.token); res.StatusCode != 400 {
		t.Fatal("bad type")
	}
	if res, _ := e.do("PUT", "/api/favorites/track/"+tr.ID, "", e.token); res.StatusCode != 204 {
		t.Fatal("fav")
	}
	if res, _ := e.do("PUT", "/api/favorites/track/nope", "", e.token); res.StatusCode != 404 {
		t.Fatal("fav 404")
	}
	if res, _ := e.do("PUT", "/api/favorites/users/x", "", e.token); res.StatusCode != 400 {
		t.Fatal("fav type")
	}
	if res, _ := e.do("PUT", "/api/ratings/track/"+tr.ID, `{"rating":4}`, e.token); res.StatusCode != 204 {
		t.Fatal("rating")
	}
	if res, _ := e.do("PUT", "/api/ratings/track/"+tr.ID, `{"rating":9}`, e.token); res.StatusCode != 400 {
		t.Fatal("rating range")
	}
	var got library.Track
	_, body = e.do("GET", "/api/tracks/"+tr.ID, "", e.token)
	json.Unmarshal(body, &got)
	if got.PlayCount != 2 || !got.Favorite || got.Rating != 4 || got.LastPlayedAt == nil {
		t.Fatalf("%s", body)
	}
	var page library.Page[library.Track]
	_, body = e.do("GET", "/api/tracks?favorite=true&sort=plays&order=desc", "", e.token)
	json.Unmarshal(body, &page)
	if page.Total != 1 || page.Items[0].ID != tr.ID {
		t.Fatalf("%s", body)
	}
	if _, body = e.do("GET", "/api/favorites?type=track", "", e.token); !strings.Contains(string(body), tr.ID) {
		t.Fatalf("%s", body)
	}
	e.do("DELETE", "/api/favorites/track/"+tr.ID, "", e.token)
	if _, body = e.do("GET", "/api/tracks?favorite=true", "", e.token); !strings.Contains(string(body), `"total":0`) {
		t.Fatalf("%s", body)
	}
}
