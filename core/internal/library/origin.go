package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// OriginLocal marks catalog items built from files under a library root.
const OriginLocal = "local"

var originTables = map[string]string{"track": "tracks", "album": "albums", "artist": "artists"}

// Origins returns the origin of each existing id of kind (track|album|artist).
// Unknown ids are absent from the map.
func (s *Scanner) Origins(ctx context.Context, kind string, ids []string) (map[string]string, error) {
	tbl, ok := originTables[kind]
	if !ok {
		return nil, fmt.Errorf("%w: unknown type %q", ErrBadInput, kind)
	}
	out := make(map[string]string, len(ids))
	for _, id := range ids {
		if _, seen := out[id]; seen {
			continue
		}
		var o string
		err := s.db.QueryRowContext(ctx, `SELECT origin FROM `+tbl+` WHERE id=?`, id).Scan(&o)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[id] = o
	}
	return out, nil
}

// SetOrigin marks an item's origin (for online source importers and tests).
func (s *Scanner) SetOrigin(ctx context.Context, kind, id, origin string) error {
	tbl, ok := originTables[kind]
	if !ok || origin == "" {
		return fmt.Errorf("%w: bad origin target", ErrBadInput)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE `+tbl+` SET origin=? WHERE id=?`, origin, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
