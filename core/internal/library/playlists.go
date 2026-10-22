package library

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/my-name-is-alan/yimeng/core/internal/owner"
	"io"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

var ErrSmartReadOnly = errors.New("smart playlists have no editable items")

type Playlist struct {
	ID         string    `json:"id"`
	OwnerID    string    `json:"owner_id"`
	Name       string    `json:"name"`
	Comment    string    `json:"comment,omitempty"`
	Smart      bool      `json:"smart"`
	Rules      *Rules    `json:"rules,omitempty"`
	TrackCount int       `json:"track_count"`
	DurationMs int64     `json:"duration_ms"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Origin     string    `json:"origin"`          // "local" (Core playlist)
	Cover      *CoverRef `json:"cover,omitempty"` // set by the gateway
}

type PlaylistPage struct {
	Playlist
	Tracks  Page[Track] `json:"tracks"`
	Missing int         `json:"missing"` // static items whose track is not in the library now
}

type PlaylistInput struct {
	Name     *string  `json:"name"`
	Comment  *string  `json:"comment"`
	Rules    *Rules   `json:"rules"`
	TrackIDs []string `json:"track_ids"`
}

const maxPlaylist = 10000

func (s *Scanner) loadPlaylist(ctx context.Context, q querier, id string) (Playlist, error) {
	var p Playlist
	var comment, rules sql.NullString
	var c, u int64
	// D49: a playlist is visible to its owner; admins (and internal callers
	// without a request identity) may reach any playlist by id.
	o := owner.From(ctx)
	err := q.QueryRowContext(ctx, `SELECT id, owner_id, name, comment, rules, created_at, updated_at FROM playlists
		WHERE id=? AND (owner_id=? OR ?)`, id, o.ID, !o.Set || o.Admin).
		Scan(&p.ID, &p.OwnerID, &p.Name, &comment, &rules, &c, &u)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, err
	}
	p.Origin = OriginLocal
	p.Comment, p.CreatedAt, p.UpdatedAt = comment.String, time.UnixMilli(c).UTC(), time.UnixMilli(u).UTC()
	if rules.Valid {
		p.Smart = true
		p.Rules = &Rules{}
		json.Unmarshal([]byte(rules.String), p.Rules)
	}
	return p, nil
}

type querier interface {
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
}

// smartIDs evaluates rules to an ordered track id list.
func (s *Scanner) smartIDs(ctx context.Context, r Rules) ([]string, error) {
	where, args, order, limit, err := r.Compile(time.Now())
	if err != nil {
		return nil, err
	}
	if where != "" {
		where = " WHERE " + where
	}
	rows, err := s.db.QueryContext(ctx, `SELECT t.id`+trackFrom(ctx)+where+order+` LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// PlaylistTrackIDs returns the playable track ids in order (static: only
// tracks currently in the library; smart: evaluated now).
func (s *Scanner) PlaylistTrackIDs(ctx context.Context, id string) ([]string, error) {
	p, err := s.loadPlaylist(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	if p.Smart {
		return s.smartIDs(ctx, *p.Rules)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT pi.track_id FROM playlist_items pi JOIN tracks t ON t.id=pi.track_id
		WHERE pi.playlist_id=? ORDER BY pi.pos`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var t string
		rows.Scan(&t)
		ids = append(ids, t)
	}
	return ids, rows.Err()
}

func (s *Scanner) fillCounts(ctx context.Context, p *Playlist) error {
	ids, err := s.PlaylistTrackIDs(ctx, p.ID)
	if err != nil {
		return err
	}
	p.TrackCount = len(ids)
	if p.Smart {
		// duration of the evaluated set
		for i := 0; i < len(ids); i += 500 {
			j := min(i+500, len(ids))
			ph := strings.Repeat(",?", j-i)[1:]
			args := make([]any, j-i)
			for k, id := range ids[i:j] {
				args[k] = id
			}
			var d int64
			s.db.QueryRowContext(ctx, `SELECT COALESCE(sum(duration_ms),0) FROM tracks WHERE id IN (`+ph+`)`, args...).Scan(&d)
			p.DurationMs += d
		}
		return nil
	}
	return s.db.QueryRowContext(ctx, `SELECT COALESCE(sum(t.duration_ms),0) FROM playlist_items pi JOIN tracks t ON t.id=pi.track_id
		WHERE pi.playlist_id=?`, p.ID).Scan(&p.DurationMs)
}

func (s *Scanner) ListPlaylists(ctx context.Context) ([]Playlist, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM playlists WHERE owner_id=? ORDER BY lower(name), id`, owner.From(ctx).ID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	out := []Playlist{}
	for _, id := range ids {
		p, err := s.loadPlaylist(ctx, s.db, id)
		if err != nil {
			return nil, err
		}
		if err := s.fillCounts(ctx, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// GetPlaylist returns the playlist with one page of its tracks.
func (s *Scanner) GetPlaylist(ctx context.Context, id string, limit, offset int) (PlaylistPage, error) {
	p, err := s.loadPlaylist(ctx, s.db, id)
	if err != nil {
		return PlaylistPage{}, err
	}
	if err := s.fillCounts(ctx, &p); err != nil {
		return PlaylistPage{}, err
	}
	lp := ListParams{Limit: limit, Offset: offset}
	lp.norm()
	out := PlaylistPage{Playlist: p, Tracks: Page[Track]{Items: []Track{}, Total: p.TrackCount, Limit: lp.Limit, Offset: lp.Offset}}
	ids, err := s.PlaylistTrackIDs(ctx, id)
	if err != nil {
		return out, err
	}
	if !p.Smart {
		var all int
		s.db.QueryRowContext(ctx, `SELECT count(*) FROM playlist_items WHERE playlist_id=?`, id).Scan(&all)
		out.Missing = all - len(ids)
	}
	if lp.Offset < len(ids) {
		for _, tid := range ids[lp.Offset:min(len(ids), lp.Offset+lp.Limit)] {
			t, err := s.GetTrack(ctx, tid)
			if err != nil {
				return out, err
			}
			out.Tracks.Items = append(out.Tracks.Items, t)
		}
	}
	return out, nil
}

func (s *Scanner) validTrackIDs(ctx context.Context, q querier, ids []string) error {
	if len(ids) > maxPlaylist {
		return fmt.Errorf("%w: at most %d tracks", ErrBadInput, maxPlaylist)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		var n int
		if err := q.QueryRowContext(ctx, `SELECT count(*) FROM tracks WHERE id=?`, id).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("%w: unknown track %q", ErrBadInput, id)
		}
	}
	return nil
}

var ErrBadInput = errors.New("invalid input")

func normName(n string) (string, error) {
	n = strings.TrimSpace(n)
	if n == "" || len(n) > 200 {
		return "", fmt.Errorf("%w: name must be 1-200 characters", ErrBadInput)
	}
	return n, nil
}

func (s *Scanner) CreatePlaylist(ctx context.Context, in PlaylistInput) (Playlist, error) {
	if err := s.EnsureExternal(ctx, in.TrackIDs); err != nil {
		return Playlist{}, fmt.Errorf("%w: %v", ErrBadInput, err)
	}
	if in.Name == nil {
		return Playlist{}, fmt.Errorf("%w: name required", ErrBadInput)
	}
	name, err := normName(*in.Name)
	if err != nil {
		return Playlist{}, err
	}
	var rules any
	if in.Rules != nil {
		if len(in.TrackIDs) > 0 {
			return Playlist{}, fmt.Errorf("%w: a smart playlist cannot have track_ids", ErrBadInput)
		}
		if _, _, _, _, err := in.Rules.Compile(time.Now()); err != nil {
			return Playlist{}, err
		}
		b, _ := json.Marshal(in.Rules)
		rules = string(b)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Playlist{}, err
	}
	defer tx.Rollback()
	if err := s.validTrackIDs(ctx, tx, in.TrackIDs); err != nil {
		return Playlist{}, err
	}
	id := "pl_" + newID()[:20]
	now := time.Now().UnixMilli()
	comment := ""
	if in.Comment != nil {
		comment = *in.Comment
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO playlists(id, owner_id, name, comment, rules, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`,
		id, owner.From(ctx).ID, name, nz(comment), rules, now, now); err != nil {
		return Playlist{}, err
	}
	if err := writeItems(ctx, tx, id, in.TrackIDs); err != nil {
		return Playlist{}, err
	}
	if err := tx.Commit(); err != nil {
		return Playlist{}, err
	}
	p, err := s.loadPlaylist(ctx, s.db, id)
	if err == nil {
		err = s.fillCounts(ctx, &p)
	}
	return p, err
}

func writeItems(ctx context.Context, tx querier, id string, ids []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM playlist_items WHERE playlist_id=?`, id); err != nil {
		return err
	}
	for i, t := range ids {
		if _, err := tx.ExecContext(ctx, `INSERT INTO playlist_items(playlist_id, pos, track_id) VALUES (?,?,?)`, id, i, t); err != nil {
			return err
		}
	}
	return nil
}

// UpdatePlaylist changes name/comment/rules (rules only on smart playlists).
func (s *Scanner) UpdatePlaylist(ctx context.Context, id string, in PlaylistInput) (Playlist, error) {
	if err := s.EnsureExternal(ctx, in.TrackIDs); err != nil {
		return Playlist{}, fmt.Errorf("%w: %v", ErrBadInput, err)
	}
	p, err := s.loadPlaylist(ctx, s.db, id)
	if err != nil {
		return p, err
	}
	sets, args := []string{}, []any{}
	if in.Name != nil {
		n, err := normName(*in.Name)
		if err != nil {
			return p, err
		}
		sets, args = append(sets, "name=?"), append(args, n)
	}
	if in.Comment != nil {
		sets, args = append(sets, "comment=?"), append(args, nz(*in.Comment))
	}
	if in.Rules != nil {
		if !p.Smart {
			return p, fmt.Errorf("%w: rules can only be changed on a smart playlist", ErrBadInput)
		}
		if _, _, _, _, err := in.Rules.Compile(time.Now()); err != nil {
			return p, err
		}
		b, _ := json.Marshal(in.Rules)
		sets, args = append(sets, "rules=?"), append(args, string(b))
	}
	if len(in.TrackIDs) > 0 {
		return p, fmt.Errorf("%w: use the items endpoint to change tracks", ErrBadInput)
	}
	if len(sets) > 0 {
		sets = append(sets, "updated_at=?")
		args = append(args, time.Now().UnixMilli(), id)
		if _, err := s.db.ExecContext(ctx, `UPDATE playlists SET `+strings.Join(sets, ", ")+` WHERE id=?`, args...); err != nil {
			return p, err
		}
	}
	p, err = s.loadPlaylist(ctx, s.db, id)
	if err == nil {
		err = s.fillCounts(ctx, &p)
	}
	return p, err
}

func (s *Scanner) DeletePlaylist(ctx context.Context, id string) error {
	o := owner.From(ctx)
	res, err := s.db.ExecContext(ctx, `DELETE FROM playlists WHERE id=? AND (owner_id=? OR ?)`, id, o.ID, !o.Set || o.Admin)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

type PlaylistOp struct {
	Op       string   `json:"op"` // replace | append | insert | remove | move | clear
	TrackIDs []string `json:"track_ids,omitempty"`
	Index    int      `json:"index,omitempty"`
	From     int      `json:"from,omitempty"`
	To       int      `json:"to,omitempty"`
	// Id-based addressing (apps): move track_id to position / before / after
	// another item; insert before/after; remove by track_ids.
	TrackID  string `json:"track_id,omitempty"`
	Position *int   `json:"position,omitempty"`
	Before   string `json:"before,omitempty"`
	After    string `json:"after,omitempty"`
}

// EditPlaylist applies an item operation to a static playlist. Indices refer
// to stored items (including currently missing tracks).
func (s *Scanner) EditPlaylist(ctx context.Context, id string, op PlaylistOp) (Playlist, error) {
	if err := s.EnsureExternal(ctx, op.TrackIDs); err != nil {
		return Playlist{}, fmt.Errorf("%w: %v", ErrBadInput, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Playlist{}, err
	}
	defer tx.Rollback()
	p, err := s.loadPlaylist(ctx, tx, id)
	if err != nil {
		return p, err
	}
	if p.Smart {
		return p, ErrSmartReadOnly
	}
	rows, err := tx.QueryContext(ctx, `SELECT track_id FROM playlist_items WHERE playlist_id=? ORDER BY pos`, id)
	if err != nil {
		return p, err
	}
	var items []string
	for rows.Next() {
		var t string
		rows.Scan(&t)
		items = append(items, t)
	}
	rows.Close()
	n := len(items)
	bad := func(m string) (Playlist, error) { return p, fmt.Errorf("%w: %s", ErrBadInput, m) }
	indexOf := func(list []string, id string) int {
		for i, t := range list {
			if t == id {
				return i
			}
		}
		return -1
	}
	// target resolves before/after/position against list (-1 = invalid)
	target := func(list []string) int {
		switch {
		case op.Before != "":
			return indexOf(list, op.Before)
		case op.After != "":
			if i := indexOf(list, op.After); i >= 0 {
				return i + 1
			}
			return -1
		case op.Position != nil:
			if *op.Position < 0 || *op.Position > len(list) {
				return -1
			}
			return *op.Position
		}
		return -2 // not given
	}
	switch {
	case op.Op == "move" && op.TrackID != "":
		from := indexOf(items, op.TrackID)
		if from < 0 {
			return bad("track_id not in playlist")
		}
		rest := append(append([]string{}, items[:from]...), items[from+1:]...)
		to := target(rest)
		if to < 0 {
			return bad("give position, before or after (an item of this playlist)")
		}
		items = append(rest[:to], append([]string{op.TrackID}, rest[to:]...)...)
		op.Op = "done"
	case op.Op == "insert" && (op.Before != "" || op.After != "" || op.Position != nil):
		at := target(items)
		if at < 0 {
			return bad("before/after/position not in playlist")
		}
		items = append(items[:at], append(append([]string{}, op.TrackIDs...), items[at:]...)...)
		op.Op = "done"
	case op.Op == "remove" && len(op.TrackIDs) > 0:
		drop := map[string]bool{}
		for _, t := range op.TrackIDs {
			drop[t] = true
		}
		kept := items[:0:0]
		for _, t := range items {
			if !drop[t] {
				kept = append(kept, t)
			}
		}
		items = kept
		op.TrackIDs = nil
		op.Op = "done"
	}
	switch op.Op {
	case "done":
	case "replace":
		items = append([]string(nil), op.TrackIDs...)
	case "append":
		items = append(items, op.TrackIDs...)
	case "insert":
		if op.Index < 0 || op.Index > n {
			return bad("index out of range")
		}
		items = append(items[:op.Index], append(append([]string{}, op.TrackIDs...), items[op.Index:]...)...)
	case "remove":
		if op.Index < 0 || op.Index >= n {
			return bad("index out of range")
		}
		items = append(items[:op.Index], items[op.Index+1:]...)
	case "move":
		if op.From < 0 || op.From >= n || op.To < 0 || op.To >= n {
			return bad("index out of range")
		}
		t := items[op.From]
		items = append(items[:op.From], items[op.From+1:]...)
		items = append(items[:op.To], append([]string{t}, items[op.To:]...)...)
	case "clear":
		items = nil
	default:
		return bad("op must be replace|append|insert|remove|move|clear")
	}
	if err := s.validTrackIDs(ctx, tx, op.TrackIDs); err != nil {
		return p, err
	}
	if len(items) > maxPlaylist {
		return bad(fmt.Sprintf("at most %d tracks", maxPlaylist))
	}
	if err := writeItems(ctx, tx, id, items); err != nil {
		return p, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE playlists SET updated_at=? WHERE id=?`, time.Now().UnixMilli(), id); err != nil {
		return p, err
	}
	if err := tx.Commit(); err != nil {
		return p, err
	}
	p, err = s.loadPlaylist(ctx, s.db, id)
	if err == nil {
		err = s.fillCounts(ctx, &p)
	}
	return p, err
}

// ExportM3U writes an extended M3U. relative=true writes paths relative to their library root.
func (s *Scanner) ExportM3U(ctx context.Context, id string, w io.Writer, relative bool) error {
	ids, err := s.PlaylistTrackIDs(ctx, id)
	if err != nil {
		return err
	}
	bw := bufio.NewWriter(w)
	fmt.Fprintln(bw, "#EXTM3U")
	for _, tid := range ids {
		t, err := s.GetTrack(ctx, tid)
		if err != nil {
			continue
		}
		var path, root string
		if err := s.db.QueryRowContext(ctx, `SELECT st.path, COALESCE(st.root,'') FROM track_links l JOIN source_tracks st ON st.id=l.source_track_id
			WHERE l.track_id=? AND st.path IS NOT NULL ORDER BY l.confidence DESC, CASE st.format WHEN 'flac' THEN 0 ELSE 1 END, st.path LIMIT 1`,
			tid).Scan(&path, &root); err != nil {
			continue
		}
		if relative && root != "" {
			if rel, err := filepath.Rel(root, path); err == nil {
				path = filepath.ToSlash(rel)
			}
		}
		fmt.Fprintf(bw, "#EXTINF:%d,%s - %s\n%s\n", t.DurationMs/1000, oneLine(t.ArtistName), oneLine(t.Title), oneLine(path))
	}
	return bw.Flush()
}

func oneLine(s string) string { return strings.NewReplacer("\n", " ", "\r", " ").Replace(s) }

type ImportResult struct {
	Playlist  Playlist `json:"playlist"`
	Matched   int      `json:"matched"`
	Unmatched []string `json:"unmatched"`
}

// ImportM3U creates a static playlist from M3U text. Entries are matched to
// library tracks only (files referenced by the playlist are never opened):
// exact path → path relative to a library root → unique file-name match →
// "#EXTINF: artist - title".
func (s *Scanner) ImportM3U(ctx context.Context, name string, r io.Reader) (ImportResult, error) {
	sc := bufio.NewScanner(io.LimitReader(r, 4<<20))
	sc.Buffer(make([]byte, 64<<10), 64<<10)
	var res ImportResult
	var ids []string
	var extinf string
	lines := 0
	for sc.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(sc.Text(), "\ufeff"))
		if line == "" {
			continue
		}
		if lines++; lines > 2*maxPlaylist {
			return res, fmt.Errorf("%w: too many lines", ErrBadInput)
		}
		if strings.HasPrefix(line, "#") {
			if strings.HasPrefix(line, "#EXTINF:") {
				if _, after, ok := strings.Cut(line, ","); ok {
					extinf = strings.TrimSpace(after)
				}
			}
			continue
		}
		if id := s.matchEntry(ctx, line, extinf); id != "" {
			ids = append(ids, id)
		} else {
			res.Unmatched = append(res.Unmatched, line)
		}
		extinf = ""
	}
	if err := sc.Err(); err != nil {
		return res, fmt.Errorf("%w: %v", ErrBadInput, err)
	}
	if len(ids) > maxPlaylist {
		ids = ids[:maxPlaylist]
	}
	p, err := s.CreatePlaylist(ctx, PlaylistInput{Name: &name, TrackIDs: ids})
	if err != nil {
		return res, err
	}
	res.Playlist, res.Matched = p, len(ids)
	if res.Unmatched == nil {
		res.Unmatched = []string{}
	}
	return res, nil
}

func (s *Scanner) trackForPath(ctx context.Context, p string) string {
	var id string
	s.db.QueryRowContext(ctx, `SELECT l.track_id FROM source_tracks st JOIN track_links l ON l.source_track_id=st.id
		WHERE st.path=? LIMIT 1`, p).Scan(&id)
	return id
}

func (s *Scanner) matchEntry(ctx context.Context, entry, extinf string) string {
	p := entry
	if strings.HasPrefix(strings.ToLower(p), "file://") {
		if u, err := url.Parse(p); err == nil {
			p = u.Path
		}
	} else if strings.Contains(p, "://") {
		p = "" // remote URL: only EXTINF matching
	}
	if p != "" {
		p = filepath.FromSlash(strings.ReplaceAll(p, `\`, "/"))
		if filepath.IsAbs(p) {
			if id := s.trackForPath(ctx, filepath.Clean(p)); id != "" {
				return id
			}
		} else {
			for _, root := range s.opts.Roots {
				if id := s.trackForPath(ctx, filepath.Join(root, p)); id != "" {
					return id
				}
			}
		}
		base := filepath.Base(p)
		esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(base)
		rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT l.track_id FROM source_tracks st JOIN track_links l ON l.source_track_id=st.id
			WHERE st.path LIKE ? ESCAPE '\' LIMIT 2`, "%"+string(filepath.Separator)+esc)
		if err == nil {
			var found []string
			for rows.Next() {
				var id string
				rows.Scan(&id)
				found = append(found, id)
			}
			rows.Close()
			if len(found) == 1 {
				return found[0]
			}
		}
	}
	if a, t, ok := strings.Cut(extinf, " - "); ok {
		var id string
		s.db.QueryRowContext(ctx, `SELECT id FROM tracks WHERE lower(title)=lower(?) AND lower(artist_name)=lower(?) LIMIT 1`,
			strings.TrimSpace(t), strings.TrimSpace(a)).Scan(&id)
		return id
	}
	return ""
}
