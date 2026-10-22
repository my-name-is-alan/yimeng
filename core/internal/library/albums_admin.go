package library

import (
	"context"
	"fmt"
	"time"
)

// MergeAlbums moves every track from sources into target and deletes empty source albums.
func (s *Scanner) MergeAlbums(ctx context.Context, target string, sources []string) (Album, error) {
	if target == "" || len(sources) == 0 {
		return Album{}, fmt.Errorf("%w: target and sources required", ErrBadInput)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Album{}, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM albums WHERE id=?`, target).Scan(&n); err != nil || n == 0 {
		return Album{}, ErrNotFound
	}
	now := time.Now().UnixMilli()
	for _, src := range sources {
		if src == target {
			continue
		}
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM albums WHERE id=?`, src).Scan(&n); err != nil || n == 0 {
			return Album{}, fmt.Errorf("%w: unknown album %q", ErrBadInput, src)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE tracks SET album_id=?, updated_at=? WHERE album_id=?`, target, now, src); err != nil {
			return Album{}, err
		}
		tx.ExecContext(ctx, `DELETE FROM album_artists WHERE album_id=?`, src)
		tx.ExecContext(ctx, `DELETE FROM albums WHERE id=?`, src)
		tx.ExecContext(ctx, `DELETE FROM metadata_overrides WHERE entity_type='album' AND entity_id=?`, src)
	}
	tx.ExecContext(ctx, `UPDATE albums SET track_count=(SELECT count(*) FROM tracks WHERE album_id=albums.id),
		duration_ms=(SELECT COALESCE(sum(duration_ms),0) FROM tracks WHERE album_id=albums.id), updated_at=? WHERE id=?`, now, target)
	if err := tx.Commit(); err != nil {
		return Album{}, err
	}
	return s.GetAlbum(ctx, target)
}

// SplitAlbum moves the given tracks into a new album (or an existing target).
func (s *Scanner) SplitAlbum(ctx context.Context, source string, trackIDs []string, title, artistName string, targetID string) (Album, error) {
	if source == "" || len(trackIDs) == 0 {
		return Album{}, fmt.Errorf("%w: source and track_ids required", ErrBadInput)
	}
	if targetID == "" {
		var nerr error
		title, nerr = normName(title)
		if nerr != nil {
			return Album{}, fmt.Errorf("%w: title required for a new album", ErrBadInput)
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Album{}, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM albums WHERE id=?`, source).Scan(&n); err != nil || n == 0 {
		return Album{}, ErrNotFound
	}
	now := time.Now().UnixMilli()
	dest := targetID
	if dest == "" {
		dest = hid("al_", "split:"+source+":"+title+":"+fmt.Sprint(now))
		if artistName == "" {
			tx.QueryRowContext(ctx, `SELECT COALESCE(artist_name,'') FROM albums WHERE id=?`, source).Scan(&artistName)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO albums(id, title, artist_name, created_at, updated_at, track_count, duration_ms)
			VALUES (?,?,?,?,?,0,0)`, dest, title, artistName, now, now); err != nil {
			return Album{}, err
		}
	} else if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM albums WHERE id=?`, dest).Scan(&n); err != nil || n == 0 {
		return Album{}, fmt.Errorf("%w: unknown target album", ErrBadInput)
	}
	for _, tid := range trackIDs {
		res, err := tx.ExecContext(ctx, `UPDATE tracks SET album_id=?, updated_at=? WHERE id=? AND album_id=?`, dest, now, tid, source)
		if err != nil {
			return Album{}, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return Album{}, fmt.Errorf("%w: track %q not on source album", ErrBadInput, tid)
		}
	}
	for _, id := range []string{source, dest} {
		tx.ExecContext(ctx, `UPDATE albums SET track_count=(SELECT count(*) FROM tracks WHERE album_id=albums.id),
			duration_ms=(SELECT COALESCE(sum(duration_ms),0) FROM tracks WHERE album_id=albums.id), updated_at=? WHERE id=?`, now, id)
	}
	if err := tx.Commit(); err != nil {
		return Album{}, err
	}
	return s.GetAlbum(ctx, dest)
}
