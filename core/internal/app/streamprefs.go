package app

import (
	"context"
	"database/sql"
	"errors"

	"github.com/my-name-is-alan/yimeng/core/internal/transcode"
)

// StreamPrefs persists each device's default stream quality (players.stream_format/stream_bitrate).
type StreamPrefs struct{ DB *sql.DB }

var ErrNoDevice = errors.New("device not found")

func (s StreamPrefs) Get(ctx context.Context, deviceID string) (transcode.Profile, error) {
	var f sql.NullString
	var b sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT stream_format, stream_bitrate FROM players WHERE id=?`, deviceID).Scan(&f, &b)
	if errors.Is(err, sql.ErrNoRows) {
		return transcode.Profile{}, ErrNoDevice
	}
	if err != nil || !f.Valid {
		return transcode.Profile{Format: transcode.Original}, err
	}
	return transcode.Profile{Format: f.String, BitrateKbps: int(b.Int64)}, nil
}

// Set stores an already validated profile.
func (s StreamPrefs) Set(ctx context.Context, deviceID string, p transcode.Profile) error {
	var f, b any
	if !p.IsOriginal() {
		f = p.Format
		if p.BitrateKbps > 0 {
			b = p.BitrateKbps
		}
	}
	r, err := s.DB.ExecContext(ctx, `UPDATE players SET stream_format=?, stream_bitrate=? WHERE id=?`, f, b, deviceID)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNoDevice
	}
	return nil
}
