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
)

// SetArtistImage stores a user artist image (upload or from a match) in
// <cover dir>/artists. It lives in artist_images, keyed by the artist id, so
// it survives rescans exactly like an uploaded album cover.
func (s *Scanner) SetArtistImage(ctx context.Context, artistID string, img []byte, source string) error {
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
	var origin string
	if err := s.db.QueryRowContext(ctx, `SELECT origin FROM artists WHERE id=?`, artistID).Scan(&origin); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if origin != "local" {
		return fmt.Errorf("%w: not a local artist", ErrBadInput)
	}
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return -1
	}, artistID)
	if name == "" {
		return ErrNotFound
	}
	if source == "" {
		source = "upload"
	}
	dir := filepath.Join(s.opts.CoverDir, "artists")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	stamp := time.Now().UnixMilli()
	path := filepath.Join(dir, fmt.Sprintf("%s-%d%s", name, stamp, ext))
	if err := os.WriteFile(path+".tmp", img, 0o644); err != nil {
		return err
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		return err
	}
	var old sql.NullString
	s.db.QueryRowContext(ctx, `SELECT path FROM artist_images WHERE artist_id=?`, artistID).Scan(&old)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO artist_images(artist_id, path, source, updated_at) VALUES (?,?,?,?)
		ON CONFLICT(artist_id) DO UPDATE SET path=excluded.path, source=excluded.source, updated_at=excluded.updated_at`,
		artistID, path, source, stamp); err != nil {
		os.Remove(path)
		return err
	}
	if old.Valid && old.String != path {
		os.Remove(old.String)
	}
	return nil
}

// ClearArtistImage removes a user artist image.
func (s *Scanner) ClearArtistImage(ctx context.Context, artistID string) error {
	var p string
	err := s.db.QueryRowContext(ctx, `SELECT path FROM artist_images WHERE artist_id=?`, artistID).Scan(&p)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM artist_images WHERE artist_id=?`, artistID); err != nil {
		return err
	}
	os.Remove(p)
	return nil
}

// ArtistImage returns the stored image path (ErrNotFound when none).
func (s *Scanner) ArtistImage(ctx context.Context, artistID string) (string, error) {
	var p string
	err := s.db.QueryRowContext(ctx, `SELECT path FROM artist_images WHERE artist_id=?`, artistID).Scan(&p)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return p, err
}
