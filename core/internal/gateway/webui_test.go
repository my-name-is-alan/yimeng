package gateway

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/infra/logging"
	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/lyrics/providers"
	"github.com/my-name-is-alan/yimeng/core/internal/metadata"
)

var testRing = logging.NewRing(50)

func testLyricsSearch(_ context.Context, q, title, artist string) ([]providers.Candidate, error) {
	return []providers.Candidate{{ID: 1, Provider: "lrclib", Title: title + q, Artist: artist, Synced: true, SyncedLyrics: "[00:01.00]hi"}}, nil
}

type fakeMatcher struct{}

func (fakeMatcher) ID() metadata.ProviderID { return metadata.MusicBrainz }
func (fakeMatcher) Capabilities() metadata.Capability {
	return metadata.CapTrackTags | metadata.CapAlbumInfo
}
func (fakeMatcher) Health(context.Context) metadata.HealthState { return metadata.HealthOK }
func (fakeMatcher) LookupArtist(context.Context, metadata.ArtistQuery) ([]metadata.ArtistInfo, error) {
	return nil, metadata.ErrNotSupported
}
func (fakeMatcher) CoverArt(context.Context, metadata.AlbumQuery) ([]metadata.CoverCandidate, error) {
	return nil, metadata.ErrNotSupported
}
func (fakeMatcher) FetchCover(context.Context, metadata.CoverCandidate) (io.ReadCloser, error) {
	return nil, metadata.ErrNotSupported
}
func (fakeMatcher) Lyrics(context.Context, metadata.TrackQuery) (*metadata.Lyrics, error) {
	return nil, metadata.ErrNotSupported
}
func (fakeMatcher) LookupTrack(_ context.Context, q metadata.TrackQuery) ([]metadata.TrackMatch, error) {
	return []metadata.TrackMatch{{Provider: metadata.MusicBrainz, ExternalID: "rec-1", Score: 0.93, Title: q.Title + " (MB)", Artist: q.Artist, Year: 2003}}, nil
}
func (fakeMatcher) LookupAlbum(_ context.Context, q metadata.AlbumQuery) ([]metadata.AlbumInfo, error) {
	return []metadata.AlbumInfo{{Provider: metadata.MusicBrainz, ExternalID: "rel-1", Title: q.Title, AlbumArtist: q.AlbumArtist, Year: q.Year}}, nil
}

func TestWebUIServed(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/admin/", "/admin/js/app.js", "/admin/app.css", "/admin/some/route"} {
		res, body := e.do("GET", p, "", "")
		if res.StatusCode != 200 || len(body) == 0 {
			t.Fatalf("%s: %d", p, res.StatusCode)
		}
		if p == "/admin/js/app.js" && !strings.Contains(res.Header.Get("Content-Type"), "javascript") {
			t.Fatalf("ct %s", res.Header.Get("Content-Type"))
		}
	}
	if res, _ := e.do("GET", "/admin/missing.js", "", ""); res.StatusCode != 404 {
		t.Fatal("missing asset should 404")
	}
	req, _ := http.NewRequest("GET", e.srv.URL+"/", nil)
	res, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err != nil || res.StatusCode != 302 || res.Header.Get("Location") != "/admin/" {
		t.Fatalf("root redirect %v %v", err, res)
	}
}

func TestUserLyricsBindAndOffset(t *testing.T) {
	e := newEnv(t)
	tr := flacTrack(t, e)
	other := e.pairAnother("phone")
	body := `{"lrc":"[00:02.00]第一句\n[00:04.00]第二句","offset_ms":500,"source":"lrclib"}`
	if res, _ := e.do("PUT", "/api/tracks/"+tr.ID+"/lyrics", body, other); res.StatusCode != 403 {
		t.Fatal("non-admin bind must be forbidden")
	}
	res, b := e.do("PUT", "/api/tracks/"+tr.ID+"/lyrics", body, e.token)
	var l library.TrackLyrics
	json.Unmarshal(b, &l)
	if res.StatusCode != 200 || l.Source != "user:lrclib" || !l.Synced || l.Lines[0].TimeMs != 1500 {
		t.Fatalf("%d %s", res.StatusCode, b)
	}
	if res, b = e.do("GET", "/api/tracks/"+tr.ID+"/lyrics/user", "", e.token); res.StatusCode != 200 || !strings.Contains(string(b), `"offset_ms":500`) {
		t.Fatalf("%d %s", res.StatusCode, b)
	}
	if res, _ = e.do("PUT", "/api/tracks/"+tr.ID+"/lyrics", `{"lrc":""}`, e.token); res.StatusCode != 400 {
		t.Fatal("empty lrc")
	}
	if res, _ = e.do("PUT", "/api/tracks/nope/lyrics", body, e.token); res.StatusCode != 404 {
		t.Fatal("unknown track")
	}
	// survives a rescan/merge
	if err := e.sc.Merge(t.Context()); err != nil {
		t.Fatal(err)
	}
	_, b = e.do("GET", "/api/tracks/"+tr.ID+"/lyrics", "", e.token)
	if !strings.Contains(string(b), "第一句") {
		t.Fatalf("lost after merge: %s", b)
	}
	if res, _ = e.do("DELETE", "/api/tracks/"+tr.ID+"/lyrics", "", e.token); res.StatusCode != 204 {
		t.Fatal("unbind")
	}
	if res, _ = e.do("DELETE", "/api/tracks/"+tr.ID+"/lyrics", "", e.token); res.StatusCode != 404 {
		t.Fatal("double unbind")
	}
	// search candidates (fake provider), defaults to the track's tags
	res, b = e.do("GET", "/api/tracks/"+tr.ID+"/lyrics/search", "", e.token)
	if res.StatusCode != 200 || !strings.Contains(string(b), tr.Title) {
		t.Fatalf("%d %s", res.StatusCode, b)
	}
	if res, _ = e.do("GET", "/api/tracks/"+tr.ID+"/lyrics/search", "", other); res.StatusCode != 403 {
		t.Fatal("search admin only")
	}
}

func TestAlbumCoverUpload(t *testing.T) {
	e := newEnv(t)
	tr := flacTrack(t, e)
	if tr.AlbumID == "" {
		t.Skip("no album")
	}
	png, err := os.ReadFile("../adapters/local/testdata/cover.png")
	if err != nil {
		t.Fatal(err)
	}
	if res, b := e.do("PUT", "/api/albums/"+tr.AlbumID+"/cover", "not an image", e.token); res.StatusCode != 400 {
		t.Fatalf("%d %s", res.StatusCode, b)
	}
	res, b := e.do("PUT", "/api/albums/"+tr.AlbumID+"/cover", string(png), e.token, "Content-Type", "image/png")
	if res.StatusCode != 200 || !strings.Contains(string(b), `"has_cover":true`) {
		t.Fatalf("%d %s", res.StatusCode, b)
	}
	if err := e.sc.Merge(t.Context()); err != nil {
		t.Fatal(err)
	}
	res, b = e.do("GET", "/api/albums/"+tr.AlbumID+"/cover", "", e.token)
	if res.StatusCode != 200 || string(b) != string(png) {
		t.Fatalf("cover after merge: %d %d bytes", res.StatusCode, len(b))
	}
	if res, _ = e.do("DELETE", "/api/albums/"+tr.AlbumID+"/cover", "", e.token); res.StatusCode != 204 {
		t.Fatal("clear cover")
	}
}

func TestMatchBatchLogsRuntime(t *testing.T) {
	e := newEnv(t)
	tr := flacTrack(t, e)
	res, b := e.do("GET", "/api/tracks/"+tr.ID+"/match", "", e.token)
	if res.StatusCode != 200 || !strings.Contains(string(b), `"score":0.93`) || !strings.Contains(string(b), "(MB)") {
		t.Fatalf("%d %s", res.StatusCode, b)
	}
	if tr.AlbumID != "" {
		res, b = e.do("GET", "/api/albums/"+tr.AlbumID+"/match", "", e.token)
		if res.StatusCode != 200 || !strings.Contains(string(b), `"score":`) {
			t.Fatalf("%d %s", res.StatusCode, b)
		}
	}
	res, b = e.do("POST", "/api/overrides/batch", `{"type":"track","ids":["`+tr.ID+`","missing"],"patch":{"genre":"摇滚"}}`, e.token)
	if res.StatusCode != 200 || !strings.Contains(string(b), `"updated":1`) {
		t.Fatalf("%d %s", res.StatusCode, b)
	}
	if _, b = e.do("GET", "/api/tracks/"+tr.ID, "", e.token); !strings.Contains(string(b), "摇滚") {
		t.Fatalf("%s", b)
	}
	if res, _ = e.do("POST", "/api/overrides/batch", `{"type":"track","ids":["`+tr.ID+`"],"patch":{"evil":1}}`, e.token); res.StatusCode != 400 {
		t.Fatal("bad field")
	}
	other := e.pairAnother("phone")
	if res, _ = e.do("POST", "/api/overrides/batch", `{"type":"track","ids":["x"],"patch":{"genre":"x"}}`, other); res.StatusCode != 403 {
		t.Fatal("batch admin only")
	}
	slog.New(testRing.Wrap(slog.NewTextHandler(io.Discard, nil))).Warn("hello from test")
	res, b = e.do("GET", "/api/system/logs?level=warn", "", e.token)
	if res.StatusCode != 200 || !strings.Contains(string(b), "hello from test") {
		t.Fatalf("%d %s", res.StatusCode, b)
	}
	if res, _ = e.do("GET", "/api/system/logs", "", other); res.StatusCode != 403 {
		t.Fatal("logs admin only")
	}
	if res, b = e.do("GET", "/api/settings/runtime", "", e.token); res.StatusCode != 200 || !strings.Contains(string(b), "watch_mode") {
		t.Fatalf("%d %s", res.StatusCode, b)
	}
}
