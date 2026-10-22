package userdata

import (
	"context"
	"strings"
	"time"
)

// Progress is a synced resume point.
type Progress struct {
	TrackID    string    `json:"track_id"`
	PositionMs int64     `json:"position_ms"`
	DurationMs int64     `json:"duration_ms,omitempty"`
	Finished   bool      `json:"finished"`
	DeviceID   string    `json:"device_id,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// SetProgress stores a resume point. Older writes (by updated_at) lose, so
// an offline device syncing late can't rewind a newer position.
func (s *Store) SetProgress(ctx context.Context, owner string, p Progress) (Progress, error) {
	if p.TrackID == "" || p.PositionMs < 0 || len(p.TrackID) > 200 {
		return p, ErrBad
	}
	if p.UpdatedAt.IsZero() || p.UpdatedAt.After(s.now().Add(time.Minute)) {
		p.UpdatedAt = s.now()
	}
	p.UpdatedAt = p.UpdatedAt.UTC().Truncate(time.Millisecond)
	_, err := s.DB.ExecContext(ctx, `INSERT INTO playback_progress(owner, track_id, position_ms, duration_ms, finished, device_id, updated_at)
		VALUES (?,?,?,?,?,?,?) ON CONFLICT(owner, track_id) DO UPDATE SET position_ms=excluded.position_ms, duration_ms=excluded.duration_ms,
		finished=excluded.finished, device_id=excluded.device_id, updated_at=excluded.updated_at WHERE excluded.updated_at >= playback_progress.updated_at`,
		owner, p.TrackID, p.PositionMs, p.DurationMs, p.Finished, p.DeviceID, p.UpdatedAt.UnixMilli())
	if err != nil {
		return p, err
	}
	got, err := s.GetProgress(ctx, owner, []string{p.TrackID})
	if err != nil || len(got) == 0 {
		return p, err
	}
	return got[0], nil
}

// GetProgress returns resume points for ids (all recent ones when ids is empty, max 200).
func (s *Store) GetProgress(ctx context.Context, owner string, ids []string) ([]Progress, error) {
	q := `SELECT track_id, position_ms, duration_ms, finished, device_id, updated_at FROM playback_progress WHERE owner=?`
	args := []any{owner}
	if len(ids) > 0 {
		if len(ids) > 500 {
			ids = ids[:500]
		}
		q += ` AND track_id IN (?` + strings.Repeat(",?", len(ids)-1) + `)`
		for _, id := range ids {
			args = append(args, id)
		}
	}
	q += ` ORDER BY updated_at DESC LIMIT 500`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Progress{}
	for rows.Next() {
		var p Progress
		var u int64
		if err := rows.Scan(&p.TrackID, &p.PositionMs, &p.DurationMs, &p.Finished, &p.DeviceID, &u); err != nil {
			return nil, err
		}
		p.UpdatedAt = time.UnixMilli(u).UTC()
		out = append(out, p)
	}
	return out, rows.Err()
}

// DeleteProgress clears a resume point.
func (s *Store) DeleteProgress(ctx context.Context, owner, trackID string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM playback_progress WHERE owner=? AND track_id=?`, owner, trackID)
	return err
}
