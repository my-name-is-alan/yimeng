package userdata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// GetPref decodes the owner's preference key into v; ok=false when unset.
func (s *Store) GetPref(ctx context.Context, owner, key string, v any) (bool, error) {
	var raw string
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM user_prefs WHERE owner=? AND key=?`, owner, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal([]byte(raw), v)
}

// SetPref stores v (JSON) as the owner's preference key.
func (s *Store) SetPref(ctx context.Context, owner, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO user_prefs(owner, key, value, updated_at) VALUES (?,?,?,?)
		ON CONFLICT(owner, key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, owner, key, string(b), s.now().UnixMilli())
	return err
}

// OwnersWithPref lists owners that have key set (for background jobs).
func (s *Store) OwnersWithPref(ctx context.Context, key string) (map[string]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT owner, value FROM user_prefs WHERE key=?`, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var o, v string
		if err := rows.Scan(&o, &v); err != nil {
			return nil, err
		}
		out[o] = v
	}
	return out, rows.Err()
}
