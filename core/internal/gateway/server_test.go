package gateway

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/app"
	"github.com/my-name-is-alan/yimeng/core/internal/auth"
	"github.com/my-name-is-alan/yimeng/core/internal/backup"
	"github.com/my-name-is-alan/yimeng/core/internal/events"
	"github.com/my-name-is-alan/yimeng/core/internal/infra/db"
	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/playback"
	"github.com/my-name-is-alan/yimeng/core/internal/system"
	"github.com/my-name-is-alan/yimeng/core/internal/transcode"
	"github.com/my-name-is-alan/yimeng/core/internal/userdata"
	"github.com/my-name-is-alan/yimeng/core/migrations"
)

type env struct {
	t     *testing.T
	srv   *httptest.Server
	sc    *library.Scanner
	auth  *auth.Service
	token string
	tc    *transcode.Transcoder
}

func (e *env) do(method, path, body, tok string, hdr ...string) (*http.Response, []byte) {
	e.t.Helper()
	var rb io.Reader
	if body != "" {
		rb = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, rb)
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	return res, b
}

func newEnv(t *testing.T) *env {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "music")
	os.MkdirAll(filepath.Join(root, "A"), 0o755)
	for _, f := range []string{"tagged.flac", "tagged.mp3"} {
		b, _ := os.ReadFile("../adapters/local/testdata/" + f)
		os.WriteFile(filepath.Join(root, "A", f), b, 0o644)
	}
	b, _ := os.ReadFile("../adapters/local/testdata/notags.mp3")
	os.WriteFile(filepath.Join(root, "loose.mp3"), b, 0o644)

	d, err := db.Open(filepath.Join(tmp, "lib.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	db.Migrate(context.Background(), d, migrations.FS)
	sc := library.NewScanner(d, library.Options{Roots: []string{root}, CoverDir: filepath.Join(tmp, "c")})
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := auth.New(d, 5*time.Minute, log)
	hub := events.NewHub()
	orch, err := playback.NewOrchestrator(context.Background(), d, hub, app.LocalLease(sc))
	if err != nil {
		t.Fatal(err)
	}
	tc, err := transcode.New(transcode.Options{CacheDir: filepath.Join(tmp, "cache"), Log: log})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewRouter(Deps{Library: &app.Library{Scanner: sc, BG: context.Background()},
		Playback: orch, Hub: hub, Auth: a, Log: log, Transcoder: tc, StreamPrefs: app.StreamPrefs{DB: d},
		Backup:    &backup.Service{DB: d, DBPath: filepath.Join(tmp, "lib.db"), Dir: filepath.Join(tmp, "backups")},
		UserData:  &userdata.Store{DB: d},
		Playlists: sc,
		Admin:     sc,
		System: &system.Collector{DB: d, DBPath: filepath.Join(tmp, "lib.db"), Roots: sc.Roots,
			CacheMaxBytes: 1 << 30, Transcoder: tc, Backup: &backup.Service{DB: d, DBPath: filepath.Join(tmp, "lib.db"), Dir: filepath.Join(tmp, "backups")},
			LatestScan: sc.Latest, Version: "test", SchemaVersion: db.Latest(migrations.FS), Started: time.Now(),
			Features: map[string]bool{"backup": true, "lyrics": true, "history": true, "playlists": true}},
		RestoreMaxBytes: 1 << 30, SchemaVersion: db.Latest(migrations.FS)}))
	t.Cleanup(srv.Close)
	e := &env{t: t, srv: srv, sc: sc, auth: a, tc: tc}
	code, _, _ := a.NewCode()
	res, body := e.do("POST", "/api/pair", `{"code":"`+code+`","name":"test","device_type":"web"}`, "")
	if res.StatusCode != 201 {
		t.Fatalf("pair: %d %s", res.StatusCode, body)
	}
	var pr struct{ Token string }
	json.Unmarshal(body, &pr)
	e.token = pr.Token
	return e
}

func TestAuthRequired(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"/api/albums", "/api/library/scan", "/api/devices", "/api/stream/x", "/api/nope"} {
		if res, _ := e.do("GET", p, "", ""); res.StatusCode != 401 {
			t.Errorf("%s without token: %d", p, res.StatusCode)
		}
		if res, _ := e.do("GET", p, "", "ymd_bogus"); res.StatusCode != 401 {
			t.Errorf("%s bad token: %d", p, res.StatusCode)
		}
	}
	// query token only for media
	if res, _ := e.do("GET", "/api/albums?access_token="+e.token, "", ""); res.StatusCode != 401 {
		t.Error("query token accepted on JSON route")
	}
	if res, _ := e.do("GET", "/healthz", "", ""); res.StatusCode != 200 {
		t.Error("healthz")
	}
	if res, _ := e.do("POST", "/api/pair", `{"code":"WRONG-CODE","name":"x"}`, ""); res.StatusCode != 401 {
		t.Error("wrong code")
	}
	res, body := e.do("POST", "/api/pair/request", "", "")
	if res.StatusCode != 202 || strings.Contains(string(body), `"code"`) {
		t.Errorf("pair request leaked code or failed: %d %s", res.StatusCode, body)
	}
	res, body = e.do("POST", "/api/pair/codes", "", e.token)
	if res.StatusCode != 201 || !strings.Contains(string(body), `"code"`) {
		t.Errorf("authed code: %d %s", res.StatusCode, body)
	}
	res, body = e.do("GET", "/api/devices/me", "", e.token)
	var me auth.Device
	json.Unmarshal(body, &me)
	if res.StatusCode != 200 || me.Name != "test" {
		t.Fatal(string(body))
	}
	if res, _ := e.do("DELETE", "/api/devices/"+me.ID, "", e.token); res.StatusCode != 204 {
		t.Fatal("revoke")
	}
	if res, _ := e.do("GET", "/api/albums", "", e.token); res.StatusCode != 401 {
		t.Fatal("revoked token still works")
	}
}

func TestScanCatalogStream(t *testing.T) {
	e := newEnv(t)
	tok := e.token
	if res, _ := e.do("GET", "/api/library/scan", "", tok); res.StatusCode != 404 {
		t.Fatal("latest before scan")
	}
	if res, _ := e.do("POST", "/api/library/scan", `{"root":"/etc"}`, tok); res.StatusCode != 400 {
		t.Fatal("bad root")
	}
	if res, _ := e.do("POST", "/api/library/scan", `{"bogus":1}`, tok); res.StatusCode != 400 {
		t.Fatal("bad body")
	}
	res, body := e.do("POST", "/api/library/scan", "", tok)
	if res.StatusCode != 202 {
		t.Fatalf("start %d %s", res.StatusCode, body)
	}
	var job library.Job
	json.Unmarshal(body, &job)
	e.sc.Wait()
	_, body = e.do("GET", "/api/library/scan/"+job.ID, "", tok)
	json.Unmarshal(body, &job)
	if job.Status != "completed" || job.Added != 3 {
		t.Fatalf("%+v", job)
	}

	var albums library.Page[library.Album]
	res, body = e.do("GET", "/api/albums?limit=10&sort=title&order=desc", "", tok)
	json.Unmarshal(body, &albums)
	if res.StatusCode != 200 || albums.Total != 1 || albums.Items[0].TrackCount != 1 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if res, _ := e.do("GET", "/api/albums?sort=evil", "", tok); res.StatusCode != 400 {
		t.Fatal("bad sort")
	}
	if res, _ := e.do("GET", "/api/albums?limit=-1", "", tok); res.StatusCode != 400 {
		t.Fatal("bad limit")
	}
	var page AlbumPage
	_, body = e.do("GET", "/api/albums/"+albums.Items[0].ID, "", tok)
	json.Unmarshal(body, &page)
	if len(page.Tracks) != 1 || page.Credits == nil {
		t.Fatalf("album page %s", body)
	}
	trackID := page.Tracks[0].ID

	var artists library.Page[library.Artist]
	_, body = e.do("GET", "/api/artists?q=测试", "", tok)
	json.Unmarshal(body, &artists)
	if artists.Total != 1 {
		t.Fatalf("artists %s", body)
	}
	if res, body := e.do("GET", "/api/artists/"+artists.Items[0].ID, "", tok); res.StatusCode != 200 || !strings.Contains(string(body), "译梦测试专辑") {
		t.Fatalf("artist page %s", body)
	}
	var sr SearchResult
	_, body = e.do("GET", "/api/search?q=梦", "", tok)
	json.Unmarshal(body, &sr)
	if len(sr.Albums) != 1 || len(sr.Tracks) != 1 {
		t.Fatalf("search %s", body)
	}
	if res, _ := e.do("GET", "/api/tracks?album_id="+albums.Items[0].ID, "", tok); res.StatusCode != 200 {
		t.Fatal("tracks")
	}
	if res, _ := e.do("GET", "/api/tracks/nope", "", tok); res.StatusCode != 404 {
		t.Fatal("404")
	}

	// cover (query token allowed for <img>)
	res, body = e.do("GET", "/api/albums/"+albums.Items[0].ID+"/cover?access_token="+tok, "", "")
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/png" || len(body) == 0 {
		t.Fatalf("cover %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}

	// stream with Range
	full, _ := os.ReadFile("../adapters/local/testdata/tagged.flac")
	res, body = e.do("GET", "/api/stream/"+trackID, "", tok, "Range", "bytes=0-99")
	if res.StatusCode != 206 || len(body) != 100 || res.Header.Get("Content-Type") != "audio/flac" ||
		res.Header.Get("Accept-Ranges") != "bytes" || string(body) != string(full[:100]) {
		t.Fatalf("range: %d len=%d ct=%s", res.StatusCode, len(body), res.Header.Get("Content-Type"))
	}
	res, body = e.do("GET", "/api/stream/"+trackID+"?access_token="+tok, "", "")
	if res.StatusCode != 200 || len(body) != len(full) {
		t.Fatalf("full stream %d %d", res.StatusCode, len(body))
	}
	for _, bad := range []string{"..%2F..%2Fetc%2Fpasswd", "%2Fetc%2Fpasswd", "nope"} {
		if res, _ := e.do("GET", "/api/stream/"+bad, "", tok); res.StatusCode != 404 {
			t.Errorf("stream %s: %d", bad, res.StatusCode)
		}
	}
}

func TestSplitSettingsAPI(t *testing.T) {
	e := newEnv(t)
	res, body := e.do("GET", "/api/settings/artist-split", "", e.token)
	if res.StatusCode != 200 || !strings.Contains(string(body), "AC/DC") {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	res, body = e.do("PUT", "/api/settings/artist-split", `{"exceptions":["Simon & Garfunkel"],"split_ampersand":true,"extra_separators":[" x "]}`, e.token)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"split_ampersand":true`) {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if res, _ := e.do("PUT", "/api/settings/artist-split", `{"extra_separators":["0123456789"]}`, e.token); res.StatusCode != 400 {
		t.Fatal("validation")
	}
	if res, _ := e.do("PUT", "/api/settings/artist-split", `{}`, ""); res.StatusCode != 401 {
		t.Fatal("auth")
	}
}

func TestCreditsAndEnrich(t *testing.T) {
	e := newEnv(t)
	e.do("POST", "/api/library/scan", "", e.token)
	e.sc.Wait()
	if res, _ := e.do("POST", "/api/metadata/enrich", "", e.token); res.StatusCode != 503 {
		t.Fatal("enrich without provider should be 503")
	}
	var tracks library.Page[library.Track]
	_, body := e.do("GET", "/api/tracks?q=梦里", "", e.token)
	json.Unmarshal(body, &tracks)
	id := tracks.Items[0].ID
	e.sc.DB().Exec(`INSERT INTO credits(id, track_id, role, name, source) VALUES ('c1', ?, 'composer', '周杰伦', 'musicbrainz')`, id)
	if res, body := e.do("GET", "/api/tracks/"+id+"/credits", "", e.token); res.StatusCode != 200 || !strings.Contains(string(body), "composer") {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	var page AlbumPage
	_, body = e.do("GET", "/api/albums/"+tracks.Items[0].AlbumID, "", e.token)
	json.Unmarshal(body, &page)
	if len(page.Credits) != 1 || page.Credits[0].Name != "周杰伦" {
		t.Fatalf("album credits %s", body)
	}
	if res, _ := e.do("GET", "/api/tracks/nope/credits", "", e.token); res.StatusCode != 404 {
		t.Fatal("404")
	}
}
