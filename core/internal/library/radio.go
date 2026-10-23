package library

import (
	"context"
	"database/sql"
)

// RadioSeeds returns up to limit track IDs similar to seed (same artist, then
// same genre), excluding exclude. Used for auto-continue / radio.
func (s *Scanner) RadioSeeds(ctx context.Context, seedTrackID string, limit int, exclude []string) ([]string, error) {
	if limit <= 0 {
		limit = 25
	}
	ex := map[string]bool{seedTrackID: true}
	for _, id := range exclude {
		ex[id] = true
	}
	var artist, genre string
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(artist_name,''), COALESCE(genre,'') FROM tracks WHERE id=?`, seedTrackID).
		Scan(&artist, &genre)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM tracks WHERE artist_name=? AND id!=? ORDER BY RANDOM() LIMIT ?`, artist, seedTrackID, limit*2)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if ex[id] {
			continue
		}
		out = append(out, id)
		if len(out) >= limit {
			return out, nil
		}
	}
	if genre != "" && len(out) < limit {
		rows2, err := s.db.QueryContext(ctx, `SELECT id FROM tracks WHERE genre=? AND id!=? ORDER BY RANDOM() LIMIT ?`, genre, seedTrackID, limit)
		if err != nil {
			return out, err
		}
		defer rows2.Close()
		for rows2.Next() {
			var id string
			rows2.Scan(&id)
			if ex[id] {
				continue
			}
			out = append(out, id)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, rows.Err()
}
