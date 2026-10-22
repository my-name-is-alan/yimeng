package metadata

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/infra/db"
	"github.com/my-name-is-alan/yimeng/core/migrations"
)

type fillProv struct {
	id    ProviderID
	calls int
	track TrackMatch
	album AlbumInfo
}

func (f *fillProv) ID() ProviderID                                      { return f.id }
func (f *fillProv) Capabilities() Capability                            { return CapTrackTags | CapAlbumInfo | CapCoverArt }
func (f *fillProv) Health(context.Context) HealthState                  { return HealthOK }
func (f *fillProv) Lyrics(context.Context, TrackQuery) (*Lyrics, error) { return nil, ErrNotSupported }
func (f *fillProv) LookupArtist(context.Context, ArtistQuery) ([]ArtistInfo, error) {
	return nil, ErrNotSupported
}
func (f *fillProv) CoverArt(context.Context, AlbumQuery) ([]CoverCandidate, error) {
	return nil, ErrNotSupported
}
func (f *fillProv) FetchCover(context.Context, CoverCandidate) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("\xff\xd8fill")), nil
}
func (f *fillProv) LookupTrack(context.Context, TrackQuery) ([]TrackMatch, error) {
	f.calls++
	if f.track.Score == 0 {
		return nil, ErrNotFound
	}
	return []TrackMatch{f.track}, nil
}
func (f *fillProv) LookupAlbum(context.Context, AlbumQuery) ([]AlbumInfo, error) {
	f.calls++
	if f.album.Score == 0 {
		return nil, ErrNotFound
	}
	return []AlbumInfo{f.album}, nil
}

func TestFillerFillOnlyLocalOnly(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	d, err := db.Open(filepath.Join(tmp, "f.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := db.Migrate(ctx, d, migrations.FS); err != nil {
		t.Fatal(err)
	}
	d.Exec(`INSERT INTO albums(id, title, artist_name, genre, created_at, updated_at) VALUES ('al1','叶惠美','周杰伦','Mandopop',1,1)`)
	d.Exec(`INSERT INTO albums(id, title, artist_name, origin, created_at, updated_at) VALUES ('alx','Online','X','qq',1,1)`)
	d.Exec(`INSERT INTO tracks(id, title, album_id, duration_ms, artist_name, year, created_at, updated_at) VALUES ('t1','晴天','al1',269000,'周杰伦',2003,1,1)`)
	d.Exec(`INSERT INTO tracks(id, title, album_id, duration_ms, artist_name, origin, created_at, updated_at) VALUES ('tx','Online','alx',1000,'X','qq',1,1)`)

	weak := &fillProv{id: Spotify, track: TrackMatch{Provider: Spotify, Score: 0.5, ISRC: "BAD"}}
	qq := &fillProv{id: QQMusic,
		track: TrackMatch{Provider: QQMusic, ExternalID: "qq:x", Score: 1, DurationMs: 269500, ISRC: "twk970300003", Year: 1999, Genre: "Pop"},
		album: AlbumInfo{Provider: QQMusic, ExternalID: "qq:album-1", Score: 0.97, Year: 2003, Genres: []string{"Rock"}, CoverURL: "https://y.gtimg.cn/a.jpg"}}
	srcs := []MetadataProvider{weak, qq}
	enabled := true
	f := &Filler{DB: d, CoverDir: filepath.Join(tmp, "covers"),
		Sources: func() []MetadataProvider {
			if !enabled {
				return nil
			}
			return srcs
		},
		Sleep: func(context.Context, time.Duration) error { return nil }}
	if err := f.Pass(ctx); err != nil {
		t.Fatal(err)
	}
	var isrc, genre string
	var year int
	d.QueryRow(`SELECT isrc, year, genre FROM tracks WHERE id='t1'`).Scan(&isrc, &year, &genre)
	if isrc != "TWK970300003" || year != 2003 || genre != "Pop" { // year was set by tags: kept
		t.Fatalf("track isrc=%s year=%d genre=%s", isrc, year, genre)
	}
	var ayear int
	var agenre, cover string
	d.QueryRow(`SELECT year, genre, cover_path FROM albums WHERE id='al1'`).Scan(&ayear, &agenre, &cover)
	if ayear != 2003 || agenre != "Mandopop" || !strings.Contains(cover, filepath.Join("covers", "fill")) {
		t.Fatalf("album year=%d genre=%s cover=%s", ayear, agenre, cover)
	}
	var n int
	d.QueryRow(`SELECT count(*) FROM metadata_fills WHERE entity_id IN ('tx','alx')`).Scan(&n)
	if n != 0 {
		t.Fatal("online items must not be filled")
	}
	// a rescan wipes the fields → fills come back; tag values still win
	d.Exec(`UPDATE tracks SET isrc=NULL, genre='FromTags' WHERE id='t1'`)
	if err := ApplyFills(ctx, d, "", ""); err != nil {
		t.Fatal(err)
	}
	d.QueryRow(`SELECT isrc, genre FROM tracks WHERE id='t1'`).Scan(&isrc, &genre)
	if isrc != "TWK970300003" || genre != "FromTags" {
		t.Fatalf("reapply isrc=%s genre=%s", isrc, genre)
	}
	// already processed: a second pass makes no lookups; disabled = no work
	calls := qq.calls
	f.Pass(ctx)
	enabled = false
	f.Reset(ctx)
	f.Pass(ctx)
	if qq.calls != calls {
		t.Fatalf("unexpected lookups %d -> %d", calls, qq.calls)
	}
}
