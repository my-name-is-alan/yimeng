package metadata

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/events"
)

// Enricher fills canonical tracks/albums from a MetadataProvider in the
// background: MBIDs/ISRC (only where missing), credits, and album covers
// when no local art exists. Results are logged in metadata_enrichment so each
// entity is queried once; a "metadata.patched" event is published per change.
type Enricher struct {
	DB       *sql.DB
	P        MetadataProvider
	Hub      *events.Hub
	CoverDir string
	Log      *slog.Logger
	// MinScore is the minimum search score (0..1) to accept a fuzzy match.
	MinScore float64
	// RetryAfter is how long errors are remembered before retrying.
	RetryAfter time.Duration

	kick chan struct{}
	once sync.Once
}

func (e *Enricher) init() {
	e.once.Do(func() {
		e.kick = make(chan struct{}, 1)
		if e.Log == nil {
			e.Log = slog.Default()
		}
		if e.MinScore == 0 {
			e.MinScore = 0.9
		}
		if e.RetryAfter == 0 {
			e.RetryAfter = 24 * time.Hour
		}
	})
}

// Kick schedules a pass (non-blocking; coalesced).
func (e *Enricher) Kick() {
	e.init()
	select {
	case e.kick <- struct{}{}:
	default:
	}
}

// Run processes passes until ctx is done.
func (e *Enricher) Run(ctx context.Context) {
	e.init()
	for {
		select {
		case <-ctx.Done():
			return
		case <-e.kick:
		}
		if err := e.Pass(ctx); err != nil && ctx.Err() == nil {
			e.Log.Warn("metadata enrichment paused", "provider", e.P.ID(), "err", err)
			if errors.Is(err, ErrRateLimited) || errors.Is(err, ErrUnavailable) {
				select { // back off, then try again
				case <-ctx.Done():
					return
				case <-time.After(time.Minute):
					e.Kick()
				}
			}
		}
	}
}

// Pass enriches every pending track, then album covers. It stops on
// rate-limit / unavailability (to be retried later).
func (e *Enricher) Pass(ctx context.Context) error {
	e.init()
	prov := string(e.P.ID())
	retry := time.Now().Add(-e.RetryAfter).UnixMilli()
	for {
		ids, err := e.pending(ctx, `SELECT t.id FROM tracks t WHERE NOT EXISTS (SELECT 1 FROM metadata_enrichment m
			WHERE m.entity_type='track' AND m.entity_id=t.id AND m.provider=? AND (m.status<>'error' OR m.fetched_at>?)) LIMIT 100`, prov, retry)
		if err != nil || len(ids) == 0 {
			if err != nil {
				return err
			}
			break
		}
		for _, id := range ids {
			if err := e.EnrichTrack(ctx, id); err != nil {
				return err
			}
		}
	}
	if e.P.Capabilities().Has(CapCoverArt) {
		ids, err := e.pending(ctx, `SELECT a.id FROM albums a WHERE a.cover_path IS NULL AND a.mb_release_id IS NOT NULL
			AND NOT EXISTS (SELECT 1 FROM metadata_enrichment m WHERE m.entity_type='album' AND m.entity_id=a.id
			AND m.provider=? AND (m.status<>'error' OR m.fetched_at>?))`, prov, retry)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err := e.EnrichAlbumCover(ctx, id); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Enricher) pending(ctx context.Context, q string, args ...any) ([]string, error) {
	rows, err := e.DB.QueryContext(ctx, q, args...)
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

func (e *Enricher) record(ctx context.Context, typ, id, status, ext, msg string) error {
	_, err := e.DB.ExecContext(ctx, `INSERT INTO metadata_enrichment(entity_type, entity_id, provider, status, external_id, error, fetched_at)
		VALUES (?,?,?,?,?,?,?) ON CONFLICT(entity_type, entity_id, provider) DO UPDATE SET status=excluded.status,
		external_id=excluded.external_id, error=excluded.error, fetched_at=excluded.fetched_at`,
		typ, id, string(e.P.ID()), status, nullable(ext), nullable(msg), time.Now().UnixMilli())
	return err
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// transient errors abort the pass without recording the entity.
func transient(err error) bool {
	return errors.Is(err, ErrRateLimited) || errors.Is(err, ErrUnavailable) ||
		errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (e *Enricher) EnrichTrack(ctx context.Context, id string) error {
	e.init()
	var q TrackQuery
	var albumID, albumRel string
	err := e.DB.QueryRowContext(ctx, `SELECT t.title, COALESCE(t.artist_name,''), COALESCE(al.title,''), COALESCE(al.artist_name,''),
		COALESCE(t.duration_ms,0), COALESCE(t.isrc,''), COALESCE(t.mb_recording_id,''), COALESCE(t.album_id,''), COALESCE(al.mb_release_id,'')
		FROM tracks t LEFT JOIN albums al ON al.id=t.album_id WHERE t.id=?`, id).
		Scan(&q.Title, &q.Artist, &q.Album, &q.AlbumArtist, &q.DurationMs, &q.ISRC, &q.MBRecordingID, &albumID, &albumRel)
	if err != nil {
		return err
	}
	q.MBReleaseID = albumRel
	if q.Artist == "Unknown Artist" {
		q.Artist = ""
	}
	matches, err := e.P.LookupTrack(ctx, q)
	if transient(err) {
		return err
	}
	if errors.Is(err, ErrNotFound) || (err == nil && len(matches) == 0) {
		return e.record(ctx, "track", id, "no_match", "", "")
	}
	if err != nil {
		return e.record(ctx, "track", id, "error", "", err.Error())
	}
	var best *TrackMatch
	for i := range matches {
		m := &matches[i]
		if q.MBRecordingID != "" {
			best = m
			break
		}
		if m.Score < e.MinScore {
			continue
		}
		if q.DurationMs > 0 && m.DurationMs > 0 && abs(q.DurationMs-m.DurationMs) > 3000 {
			continue
		}
		best = m
		break
	}
	if best == nil {
		return e.record(ctx, "track", id, "no_match", "", "")
	}
	// search / ISRC hits are shallow; fetch the full recording for credits.
	if q.MBRecordingID == "" && best.MBRecordingID != "" {
		full, err := e.P.LookupTrack(ctx, TrackQuery{MBRecordingID: best.MBRecordingID, Album: q.Album})
		if transient(err) {
			return err
		}
		if err == nil && len(full) > 0 {
			f := full[0]
			best = &f
		}
	}

	tx, err := e.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE tracks SET mb_recording_id=COALESCE(mb_recording_id, ?), isrc=COALESCE(isrc, ?) WHERE id=?`,
		nullable(best.MBRecordingID), nullable(best.ISRC), id); err != nil {
		return err
	}
	if albumID != "" && albumRel == "" && best.MBReleaseID != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE albums SET mb_release_id=? WHERE id=? AND mb_release_id IS NULL`, best.MBReleaseID, albumID); err != nil {
			return err
		}
	}
	src := string(e.P.ID())
	if _, err := tx.ExecContext(ctx, `DELETE FROM credits WHERE track_id=? AND source=?`, id, src); err != nil {
		return err
	}
	for _, c := range best.Credits {
		sum := sha1.Sum([]byte(id + "\x00" + src + "\x00" + c.Role + "\x00" + c.Name + "\x00" + c.MBID))
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO credits(id, track_id, role, name, mb_id, source) VALUES (?,?,?,?,?,?)`,
			"cr_"+hex.EncodeToString(sum[:10]), id, c.Role, c.Name, nullable(c.MBID), src); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if err := e.record(ctx, "track", id, "matched", best.MBRecordingID, ""); err != nil {
		return err
	}
	e.Hub.Publish("metadata.patched", map[string]any{"entity_type": "track", "entity_id": id, "album_id": albumID})
	return nil
}

const maxCoverBytes = 10 << 20

func (e *Enricher) EnrichAlbumCover(ctx context.Context, albumID string) error {
	e.init()
	var rel string
	var cover sql.NullString
	if err := e.DB.QueryRowContext(ctx, `SELECT COALESCE(mb_release_id,''), cover_path FROM albums WHERE id=?`, albumID).Scan(&rel, &cover); err != nil {
		return err
	}
	if rel == "" || cover.Valid {
		return nil
	}
	cands, err := e.P.CoverArt(ctx, AlbumQuery{MBReleaseID: rel})
	if transient(err) {
		return err
	}
	if errors.Is(err, ErrNotFound) || (err == nil && len(cands) == 0) {
		return e.record(ctx, "album", albumID, "no_match", rel, "")
	}
	if err != nil {
		return e.record(ctx, "album", albumID, "error", rel, err.Error())
	}
	rc, err := e.P.FetchCover(ctx, cands[0])
	if transient(err) {
		return err
	}
	if err != nil {
		return e.record(ctx, "album", albumID, "error", rel, err.Error())
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxCoverBytes+1))
	if err != nil || len(data) > maxCoverBytes || len(data) == 0 {
		return e.record(ctx, "album", albumID, "error", rel, fmt.Sprintf("cover download failed (%d bytes): %v", len(data), err))
	}
	ext := ".jpg"
	if strings.HasPrefix(string(data), "\x89PNG") {
		ext = ".png"
	}
	dir := filepath.Join(e.CoverDir, "remote")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	sum := sha1.Sum([]byte(string(e.P.ID()) + ":" + rel))
	dst := filepath.Join(dir, hex.EncodeToString(sum[:])+ext)
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	if _, err := e.DB.ExecContext(ctx, `UPDATE albums SET cover_path=?, cover_source='remote' WHERE id=? AND cover_path IS NULL`, dst, albumID); err != nil {
		return err
	}
	if err := e.record(ctx, "album", albumID, "matched", rel, ""); err != nil {
		return err
	}
	e.Hub.Publish("metadata.patched", map[string]any{"entity_type": "album", "entity_id": albumID})
	return nil
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
