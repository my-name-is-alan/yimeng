package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/lyrics"
)

// MaxUserLyricsBytes caps a bound / hand-edited LRC.
const MaxUserLyricsBytes = 256 << 10

// UserLyrics is a lyric text pinned to a track from the web admin (manual edit or provider bind).
type UserLyrics struct {
	TrackID   string `json:"track_id"`
	LRC       string `json:"lrc"`
	OffsetMs  int64  `json:"offset_ms"`
	Source    string `json:"source"`
	UpdatedAt int64  `json:"updated_at"`
}

func (s *Scanner) trackExists(ctx context.Context, id string) error {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM tracks WHERE id=?`, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetUserLyrics binds lyrics to a track; they win over sidecar/embedded/provider lyrics.
func (s *Scanner) SetUserLyrics(ctx context.Context, u UserLyrics) (UserLyrics, error) {
	u.LRC = strings.TrimSpace(strings.ReplaceAll(u.LRC, "\r\n", "\n"))
	if u.LRC == "" {
		return u, fmt.Errorf("%w: lrc required", ErrBadInput)
	}
	if len(u.LRC) > MaxUserLyricsBytes {
		return u, fmt.Errorf("%w: lrc too large", ErrBadInput)
	}
	if u.OffsetMs < -600_000 || u.OffsetMs > 600_000 {
		return u, fmt.Errorf("%w: offset_ms out of range", ErrBadInput)
	}
	u.Source = strings.TrimSpace(u.Source)
	if u.Source == "" {
		u.Source = "manual"
	}
	if len(u.Source) > 32 {
		return u, fmt.Errorf("%w: source too long", ErrBadInput)
	}
	if err := s.trackExists(ctx, u.TrackID); err != nil {
		return u, err
	}
	u.UpdatedAt = time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx, `INSERT INTO user_lyrics(track_id, lrc, offset_ms, source, updated_at) VALUES (?,?,?,?,?)
		ON CONFLICT(track_id) DO UPDATE SET lrc=excluded.lrc, offset_ms=excluded.offset_ms, source=excluded.source, updated_at=excluded.updated_at`,
		u.TrackID, u.LRC, u.OffsetMs, u.Source, u.UpdatedAt)
	return u, err
}

func (s *Scanner) GetUserLyrics(ctx context.Context, trackID string) (UserLyrics, error) {
	u := UserLyrics{TrackID: trackID}
	err := s.db.QueryRowContext(ctx, `SELECT lrc, offset_ms, source, updated_at FROM user_lyrics WHERE track_id=?`, trackID).
		Scan(&u.LRC, &u.OffsetMs, &u.Source, &u.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

func (s *Scanner) ClearUserLyrics(ctx context.Context, trackID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM user_lyrics WHERE track_id=?`, trackID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// userLyrics returns parsed bound lyrics with the stored offset applied on top of any [offset:] tag.
func (s *Scanner) userLyrics(ctx context.Context, trackID string) (TrackLyrics, bool) {
	u, err := s.GetUserLyrics(ctx, trackID)
	if err != nil {
		return TrackLyrics{}, false
	}
	l := lyrics.Parse(u.LRC)
	if l.Synced && u.OffsetMs != 0 {
		for i := range l.Lines {
			l.Lines[i].TimeMs = max(l.Lines[i].TimeMs-u.OffsetMs, 0)
		}
		l.OffsetMs += u.OffsetMs
	}
	return TrackLyrics{TrackID: trackID, Source: "user:" + u.Source, Lyrics: l}, true
}

// MaxCoverBytes caps an uploaded album cover.
const MaxCoverBytes = 10 << 20

// SetAlbumCover stores an uploaded JPEG/PNG/WebP as the album's cover (survives rescans).
func (s *Scanner) SetAlbumCover(ctx context.Context, albumID string, img []byte) error {
	if len(img) == 0 || len(img) > MaxCoverBytes {
		return fmt.Errorf("%w: image must be 1 byte..10 MiB", ErrBadInput)
	}
	ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}[http.DetectContentType(img)]
	if ext == "" {
		return fmt.Errorf("%w: image must be JPEG, PNG or WebP", ErrBadInput)
	}
	if s.opts.CoverDir == "" {
		return fmt.Errorf("%w: cover cache not configured", ErrBadInput)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM albums WHERE id=?`, albumID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	dir := filepath.Join(s.opts.CoverDir, "custom")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// albumID is a hex DB id, but never trust it as a path component.
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return -1
	}, albumID)
	if name == "" {
		return ErrNotFound
	}
	stamp := time.Now().UnixMilli()
	path := filepath.Join(dir, fmt.Sprintf("%s-%d%s", name, stamp, ext)) // new name busts thumb caches
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, img, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	var old sql.NullString
	s.db.QueryRowContext(ctx, `SELECT path FROM user_covers WHERE album_id=?`, albumID).Scan(&old)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO user_covers(album_id, path, updated_at) VALUES (?,?,?)
		ON CONFLICT(album_id) DO UPDATE SET path=excluded.path, updated_at=excluded.updated_at`, albumID, path, stamp); err != nil {
		os.Remove(path)
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE albums SET cover_path=? WHERE id=?`, path, albumID); err != nil {
		return err
	}
	if old.Valid && old.String != path {
		os.Remove(old.String)
	}
	return nil
}

// ClearAlbumCover removes an uploaded cover; the next rescan/merge restores the scanned one.
func (s *Scanner) ClearAlbumCover(ctx context.Context, albumID string) error {
	var p string
	err := s.db.QueryRowContext(ctx, `SELECT path FROM user_covers WHERE album_id=?`, albumID).Scan(&p)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM user_covers WHERE album_id=?`, albumID); err != nil {
		return err
	}
	s.db.ExecContext(ctx, `UPDATE albums SET cover_path=NULL WHERE id=? AND cover_path=?`, albumID, p)
	os.Remove(p)
	return nil
}

// applyUserCovers re-points albums at uploaded covers (called at the end of Merge).
func applyUserCovers(ctx context.Context, tx querier) error {
	_, err := tx.ExecContext(ctx, `UPDATE albums SET cover_path=(SELECT path FROM user_covers u WHERE u.album_id=albums.id)
		WHERE id IN (SELECT album_id FROM user_covers)`)
	return err
}
