package metadata

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/events"
)

// FillProvider is the enrichment-log provider id of the Filler.
const FillProvider = "fill"

// Filler is the opt-in background "fill-only" enricher: for local tracks and
// albums with empty ISRC / year / genre / cover it asks the enabled sources
// in order (Spotify, 网易云, QQ音乐, 酷狗, MusicBrainz …) and writes values
// only into fields that are still empty. Fills are stored in
// metadata_fills and re-applied after every merge, so they survive rescans;
// tag values and user overrides always win. Lookups are rate-limited.
type Filler struct {
	DB       *sql.DB
	Hub      *events.Hub
	CoverDir string
	Log      *slog.Logger
	// Sources returns the currently enabled sources in priority order (nil = off).
	Sources func() []MetadataProvider
	// PerMinute caps entity lookups per minute (default 20).
	PerMinute func() int
	MinScore  float64
	Sleep     func(ctx context.Context, d time.Duration) error // tests

	kick chan struct{}
	once sync.Once
}

func (f *Filler) init() {
	f.once.Do(func() {
		f.kick = make(chan struct{}, 1)
		if f.Log == nil {
			f.Log = slog.Default()
		}
		if f.MinScore == 0 {
			f.MinScore = 0.9
		}
		if f.Sleep == nil {
			f.Sleep = func(ctx context.Context, d time.Duration) error {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(d):
					return nil
				}
			}
		}
	})
}

func (f *Filler) Kick() {
	f.init()
	select {
	case f.kick <- struct{}{}:
	default:
	}
}

// Reset forgets "no match" results so a changed source list retries them.
func (f *Filler) Reset(ctx context.Context) error {
	_, err := f.DB.ExecContext(ctx, `DELETE FROM metadata_enrichment WHERE provider=?`, FillProvider)
	return err
}

func (f *Filler) Run(ctx context.Context) {
	f.init()
	t := time.NewTicker(6 * time.Hour)
	defer t.Stop()
	f.Kick()
	for {
		select {
		case <-ctx.Done():
			return
		case <-f.kick:
		case <-t.C:
		}
		if err := f.Pass(ctx); err != nil && ctx.Err() == nil {
			f.Log.Warn("metadata fill paused", "err", err)
		}
	}
}

func (f *Filler) gap() time.Duration {
	n := 20
	if f.PerMinute != nil {
		if v := f.PerMinute(); v > 0 {
			n = v
		}
	}
	return time.Minute / time.Duration(n)
}

// Pass fills pending local tracks, then albums. Returns early when disabled.
func (f *Filler) Pass(ctx context.Context) error {
	f.init()
	retry := time.Now().Add(-7 * 24 * time.Hour).UnixMilli()
	for _, kind := range []string{"track", "album"} {
		q := `SELECT t.id FROM tracks t WHERE t.origin='local' AND (t.isrc IS NULL OR t.isrc='' OR t.year IS NULL OR t.genre IS NULL OR t.genre='')`
		if kind == "album" {
			q = `SELECT t.id FROM albums t WHERE t.origin='local' AND (t.cover_path IS NULL OR t.year IS NULL OR t.genre IS NULL OR t.genre='')`
		}
		q += ` AND NOT EXISTS (SELECT 1 FROM metadata_enrichment m WHERE m.entity_type=? AND m.entity_id=t.id AND m.provider=? AND m.fetched_at>?) LIMIT 500`
		for {
			srcs := f.sources()
			if len(srcs) == 0 {
				return nil
			}
			ids, err := idList(ctx, f.DB, q, kind, FillProvider, retry)
			if err != nil || len(ids) == 0 {
				if err != nil {
					return err
				}
				break
			}
			for _, id := range ids {
				srcs = f.sources()
				if len(srcs) == 0 {
					return nil
				}
				var err error
				if kind == "track" {
					err = f.FillTrack(ctx, id, srcs)
				} else {
					err = f.FillAlbum(ctx, id, srcs)
				}
				if err != nil && ctx.Err() != nil {
					return err
				}
				if err := f.Sleep(ctx, f.gap()); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (f *Filler) sources() []MetadataProvider {
	if f.Sources == nil {
		return nil
	}
	return f.Sources()
}

func idList(ctx context.Context, db *sql.DB, q string, args ...any) ([]string, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (f *Filler) record(ctx context.Context, typ, id, status, msg string) {
	f.DB.ExecContext(ctx, `INSERT INTO metadata_enrichment(entity_type, entity_id, provider, status, external_id, error, fetched_at)
		VALUES (?,?,?,?,NULL,?,?) ON CONFLICT(entity_type, entity_id, provider) DO UPDATE SET status=excluded.status,
		error=excluded.error, fetched_at=excluded.fetched_at`, typ, id, FillProvider, status, nullable(msg), time.Now().UnixMilli())
}

type fillVal struct{ value, provider, ext string }

// FillTrack fills one local track's empty isrc/year/genre.
func (f *Filler) FillTrack(ctx context.Context, id string, srcs []MetadataProvider) error {
	f.init()
	var q TrackQuery
	var isrc, genre string
	var year sql.NullInt64
	err := f.DB.QueryRowContext(ctx, `SELECT t.title, COALESCE(t.artist_name,''), COALESCE(al.title,''), COALESCE(t.duration_ms,0),
		COALESCE(t.isrc,''), t.year, COALESCE(t.genre,'') FROM tracks t LEFT JOIN albums al ON al.id=t.album_id WHERE t.id=? AND t.origin='local'`, id).
		Scan(&q.Title, &q.Artist, &q.Album, &q.DurationMs, &isrc, &year, &genre)
	if err != nil {
		return err
	}
	if q.Artist == "Unknown Artist" {
		q.Artist = ""
	}
	want := map[string]bool{"isrc": isrc == "", "year": !year.Valid, "genre": genre == ""}
	got := map[string]fillVal{}
	var lastErr error
	for _, p := range srcs {
		if !anyMissing(want, got) {
			break
		}
		ms, err := p.LookupTrack(ctx, q)
		if err != nil {
			if !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrNotSupported) {
				lastErr = err
			}
			continue
		}
		for _, m := range ms {
			if m.Score < f.MinScore || (q.DurationMs > 0 && m.DurationMs > 0 && abs(q.DurationMs-m.DurationMs) > 3000) {
				continue
			}
			put(want, got, "isrc", strings.ToUpper(strings.TrimSpace(m.ISRC)), m)
			if m.Year > 0 {
				put(want, got, "year", strconv.Itoa(m.Year), m)
			}
			put(want, got, "genre", strings.TrimSpace(m.Genre), m)
			break
		}
	}
	return f.commit(ctx, "track", id, got, lastErr)
}

func anyMissing(want map[string]bool, got map[string]fillVal) bool {
	for k, w := range want {
		if w {
			if _, ok := got[k]; !ok {
				return true
			}
		}
	}
	return false
}

func put(want map[string]bool, got map[string]fillVal, field, v string, m TrackMatch) {
	if v == "" || !want[field] {
		return
	}
	if _, ok := got[field]; !ok {
		got[field] = fillVal{v, string(m.Provider), m.ExternalID}
	}
}

func (f *Filler) commit(ctx context.Context, typ, id string, got map[string]fillVal, lastErr error) error {
	if len(got) == 0 {
		if lastErr != nil {
			f.record(ctx, typ, id, "error", lastErr.Error())
		} else {
			f.record(ctx, typ, id, "no_match", "")
		}
		return nil
	}
	tx, err := f.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	for field, v := range got {
		if _, err := tx.ExecContext(ctx, `INSERT INTO metadata_fills(entity_type, entity_id, field, value, provider, external_id, filled_at)
			VALUES (?,?,?,?,?,?,?) ON CONFLICT(entity_type, entity_id, field) DO UPDATE SET value=excluded.value,
			provider=excluded.provider, external_id=excluded.external_id, filled_at=excluded.filled_at`,
			typ, id, field, v.value, v.provider, nullable(v.ext), now); err != nil {
			return err
		}
	}
	if err := ApplyFills(ctx, tx, typ, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	f.record(ctx, typ, id, "matched", "")
	if f.Hub != nil {
		f.Hub.Publish("metadata.patched", map[string]any{"entity_type": typ, "entity_id": id, "source": FillProvider})
	}
	return nil
}

// FillAlbum fills one local album's empty year/genre/cover.
func (f *Filler) FillAlbum(ctx context.Context, id string, srcs []MetadataProvider) error {
	f.init()
	var q AlbumQuery
	var genre string
	var year sql.NullInt64
	var cover sql.NullString
	if err := f.DB.QueryRowContext(ctx, `SELECT title, COALESCE(artist_name,''), year, COALESCE(genre,''), cover_path FROM albums WHERE id=? AND origin='local'`, id).
		Scan(&q.Title, &q.AlbumArtist, &year, &genre, &cover); err != nil {
		return err
	}
	if year.Valid {
		q.Year = int(year.Int64)
	}
	want := map[string]bool{"year": !year.Valid, "genre": genre == "", "cover_path": !cover.Valid || cover.String == ""}
	got := map[string]fillVal{}
	var lastErr error
	for _, p := range srcs {
		if !anyMissing(want, got) {
			break
		}
		as, err := p.LookupAlbum(ctx, q)
		if err != nil {
			if !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrNotSupported) {
				lastErr = err
			}
			continue
		}
		for _, a := range as {
			if a.Score < f.MinScore {
				continue
			}
			m := TrackMatch{Provider: a.Provider, ExternalID: a.ExternalID}
			if a.Year > 0 {
				put(want, got, "year", strconv.Itoa(a.Year), m)
			}
			if len(a.Genres) > 0 {
				put(want, got, "genre", a.Genres[0], m)
			}
			if want["cover_path"] && a.CoverURL != "" {
				if _, ok := got["cover_path"]; !ok {
					if path, err := f.saveCover(ctx, p, a); err == nil {
						got["cover_path"] = fillVal{path, string(a.Provider), a.ExternalID}
					}
				}
			}
			break
		}
	}
	return f.commit(ctx, "album", id, got, lastErr)
}

func (f *Filler) saveCover(ctx context.Context, p MetadataProvider, a AlbumInfo) (string, error) {
	rc, err := p.FetchCover(ctx, CoverCandidate{Provider: a.Provider, URL: a.CoverURL})
	if err != nil {
		return "", err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxCoverBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxCoverBytes {
		return "", errors.New("cover download failed")
	}
	ext := ".jpg"
	if strings.HasPrefix(string(data), "\x89PNG") {
		ext = ".png"
	}
	dir := filepath.Join(f.CoverDir, "fill")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	sum := sha1.Sum([]byte(string(a.Provider) + ":" + a.ExternalID + ":" + a.CoverURL))
	dst := filepath.Join(dir, hex.EncodeToString(sum[:])+ext)
	if err := os.WriteFile(dst+".tmp", data, 0o644); err != nil {
		return "", err
	}
	return dst, os.Rename(dst+".tmp", dst)
}

type execer interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
}

var fillCols = map[string]map[string]string{
	"track": {"isrc": "isrc", "year": "year", "genre": "genre"},
	"album": {"year": "year", "genre": "genre", "cover_path": "cover_path"},
}

// ApplyFills writes stored fills into still-empty fields (one entity, or all
// when id == ""). Called by the Filler and at the end of every library merge.
func ApplyFills(ctx context.Context, q execer, typ, id string) error {
	for _, t := range []string{"track", "album"} {
		if typ != "" && typ != t {
			continue
		}
		table := map[string]string{"track": "tracks", "album": "albums"}[t]
		for field, col := range fillCols[t] {
			val := `(SELECT value FROM metadata_fills f WHERE f.entity_type='` + t + `' AND f.entity_id=` + table + `.id AND f.field='` + field + `')`
			if col == "year" {
				val = `CAST(` + val + ` AS INTEGER)`
			}
			set := col + `=` + val
			if col == "cover_path" {
				set += `, cover_source='remote'`
			}
			stmt := `UPDATE ` + table + ` SET ` + set + ` WHERE origin='local' AND (` + col + ` IS NULL OR ` + col + `='')
				AND EXISTS (SELECT 1 FROM metadata_fills f WHERE f.entity_type='` + t + `' AND f.entity_id=` + table + `.id AND f.field='` + field + `')`
			args := []any{}
			if id != "" {
				stmt += ` AND id=?`
				args = append(args, id)
			}
			if _, err := q.ExecContext(ctx, stmt, args...); err != nil {
				return err
			}
		}
	}
	return nil
}
