package library

import (
	"context"
	"strings"
	"time"
)

// SetExternalStatus records the last known entitlement of stored online
// tracks (ids not in the tracks table are ignored).
func (s *Scanner) SetExternalStatus(ctx context.Context, st map[string]string) error {
	now := time.Now().UnixMilli()
	for id, ent := range st {
		if !IsExternalID(id) || ent == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO external_status(track_id, entitlement, checked_at)
			SELECT ?, ?, ? WHERE EXISTS (SELECT 1 FROM tracks WHERE id=?)
			ON CONFLICT(track_id) DO UPDATE SET entitlement=excluded.entitlement, checked_at=excluded.checked_at`, id, ent, now, id); err != nil {
			return err
		}
	}
	return nil
}

// ExternalStatus returns the recorded entitlement per id (missing = unknown).
func (s *Scanner) ExternalStatus(ctx context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	var ext []any
	for _, id := range ids {
		if IsExternalID(id) {
			ext = append(ext, id)
		}
	}
	for len(ext) > 0 {
		n := min(len(ext), 500)
		chunk := ext[:n]
		ext = ext[n:]
		rows, err := s.db.QueryContext(ctx, `SELECT track_id, entitlement FROM external_status WHERE track_id IN (?`+strings.Repeat(",?", len(chunk)-1)+`)`, chunk...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id, ent string
			if err := rows.Scan(&id, &ent); err != nil {
				rows.Close()
				return nil, err
			}
			out[id] = ent
		}
		rows.Close()
	}
	return out, nil
}
