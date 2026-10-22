package spotify

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/metadata"
)

type fake struct {
	tokens, calls atomic.Int32
	expire        atomic.Bool // next API call answers 401 once
	lastQ         atomic.Value
}

func (f *fake) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/token", func(w http.ResponseWriter, r *http.Request) {
		id, sec, ok := r.BasicAuth()
		if !ok || id != "cid" || sec != "csecret" || r.FormValue("grant_type") != "client_credentials" {
			w.WriteHeader(400)
			return
		}
		n := f.tokens.Add(1)
		w.Write([]byte(`{"access_token":"tok` + string(rune('0'+n)) + `","token_type":"Bearer","expires_in":3600}`))
	})
	serve := func(name string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			f.calls.Add(1)
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer tok") {
				w.WriteHeader(401)
				return
			}
			if f.expire.CompareAndSwap(true, false) {
				w.WriteHeader(401)
				return
			}
			b, err := os.ReadFile("testdata/" + name)
			if err != nil {
				t.Fatal(err)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(b)
		}
	}
	mux.HandleFunc("GET /v1/search", func(w http.ResponseWriter, r *http.Request) {
		f.lastQ.Store(r.URL.Query().Get("q"))
		switch r.URL.Query().Get("type") {
		case "track":
			serve("search_track.json")(w, r)
		case "album":
			serve("search_album.json")(w, r)
		default:
			w.Write([]byte(`{"artists":{"items":[]}}`))
		}
	})
	mux.HandleFunc("GET /v1/artists/{id}", serve("artist.json"))
	mux.HandleFunc("GET /v1/albums/{id}", serve("album.json"))
	mux.HandleFunc("GET /v1/tracks/{id}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
	return mux
}

func newP(t *testing.T, creds Creds) (*Provider, *fake) {
	f := &fake{}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	return New(Config{APIBase: srv.URL + "/v1", AuthBase: srv.URL + "/api", HTTP: srv.Client(), Creds: creds}), f
}

func ok() (string, string, bool) { return "cid", "csecret", true }

func TestUnconfigured(t *testing.T) {
	p, f := newP(t, func() (string, string, bool) { return "", "", false })
	if _, err := p.LookupTrack(context.Background(), metadata.TrackQuery{Title: "x"}); !errors.Is(err, metadata.ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	if f.calls.Load() != 0 || f.tokens.Load() != 0 {
		t.Fatal("no network without credentials")
	}
}

func TestBadCredentials(t *testing.T) {
	p, _ := newP(t, func() (string, string, bool) { return "cid", "wrong", true })
	_, err := p.LookupTrack(context.Background(), metadata.TrackQuery{Title: "晴天"})
	if !errors.Is(err, metadata.ErrAuthExpired) || strings.Contains(err.Error(), "wrong") {
		t.Fatalf("got %v", err)
	}
}

func TestLookupTrack(t *testing.T) {
	p, f := newP(t, ok)
	ms, err := p.LookupTrack(context.Background(), metadata.TrackQuery{Title: "晴天", Artist: "Jay Chou", DurationMs: 269000})
	if err != nil || len(ms) != 3 {
		t.Fatalf("%v %d", err, len(ms))
	}
	m := ms[0]
	if m.Title != "晴天" || m.Artist != "Jay Chou" || m.Album != "葉惠美" || m.ISRC != "TWK970300503" || m.Year != 2003 ||
		m.ReleaseDate != "2003-07-29" || m.Popularity == 0 || !strings.HasPrefix(m.CoverURL, "https://i.scdn.co/") || m.Genre != "mandopop" {
		t.Fatalf("%+v", m)
	}
	if m.Score < 0.9 || ms[1].Score >= m.Score {
		t.Fatalf("scores %v %v", m.Score, ms[1].Score)
	}
	if q, _ := f.lastQ.Load().(string); q != `track:"晴天" artist:"Jay Chou"` {
		t.Fatalf("query %q", q)
	}
	// token cached across calls
	p.LookupTrack(context.Background(), metadata.TrackQuery{Title: "晴天"})
	if f.tokens.Load() != 1 {
		t.Fatalf("tokens %d", f.tokens.Load())
	}
}

func TestISRCAndRefreshOn401(t *testing.T) {
	p, f := newP(t, ok)
	p.LookupTrack(context.Background(), metadata.TrackQuery{Title: "a"})
	f.expire.Store(true)
	ms, err := p.LookupTrack(context.Background(), metadata.TrackQuery{ISRC: "twk970300503"})
	if err != nil || len(ms) == 0 || ms[0].Score != 1 {
		t.Fatalf("%v %+v", err, ms)
	}
	if q, _ := f.lastQ.Load().(string); q != "isrc:twk970300503" {
		t.Fatalf("query %q", q)
	}
	if f.tokens.Load() != 2 {
		t.Fatalf("expected a token refresh, got %d tokens", f.tokens.Load())
	}
}

func TestLookupAlbumAndCover(t *testing.T) {
	p, _ := newP(t, ok)
	as, err := p.LookupAlbum(context.Background(), metadata.AlbumQuery{Title: "葉惠美", AlbumArtist: "Jay Chou"})
	if err != nil || len(as) == 0 {
		t.Fatal(err)
	}
	a := as[0]
	if a.Title != "葉惠美" || a.Label == "" || a.UPC == "" || a.ReleaseDate != "2003-07-29" || a.TrackCount != 11 || a.CoverURL == "" {
		t.Fatalf("%+v", a)
	}
	cs, err := p.CoverArt(context.Background(), metadata.AlbumQuery{Title: "葉惠美"})
	if err != nil || len(cs) == 0 || cs[0].Provider != metadata.Spotify {
		t.Fatal(err, cs)
	}
}

func TestArtist(t *testing.T) {
	p, _ := newP(t, ok)
	as, err := p.LookupArtist(context.Background(), metadata.ArtistQuery{ExternalIDs: map[metadata.ProviderID]string{metadata.Spotify: "2elBjNSdBE2Y3f0j1mjrql"}})
	if err != nil || len(as) != 1 || as[0].Name != "Jay Chou" || len(as[0].Genres) == 0 || len(as[0].ImageURLs) != 1 {
		t.Fatalf("%v %+v", err, as)
	}
}

func TestCoverHostAllowlist(t *testing.T) {
	for u, want := range map[string]bool{
		"https://i.scdn.co/image/abc":           true,
		"https://image-cdn-ak.spotifycdn.com/x": true,
		"http://i.scdn.co/image/abc":            false,
		"https://evil.example/i.scdn.co":        false,
		"https://127.0.0.1/x":                   false,
	} {
		if AllowedImage(u) != want {
			t.Errorf("%s", u)
		}
	}
	p, _ := newP(t, ok)
	if _, err := p.FetchCover(context.Background(), metadata.CoverCandidate{URL: "http://169.254.169.254/"}); err == nil {
		t.Fatal("must refuse non-CDN urls")
	}
}

func TestMultiSkipsUnconfigured(t *testing.T) {
	sp, _ := newP(t, func() (string, string, bool) { return "", "", false })
	good, _ := newP(t, ok)
	ms, err := metadata.Multi{sp, good}.LookupTrack(context.Background(), metadata.TrackQuery{Title: "晴天"})
	if err != nil || len(ms) != 3 {
		t.Fatalf("%v %d", err, len(ms))
	}
	if _, err := (metadata.Multi{sp}).LookupTrack(context.Background(), metadata.TrackQuery{Title: "x"}); !errors.Is(err, metadata.ErrUnavailable) {
		t.Fatalf("all unconfigured → ErrUnavailable, got %v", err)
	}
}

func TestChineseArtistAliases(t *testing.T) {
	f := &fake{}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	p := New(Config{APIBase: srv.URL + "/v1", AuthBase: srv.URL, HTTP: srv.Client(), Creds: ok,
		Aliases: func(_ context.Context, name string) []string {
			if name == "周杰伦" {
				return []string{"Jay Chou", "Zhou Jielun"}
			}
			return nil
		}})
	ms, err := p.LookupTrack(context.Background(), metadata.TrackQuery{Title: "晴天", Artist: "周杰伦", DurationMs: 269000})
	if err != nil || len(ms) == 0 {
		t.Fatal(err)
	}
	if ms[0].Score < 0.9 {
		t.Fatalf("周杰伦 vs Jay Chou should score high, got %.2f", ms[0].Score)
	}
	if q, _ := f.lastQ.Load().(string); !strings.Contains(q, `artist:"`) {
		t.Fatalf("query %q", q)
	}
	// traditional vs simplified album title + alias
	as, err := p.LookupAlbum(context.Background(), metadata.AlbumQuery{Title: "叶惠美", AlbumArtist: "周杰伦", Year: 2003})
	if err != nil || len(as) == 0 || as[0].Score < 0.9 {
		t.Fatalf("%v %+v", err, as)
	}
	if sim("叶惠美", "葉惠美") < 0.9 || sim("晴天", "七里香") != 0 {
		t.Fatal("pinyin sim")
	}
}
