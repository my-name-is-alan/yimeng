package musicbrainz

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/metadata"
)

const recJSON = `{"id":"rec-1","title":"晴天","length":269000,"isrcs":["TWK970300114"],
 "artist-credit":[{"name":"周杰伦","joinphrase":"","artist":{"id":"art-1","name":"周杰伦"}}],
 "releases":[{"id":"rel-x","title":"Greatest Hits","date":"2010"},{"id":"rel-1","title":"叶惠美","date":"2003-07-31","artist-credit":[{"name":"周杰伦","joinphrase":""}]}],
 "relations":[
  {"type":"producer","target-type":"artist","artist":{"id":"a-p","name":"周杰伦"}},
  {"type":"instrument","target-type":"artist","attributes":["guitar"],"artist":{"id":"a-g","name":"蔡科俊"}},
  {"type":"performance","target-type":"work","work":{"id":"w1","title":"晴天","relations":[
    {"type":"composer","target-type":"artist","artist":{"id":"art-1","name":"周杰伦"}},
    {"type":"lyricist","target-type":"artist","artist":{"id":"art-1","name":"周杰伦"}}]}}]}`

type fakeMB struct {
	mu    sync.Mutex
	times []time.Time
	paths []string
	uas   []string
}

func (f *fakeMB) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.times = append(f.times, time.Now())
		f.paths = append(f.paths, r.URL.Path+"?"+r.URL.RawQuery)
		f.uas = append(f.uas, r.UserAgent())
		f.mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/ws/2/") && r.URL.Query().Get("fmt") != "json" {
			t.Errorf("missing fmt=json: %s", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/ws/2/recording/rec-1":
			io.WriteString(w, recJSON)
		case r.URL.Path == "/ws/2/recording":
			q := r.URL.Query().Get("query")
			if !strings.Contains(q, `recording:"晴天"`) || !strings.Contains(q, `artist:"周杰伦"`) {
				t.Errorf("query %q", q)
			}
			io.WriteString(w, `{"recordings":[{"id":"rec-1","score":100,"title":"晴天","length":269000,"releases":[{"id":"rel-1","title":"叶惠美"}]}]}`)
		case r.URL.Path == "/ws/2/isrc/TWK970300114":
			io.WriteString(w, `{"recordings":[{"id":"rec-1","title":"晴天","length":269000}]}`)
		case r.URL.Path == "/ws/2/isrc/NOPE":
			w.WriteHeader(404)
		case r.URL.Path == "/ws/2/release/rel-1":
			io.WriteString(w, `{"id":"rel-1","title":"叶惠美","date":"2003-07-31","label-info":[{"label":{"name":"JVR"}}]}`)
		case r.URL.Path == "/ws/2/artist":
			io.WriteString(w, `{"artists":[{"id":"art-1","name":"周杰伦","sort-name":"Chou, Jay","country":"TW","aliases":[{"name":"Jay Chou"}]}]}`)
		case r.URL.Path == "/caa/release/rel-1":
			io.WriteString(w, `{"images":[{"image":"http://x/back.jpg","front":false,"thumbnails":{}},{"image":"http://x/full.jpg","front":true,"thumbnails":{"500":"`+"http://"+r.Host+`/img/500.jpg"}}]}`)
		case r.URL.Path == "/img/500.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			io.WriteString(w, "\xff\xd8jpeg")
		case r.URL.Path == "/ws/2/recording/busy":
			w.WriteHeader(503)
		default:
			w.WriteHeader(404)
		}
	})
}

func newTest(t *testing.T, interval time.Duration) (*Provider, *fakeMB) {
	f := &fakeMB{}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	p, err := New(Config{BaseURL: srv.URL, CAAURL: srv.URL + "/caa", UserAgent: "yimeng/test ( test@example.com )", Interval: interval})
	if err != nil {
		t.Fatal(err)
	}
	return p, f
}

func TestNewRequiresContact(t *testing.T) {
	if _, err := New(Config{UserAgent: "yimeng"}); err == nil {
		t.Fatal("UA without contact accepted")
	}
}

func TestLookups(t *testing.T) {
	p, f := newTest(t, time.Millisecond)
	ctx := context.Background()
	m, err := p.LookupTrack(ctx, metadata.TrackQuery{MBRecordingID: "rec-1", Album: "叶惠美"})
	if err != nil || len(m) != 1 {
		t.Fatal(err)
	}
	got := m[0]
	if got.MBReleaseID != "rel-1" || got.Year != 2003 || got.ISRC != "TWK970300114" || got.DurationMs != 269000 || got.Artist != "周杰伦" {
		t.Fatalf("%+v", got)
	}
	roles := map[string]bool{}
	for _, c := range got.Credits {
		roles[c.Role+"="+c.Name] = true
	}
	for _, want := range []string{"producer=周杰伦", "instrument (guitar)=蔡科俊", "composer=周杰伦", "lyricist=周杰伦"} {
		if !roles[want] {
			t.Errorf("missing credit %s in %v", want, roles)
		}
	}
	if m, err := p.LookupTrack(ctx, metadata.TrackQuery{ISRC: "TWK970300114"}); err != nil || m[0].MBRecordingID != "rec-1" || m[0].Score != 0.95 {
		t.Fatal(m, err)
	}
	// ISRC miss falls back to search
	if m, err := p.LookupTrack(ctx, metadata.TrackQuery{ISRC: "NOPE", Title: "晴天", Artist: "周杰伦", Album: "叶惠美"}); err != nil || m[0].Score != 1 {
		t.Fatal(m, err)
	}
	if a, err := p.LookupAlbum(ctx, metadata.AlbumQuery{MBReleaseID: "rel-1"}); err != nil || a[0].Label != "JVR" || a[0].Year != 2003 {
		t.Fatal(a, err)
	}
	if a, err := p.LookupArtist(ctx, metadata.ArtistQuery{Name: "周杰伦"}); err != nil || a[0].SortName != "Chou, Jay" || a[0].Aliases[0] != "Jay Chou" {
		t.Fatal(a, err)
	}
	covers, err := p.CoverArt(ctx, metadata.AlbumQuery{MBReleaseID: "rel-1"})
	if err != nil || !strings.HasSuffix(covers[0].URL, "/img/500.jpg") || covers[0].Width != 500 {
		t.Fatalf("%+v %v", covers, err)
	}
	rc, err := p.FetchCover(ctx, covers[0])
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if string(b) != "\xff\xd8jpeg" {
		t.Fatal("cover bytes")
	}
	if _, err := p.LookupTrack(ctx, metadata.TrackQuery{MBRecordingID: "busy"}); !errors.Is(err, metadata.ErrRateLimited) {
		t.Fatal("503 should map to ErrRateLimited")
	}
	if _, err := p.Lyrics(ctx, metadata.TrackQuery{}); !errors.Is(err, metadata.ErrNotSupported) {
		t.Fatal("lyrics")
	}
	for _, ua := range f.uas {
		if ua != "yimeng/test ( test@example.com )" {
			t.Fatalf("UA %q", ua)
		}
	}
}

func TestRateLimit(t *testing.T) {
	p, f := newTest(t, 100*time.Millisecond)
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); p.LookupTrack(ctx, metadata.TrackQuery{MBRecordingID: "rec-1"}) }()
	}
	wg.Wait()
	if len(f.times) != 4 {
		t.Fatal(len(f.times))
	}
	for i := 1; i < len(f.times); i++ {
		// sorted by arrival; spacing must respect the interval (small jitter allowed)
		if d := f.times[i].Sub(f.times[i-1]); d < 90*time.Millisecond {
			t.Fatalf("requests %d and %d only %v apart", i-1, i, d)
		}
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := p.LookupTrack(cctx, metadata.TrackQuery{MBRecordingID: "rec-1"}); err == nil {
		t.Fatal("cancelled ctx should fail")
	}
}
