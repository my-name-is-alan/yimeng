package gateway

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/covers"
	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/metadata"
	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

type imgMatcher struct {
	fakeMP
	base  string
	calls *atomic.Int64
}

func (m imgMatcher) LookupArtist(_ context.Context, q metadata.ArtistQuery) ([]metadata.ArtistInfo, error) {
	m.calls.Add(1)
	return []metadata.ArtistInfo{
		{Provider: "spotify", Name: q.Name + " Tribute Band", ImageURLs: []string{m.base + "/wrong.png"}},
		{Provider: "spotify", Name: strings.ToUpper(q.Name), ImageURLs: []string{m.base + "/artist.png"}},
	}, nil
}

func testPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 640, 640))
	for i := 0; i < 640; i++ {
		img.Set(i, i, color.RGBA{10, 120, 200, 255})
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func TestImageCacheEndpoints(t *testing.T) {
	var hits atomic.Int64
	body := testPNG()
	cdn := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/wrong.png" {
			t.Error("wrong artist image fetched")
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(body)
	}))
	defer cdn.Close()
	var calls atomic.Int64
	var cache *covers.Cache
	e := newEnvWith(t, func(d *Deps, _ *sql.DB, _ *library.Scanner) {
		dir, _ := os.MkdirTemp("", "ymimg")
		t.Cleanup(func() { time.Sleep(200 * time.Millisecond); os.RemoveAll(dir) }) // background prewarm may still write
		cache = &covers.Cache{Dir: dir, AllowHosts: []string{"127.0.0.1"}, AnyPort: true, Client: cdn.Client(), TTL: time.Hour, MaxBytes: 50 << 20}
		d.Covers = cache
		d.Matcher = imgMatcher{base: cdn.URL, calls: &calls}
		d.ArtistImageAuto = true
	})
	tr := flacTrack(t, e)

	// registered remote image: public, sized, cached once
	key, ok := cache.Register(cdn.URL + "/cover.png")
	if !ok {
		t.Fatal("register")
	}
	for i := 0; i < 3; i++ {
		res, b := e.do("GET", "/api/v1/images/"+key+"?size=120&format=jpeg", "", "")
		if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/jpeg" || len(b) == 0 {
			t.Fatalf("image: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("cdn hit %d times", hits.Load())
	}
	for _, k := range []string{"0123456789abcdef0123456789abcdef", "..%2F..%2Fetc", "nothex"} {
		if res, _ := e.do("GET", "/api/v1/images/"+k, "", ""); res.StatusCode != 404 {
			t.Errorf("key %s: %d", k, res.StatusCode)
		}
	}
	// arbitrary URLs cannot be fetched: only registered keys exist
	if res, _ := e.do("POST", "/api/v1/images/"+key, "", ""); res.StatusCode == 200 {
		t.Error("POST accepted")
	}

	// local artist: auto-matched exact-name picture (background, paced)
	ar := tr.Artists[0].ID
	e.do("GET", "/api/v1/artists/"+ar+"/image", "", e.token)
	deadline := time.Now().Add(8 * time.Second)
	var res *http.Response
	for time.Now().Before(deadline) {
		res, _ = e.do("GET", "/api/v1/artists/"+ar+"/image?size=300", "", e.token)
		if _, ok := cache.Aliased("artist:" + ar); ok && res.StatusCode == 200 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if _, ok := cache.Aliased("artist:" + ar); !ok || res.StatusCode != 200 {
		t.Fatalf("artist image not matched: %d", res.StatusCode)
	}
	if calls.Load() != 1 {
		t.Errorf("lookups %d", calls.Load())
	}
	// artist JSON carries a cover with placeholder data once cached
	_, b := e.do("GET", "/api/v1/artists/"+ar, "", e.token)
	if !strings.Contains(string(b), `"image_url"`) {
		t.Errorf("artist json: %s", b)
	}

	// Subsonic: ar- cover + artist info with public image URLs
	_, alice, _ := e.accounts()
	k := e.mkCred(alice, `{"kind":"api_key","name":"img","scopes":["read"]}`)
	q := url.Values{"apiKey": {k.Secret}, "id": {"ar-" + ar}, "size": {"120"}}
	res, _ = e.do("GET", "/rest/getCoverArt?"+q.Encode(), "", "")
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "image/") {
		t.Fatalf("subsonic ar cover: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	info := sub(e, "getArtistInfo2", url.Values{"apiKey": {k.Secret}, "id": {ar}})
	ai, _ := info["artistInfo2"].(map[string]any)
	if ai == nil || !strings.Contains(ai["largeImageUrl"].(string), "/api/v1/images/") {
		t.Fatalf("artistInfo2 %v", info)
	}
	if r := sub(e, "getArtist", url.Values{"apiKey": {k.Secret}, "id": {ar}}); r["artist"].(map[string]any)["coverArt"] != "ar-"+ar {
		t.Errorf("getArtist coverArt %v", r)
	}
	if r := sub(e, "getAlbumInfo2", url.Values{"apiKey": {k.Secret}, "id": {tr.AlbumID}}); r["status"] != "ok" {
		t.Errorf("albumInfo2 %v", r)
	}

	// admin stats + clear
	_, b = e.do("GET", "/api/v1/settings/cache", "", e.token)
	var st struct {
		Images covers.Stats `json:"images"`
	}
	json.Unmarshal(b, &st)
	if st.Images.Originals < 2 || st.Images.Hits < 1 || st.Images.Bytes == 0 {
		t.Fatalf("stats %s", b)
	}
	if res, _ := e.do("DELETE", "/api/v1/settings/cache?kind=images", "", alice); res.StatusCode != 403 {
		t.Errorf("member clear: %d", res.StatusCode)
	}
	res, b = e.do("DELETE", "/api/v1/settings/cache?kind=images", "", e.token)
	if res.StatusCode != 200 {
		t.Fatalf("clear %d %s", res.StatusCode, b)
	}
	// URL records survive: refetched on demand
	if res, _ := e.do("GET", "/api/v1/images/"+key, "", ""); res.StatusCode != 200 || hits.Load() < 3 {
		t.Errorf("refetch after clear: %d hits=%d", res.StatusCode, hits.Load())
	}
}

func TestDecorateOnlineArtistsAliases(t *testing.T) {
	c := &covers.Cache{Dir: t.TempDir()}
	d := Deps{Covers: c}
	as := []online.Artist{{ID: "netease:6452", PictureURL: "http://p1.music.126.net/x.jpg"}, {ID: "qq:1", PictureURL: "https://evil.example/a.jpg"}}
	d.decorateOnlineArtists(as)
	if as[0].Cover == nil || !strings.HasPrefix(as[0].Cover.URL, "/api/v1/images/") || as[0].Cover.SourceURL != "https://p1.music.126.net/x.jpg" {
		t.Fatalf("cover %+v", as[0].Cover)
	}
	if u, ok := c.Aliased("artist:netease:6452"); !ok || u != "https://p1.music.126.net/x.jpg" {
		t.Fatal("alias not remembered")
	}
	if as[1].Cover == nil || strings.HasPrefix(as[1].Cover.URL, "/api/") {
		t.Fatalf("non-allow-listed host must stay a plain CDN URL: %+v", as[1].Cover)
	}
	if _, ok := c.Aliased("artist:qq:1"); ok {
		t.Fatal("disallowed host aliased")
	}
}

func TestDecorateOnlinePlaylistUsesImageCache(t *testing.T) {
	dir := t.TempDir()
	d := Deps{Covers: &covers.Cache{Dir: dir, AllowHosts: []string{"p1.music.126.net", "music-file.y.qq.com"}, TTL: time.Hour, MaxBytes: 1 << 20}}
	p := online.Playlist{CoverURL: "https://music-file.y.qq.com/x/pl.jpg", Tracks: []online.Track{{CoverURL: "http://p1.music.126.net/a.jpg"}}}
	d.decorateOnlinePlaylist(&p)
	if p.Cover == nil || !strings.HasPrefix(p.Cover.URL, "/api/v1/images/") {
		t.Fatalf("playlist cover %+v", p.Cover)
	}
	if c := p.Tracks[0].Cover; c == nil || !strings.HasPrefix(c.URL, "/api/v1/images/") {
		t.Fatalf("track cover %+v", c)
	}
}
