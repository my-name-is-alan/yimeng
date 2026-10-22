package library

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func applyPatch(ctx context.Context, q querier, typ, id string, patch map[string]any) error {
	allowed := map[string]bool{}
	for _, col := range overrideFields[typ] {
		allowed[col] = true
	}
	table := map[string]string{"track": "tracks", "album": "albums", "artist": "artists"}[typ]
	sets, args := []string{}, []any{}
	for k, v := range patch {
		if !allowed[k] {
			continue
		}
		sets = append(sets, k+"=?")
		args = append(args, v)
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "updated_at=?")
	args = append(args, time.Now().UnixMilli(), id)
	res, err := q.ExecContext(ctx, `UPDATE `+table+` SET `+strings.Join(sets, ", ")+` WHERE id=?`, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetLibraryRoots replaces the runtime roots (also persisted in settings).
func (s *Scanner) SetLibraryRoots(ctx context.Context, roots []string) error {
	if len(roots) == 0 {
		return fmt.Errorf("%w: at least one root required", ErrBadInput)
	}
	clean := make([]string, 0, len(roots))
	seen := map[string]bool{}
	for _, r := range roots {
		r = strings.TrimSpace(r)
		if r == "" || !strings.HasPrefix(r, "/") {
			return fmt.Errorf("%w: roots must be absolute paths", ErrBadInput)
		}
		if seen[r] {
			continue
		}
		seen[r] = true
		clean = append(clean, r)
	}
	b, _ := jsonMarshal(clean)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES ('library.roots', ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(b)); err != nil {
		return err
	}
	s.opts.Roots = clean
	return nil
}

func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}
