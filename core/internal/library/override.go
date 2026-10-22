package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Allowed override fields per entity (whitelist — never raw SQL).
var overrideFields = map[string]map[string]string{
	"track": {
		"title": "title", "artist_name": "artist_name", "album_id": "album_id",
		"disc_no": "disc_no", "track_no": "track_no", "year": "year", "genre": "genre",
	},
	"album": {
		"title": "title", "artist_name": "artist_name", "year": "year", "genre": "genre",
	},
	"artist": {
		"name": "name", "sort_name": "sort_name",
	},
}

// SetOverride merges patch into the stored override and applies it to the canonical row.
func (s *Scanner) SetOverride(ctx context.Context, typ, id string, patch map[string]any) error {
	fields, ok := overrideFields[typ]
	if !ok {
		return fmt.Errorf("%w: type must be track|album|artist", ErrBadInput)
	}
	clean := map[string]any{}
	for k, v := range patch {
		col, ok := fields[k]
		if !ok {
			return fmt.Errorf("%w: field %q not overridable on %s", ErrBadInput, k, typ)
		}
		switch col {
		case "disc_no", "track_no", "year":
			n, ok := asInt(v)
			if !ok {
				return fmt.Errorf("%w: %s must be an integer", ErrBadInput, k)
			}
			clean[col] = n
		case "album_id":
			str, _ := v.(string)
			if str != "" {
				var n int
				if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM albums WHERE id=?`, str).Scan(&n); err != nil || n == 0 {
					return fmt.Errorf("%w: unknown album", ErrBadInput)
				}
			}
			clean[col] = nullEmpty(str)
		default:
			str, ok := v.(string)
			if !ok {
				return fmt.Errorf("%w: %s must be a string", ErrBadInput, k)
			}
			str = strings.TrimSpace(str)
			if len(str) > 500 {
				return fmt.Errorf("%w: %s too long", ErrBadInput, k)
			}
			if col == "name" || col == "title" {
				if str == "" {
					return fmt.Errorf("%w: %s required", ErrBadInput, k)
				}
			}
			clean[col] = str
		}
	}
	if len(clean) == 0 {
		return fmt.Errorf("%w: empty patch", ErrBadInput)
	}
	table := map[string]string{"track": "tracks", "album": "albums", "artist": "artists"}[typ]
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM `+table+` WHERE id=?`, id).Scan(&n); err != nil || n == 0 {
		return ErrNotFound
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existing sql.NullString
	tx.QueryRowContext(ctx, `SELECT patch_json FROM metadata_overrides WHERE entity_type=? AND entity_id=?`, typ, id).Scan(&existing)
	merged := map[string]any{}
	if existing.Valid {
		json.Unmarshal([]byte(existing.String), &merged)
	}
	for k, v := range clean {
		merged[k] = v
	}
	b, _ := json.Marshal(merged)
	if _, err := tx.ExecContext(ctx, `INSERT INTO metadata_overrides(entity_type, entity_id, patch_json, updated_at) VALUES (?,?,?,?)
		ON CONFLICT(entity_type, entity_id) DO UPDATE SET patch_json=excluded.patch_json, updated_at=excluded.updated_at`,
		typ, id, string(b), time.Now().UnixMilli()); err != nil {
		return err
	}
	if err := applyPatch(ctx, tx, typ, id, clean); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Scanner) ClearOverride(ctx context.Context, typ, id string) error {
	if _, ok := overrideFields[typ]; !ok {
		return fmt.Errorf("%w: type must be track|album|artist", ErrBadInput)
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM metadata_overrides WHERE entity_type=? AND entity_id=?`, typ, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	// next Merge / rescan restores tag values
	return nil
}

func (s *Scanner) GetOverride(ctx context.Context, typ, id string) (map[string]any, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT patch_json FROM metadata_overrides WHERE entity_type=? AND entity_id=?`, typ, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	json.Unmarshal([]byte(raw), &out)
	return out, nil
}

// applyOverrides reapplies every stored override (called at the end of Merge).
func (s *Scanner) applyOverrides(ctx context.Context, tx querier) error {
	rows, err := tx.QueryContext(ctx, `SELECT entity_type, entity_id, patch_json FROM metadata_overrides`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var typ, id, raw string
		if err := rows.Scan(&typ, &id, &raw); err != nil {
			return err
		}
		patch := map[string]any{}
		json.Unmarshal([]byte(raw), &patch)
		if err := applyPatch(ctx, tx, typ, id, patch); err != nil {
			// entity may have been deleted by merge — drop the override
			tx.ExecContext(ctx, `DELETE FROM metadata_overrides WHERE entity_type=? AND entity_id=?`, typ, id)
		}
	}
	return rows.Err()
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), n == float64(int(n))
	case int:
		return n, true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	}
	return 0, false
}

func nullEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
