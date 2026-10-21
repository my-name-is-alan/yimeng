package metadata

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/events"
	"github.com/my-name-is-alan/yimeng/core/internal/infra/db"
	"github.com/my-name-is-alan/yimeng/core/migrations"
)

type fakeProv struct {
	calls   []TrackQuery
	rateHit bool
}

func (f *fakeProv) ID() ProviderID                                      { return MusicBrainz }
func (f *fakeProv) Capabilities() Capability                            { return CapTrackTags | CapCoverArt | CapCredits }
func (f *fakeProv) Health(context.Context) HealthState                  { return HealthOK }
func (f *fakeProv) Lyrics(context.Context, TrackQuery) (*Lyrics, error) { return nil, ErrNotSupported }
func (f *fakeProv) LookupAlbum(context.Context, AlbumQuery) ([]AlbumInfo, error) {
	return nil, ErrNotSupported
}
func (f *fakeProv) LookupArtist(context.Context, ArtistQuery) ([]ArtistInfo, error) {
	return nil, ErrNotSupported
}
func (f *fakeProv) LookupTrack(_ context.Context, q TrackQuery) ([]TrackMatch, error) {
	f.calls = append(f.calls, q)
	if f.rateHit {
		return nil, ErrRateLimited
	}
	switch {
	case q.MBRecordingID == "rec-1":
		return []TrackMatch{{MBRecordingID: "rec-1", MBReleaseID: "rel-1", ISRC: "ISRC1", Score: 1,
			Credits: []Credit{{Role: "producer", Name: "P"}, {Role: "composer", Name: "C", MBID: "c1"}}}}, nil
	case q.Title == "Hit":
		return []TrackMatch{{MBRecordingID: "rec-1", Score: 1, DurationMs: 200500}}, nil
	case q.Title == "Weak":
		return []TrackMatch{{MBRecordingID: "rec-2", Score: 0.5}}, nil
	case q.Title == "WrongLen":
		return []TrackMatch{{MBRecordingID: "rec-3", Score: 1, DurationMs: 999000}}, nil
	}
	return nil, ErrNotFound
}
func (f *fakeProv) CoverArt(_ context.Context, q AlbumQuery) ([]CoverCandidate, error) {
	if q.MBReleaseID == "rel-1" {
		return []CoverCandidate{{URL: "http://x/c.jpg"}}, nil
	}
	return nil, ErrNotFound
}
func (f *fakeProv) FetchCover(context.Context, CoverCandidate) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("\xff\xd8img")), nil
}

func TestEnricher(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	d, err := db.Open(filepath.Join(tmp, "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	db.Migrate(ctx, d, migrations.FS)
	d.Exec(`INSERT INTO albums(id, title, artist_name, created_at, updated_at) VALUES ('al1','Album','Artist',1,1)`)
	for _, r := range [][]any{{"t1", "Hit", 200000}, {"t2", "Weak", 0}, {"t3", "WrongLen", 200000}, {"t4", "Nothing", 0}} {
		d.Exec(`INSERT INTO tracks(id, title, album_id, duration_ms, artist_name, created_at, updated_at) VALUES (?,?,'al1',?,'Artist',1,1)`, r...)
	}
	hub := events.NewHub()
	ch, cancel := hub.Subscribe()
	defer cancel()
	p := &fakeProv{}
	e := &Enricher{DB: d, P: p, Hub: hub, CoverDir: filepath.Join(tmp, "covers")}
	if err := e.Pass(ctx); err != nil {
		t.Fatal(err)
	}
	var mb, isrc, rel, cover, coverSrc string
	d.QueryRow(`SELECT mb_recording_id, isrc FROM tracks WHERE id='t1'`).Scan(&mb, &isrc)
	d.QueryRow(`SELECT mb_release_id, cover_path, cover_source FROM albums WHERE id='al1'`).Scan(&rel, &cover, &coverSrc)
	if mb != "rec-1" || isrc != "ISRC1" || rel != "rel-1" || coverSrc != "remote" {
		t.Fatalf("mb=%s isrc=%s rel=%s cover=%s/%s", mb, isrc, rel, cover, coverSrc)
	}
	if b, _ := os.ReadFile(cover); string(b) != "\xff\xd8img" {
		t.Fatal("cover file")
	}
	var n int
	d.QueryRow(`SELECT count(*) FROM credits WHERE track_id='t1' AND source='musicbrainz'`).Scan(&n)
	if n != 2 {
		t.Fatalf("credits %d", n)
	}
	for _, id := range []string{"t2", "t3", "t4"} {
		var s string
		d.QueryRow(`SELECT status FROM metadata_enrichment WHERE entity_id=?`, id).Scan(&s)
		if s != "no_match" {
			t.Errorf("%s status %q", id, s)
		}
		var got string
		d.QueryRow(`SELECT COALESCE(mb_recording_id,'') FROM tracks WHERE id=?`, id).Scan(&got)
		if got != "" {
			t.Errorf("%s wrongly matched %s", id, got)
		}
	}
	if e := <-ch; e.Type != "metadata.patched" {
		t.Fatal(e.Type)
	}
	// second pass is a no-op (everything recorded)
	calls := len(p.calls)
	e.Pass(ctx)
	if len(p.calls) != calls {
		t.Fatal("re-queried recorded tracks")
	}
	// rate limit aborts without recording
	d.Exec(`INSERT INTO tracks(id, title, created_at, updated_at) VALUES ('t5','Hit',1,1)`)
	p.rateHit = true
	if err := e.Pass(ctx); err == nil {
		t.Fatal("want rate limit error")
	}
	d.QueryRow(`SELECT count(*) FROM metadata_enrichment WHERE entity_id='t5'`).Scan(&n)
	if n != 0 {
		t.Fatal("rate-limited track recorded")
	}
}
