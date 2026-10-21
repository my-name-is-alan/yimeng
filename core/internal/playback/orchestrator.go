package playback

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/events"
)

// Transport states (docs §4.4).
const (
	Idle      = "idle"
	Buffering = "buffering"
	Playing   = "playing"
	Paused    = "paused"
)

const maxQueue = 5000

var (
	ErrPlayerNotFound = errors.New("player not found")
	ErrBadRequest     = errors.New("bad playback request")
	ErrEmptyQueue     = errors.New("queue is empty")
)

type TrackRef struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	ArtistName string `json:"artist_name"`
	AlbumID    string `json:"album_id,omitempty"`
	AlbumTitle string `json:"album_title,omitempty"`
	DurationMs int64  `json:"duration_ms"`
}

// Lease tells the client where to fetch audio (P0: Core's own Range endpoint).
type Lease struct {
	Delivery  Delivery  `json:"delivery"`
	URL       string    `json:"url"`
	Format    string    `json:"format,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
}

type State struct {
	PlayerID     string    `json:"player_id"`
	Transport    string    `json:"transport"`
	PositionMs   int64     `json:"position_ms"` // extrapolated to UpdatedAt when playing
	UpdatedAt    time.Time `json:"updated_at"`
	CurrentIndex int       `json:"current_index"` // -1 = none
	QueueLength  int       `json:"queue_length"`
	Repeat       string    `json:"repeat"` // off | all | one
	Shuffle      bool      `json:"shuffle"`
	Track        *TrackRef `json:"track"`
	Lease        *Lease    `json:"lease"`
	// Next is what plays after Track ends naturally (repeat/shuffle aware);
	// clients prefetch NextLease for gapless transitions.
	NextIndex int       `json:"next_index"` // -1 = none
	Next      *TrackRef `json:"next"`
	NextLease *Lease    `json:"next_lease"`
}

type QueueItem struct {
	Index int `json:"index"`
	TrackRef
}

type Queue struct {
	PlayerID     string `json:"player_id"`
	CurrentIndex int    `json:"current_index"`
	Shuffle      bool   `json:"shuffle"`
	// PlayOrder lists queue indices in the order they will play (identity when not shuffled).
	PlayOrder []int       `json:"play_order"`
	Items     []QueueItem `json:"items"`
}

// LeaseFunc resolves a playable lease for a canonical track.
type LeaseFunc func(ctx context.Context, trackID string) (Lease, error)

// Orchestrator owns player sessions and queues. All mutations are serialized,
// persisted (sessions / queue_items) and broadcast as player.state /
// queue.updated events so every connected client observes the same state.
type Orchestrator struct {
	db    *sql.DB
	hub   *events.Hub
	lease LeaseFunc
	now   func() time.Time
	mu    sync.Mutex
}

func NewOrchestrator(ctx context.Context, db *sql.DB, hub *events.Hub, lease LeaseFunc) (*Orchestrator, error) {
	o := &Orchestrator{db: db, hub: hub, lease: lease, now: time.Now}
	// No client is producing audio after a restart.
	_, err := db.ExecContext(ctx, `UPDATE sessions SET state='paused' WHERE state IN ('playing','buffering')`)
	return o, err
}

type session struct {
	state    string
	pos      int64
	updated  time.Time
	idx      int
	repeat   string
	queue    []string // track ids
	duration int64    // current track
	shuffle  bool
	order    []int // play order (queue indices) when shuffled
	seed     int64
}

func (o *Orchestrator) load(ctx context.Context, q querier, pid string) (*session, error) {
	var exists int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM players WHERE id=?`, pid).Scan(&exists); err != nil {
		return nil, err
	}
	if exists == 0 {
		return nil, ErrPlayerNotFound
	}
	s := &session{state: Idle, idx: -1, repeat: "off", updated: o.now()}
	var upd int64
	var order, seed sql.NullString
	err := q.QueryRowContext(ctx, `SELECT state, position_ms, updated_at, current_idx, repeat_mode, shuffle, shuffle_order,
		CAST(shuffle_seed AS TEXT) FROM sessions WHERE player_id=?`, pid).
		Scan(&s.state, &s.pos, &upd, &s.idx, &s.repeat, &s.shuffle, &order, &seed)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		s.updated = time.UnixMilli(upd)
	}
	rows, err := q.QueryContext(ctx, `SELECT track_id FROM queue_items WHERE player_id=? ORDER BY idx`, pid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		s.queue = append(s.queue, id)
	}
	// queue rows may have vanished with deleted tracks
	if s.idx >= len(s.queue) {
		s.idx = len(s.queue) - 1
	}
	if s.idx < 0 && s.state != Idle {
		s.state, s.pos = Idle, 0
	}
	if s.idx >= 0 {
		q.QueryRowContext(ctx, `SELECT COALESCE(duration_ms,0) FROM tracks WHERE id=?`, s.queue[s.idx]).Scan(&s.duration)
	}
	if s.shuffle {
		fmt.Sscan(seed.String, &s.seed)
		if json.Unmarshal([]byte(order.String), &s.order) != nil || !validOrder(s.order, len(s.queue)) {
			s.reshuffle(s.idx) // queue changed underneath (deleted tracks)
		}
	}
	return s, rows.Err()
}

type querier interface {
	QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row
	QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error)
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
}

// position extrapolates the playhead for a playing session.
func (o *Orchestrator) position(s *session) int64 {
	p := s.pos
	if s.state == Playing {
		p += o.now().Sub(s.updated).Milliseconds()
	}
	if s.duration > 0 && p > s.duration {
		p = s.duration
	}
	if p < 0 {
		p = 0
	}
	return p
}

// mutate runs f on the loaded session inside a transaction, persists, and broadcasts.
func (o *Orchestrator) mutate(ctx context.Context, pid string, queueChanged bool, f func(s *session) error) (State, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return State{}, err
	}
	defer tx.Rollback()
	s, err := o.load(ctx, tx, pid)
	if err != nil {
		return State{}, err
	}
	before := len(s.queue)
	beforeOrder := fmt.Sprint(s.shuffle, s.order)
	beforeQ := strings.Join(s.queue, ",")
	known := map[string]bool{}
	for _, id := range s.queue {
		known[id] = true
	}
	if err := f(s); err != nil {
		return State{}, err
	}
	if len(s.queue) > maxQueue {
		return State{}, fmt.Errorf("%w: queue longer than %d", ErrBadRequest, maxQueue)
	}
	queueChanged = queueChanged || before != len(s.queue) || beforeQ != strings.Join(s.queue, ",")
	if queueChanged {
		var fresh []string
		for _, id := range s.queue {
			if !known[id] {
				fresh = append(fresh, id)
			}
		}
		if err := validTracks(ctx, tx, fresh); err != nil {
			return State{}, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM queue_items WHERE player_id=?`, pid); err != nil {
			return State{}, err
		}
		for i, id := range s.queue {
			if _, err := tx.ExecContext(ctx, `INSERT INTO queue_items(player_id, idx, track_id) VALUES (?,?,?)`, pid, i, id); err != nil {
				return State{}, err
			}
		}
	}
	if s.idx >= 0 && s.idx < len(s.queue) {
		tx.QueryRowContext(ctx, `SELECT COALESCE(duration_ms,0) FROM tracks WHERE id=?`, s.queue[s.idx]).Scan(&s.duration)
	}
	s.updated = o.now()
	if s.shuffle && !validOrder(s.order, len(s.queue)) {
		return State{}, fmt.Errorf("internal: shuffle order out of sync")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO sessions(player_id, state, position_ms, updated_at, current_idx, repeat_mode,
		shuffle, shuffle_order, shuffle_seed)
		VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT(player_id) DO UPDATE SET state=excluded.state, position_ms=excluded.position_ms,
		updated_at=excluded.updated_at, current_idx=excluded.current_idx, repeat_mode=excluded.repeat_mode,
		shuffle=excluded.shuffle, shuffle_order=excluded.shuffle_order, shuffle_seed=excluded.shuffle_seed`,
		pid, s.state, s.pos, s.updated.UnixMilli(), s.idx, s.repeat, s.shuffle, encodeOrder(s), s.seed); err != nil {
		return State{}, err
	}
	st, err := o.state(ctx, tx, pid, s)
	if err != nil {
		return State{}, err
	}
	var q Queue
	if queueChanged || orderChanged(beforeOrder, s) {
		queueChanged = true
		if q, err = o.queue(ctx, tx, pid, s); err != nil {
			return State{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return State{}, err
	}
	o.hub.Publish("player.state", st)
	if queueChanged {
		o.hub.Publish("queue.updated", q)
	}
	return st, nil
}

func validTracks(ctx context.Context, q querier, ids []string) error {
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
			return fmt.Errorf("%w: unknown track %q", ErrBadRequest, id)
		}
	}
	return nil
}

func trackRef(ctx context.Context, q querier, id string) (*TrackRef, error) {
	t := &TrackRef{ID: id}
	err := q.QueryRowContext(ctx, `SELECT t.title, COALESCE(t.artist_name,''), COALESCE(t.album_id,''), COALESCE(al.title,''),
		COALESCE(t.duration_ms,0) FROM tracks t LEFT JOIN albums al ON al.id=t.album_id WHERE t.id=?`, id).
		Scan(&t.Title, &t.ArtistName, &t.AlbumID, &t.AlbumTitle, &t.DurationMs)
	return t, err
}

func (o *Orchestrator) state(ctx context.Context, q querier, pid string, s *session) (State, error) {
	st := State{PlayerID: pid, Transport: s.state, PositionMs: o.position(s), UpdatedAt: o.now().UTC(),
		CurrentIndex: s.idx, QueueLength: len(s.queue), Repeat: s.repeat, Shuffle: s.shuffle, NextIndex: -1}
	if ni, _ := s.nextIndex(true); ni >= 0 && s.idx >= 0 {
		t, err := trackRef(ctx, q, s.queue[ni])
		if err != nil {
			return st, err
		}
		st.NextIndex, st.Next = ni, t
		if s.state != Idle && o.lease != nil {
			if l, err := o.lease(ctx, t.ID); err == nil {
				st.NextLease = &l
			}
		}
	}
	if s.idx >= 0 && s.idx < len(s.queue) {
		t, err := trackRef(ctx, q, s.queue[s.idx])
		if err != nil {
			return st, err
		}
		st.Track = t
		if s.state != Idle && o.lease != nil {
			if l, err := o.lease(ctx, t.ID); err == nil {
				st.Lease = &l
			}
		}
	}
	return st, nil
}

func (o *Orchestrator) queue(ctx context.Context, q querier, pid string, s *session) (Queue, error) {
	out := Queue{PlayerID: pid, CurrentIndex: s.idx, Shuffle: s.shuffle, Items: []QueueItem{}, PlayOrder: []int{}}
	for p := range s.queue {
		out.PlayOrder = append(out.PlayOrder, s.at(p))
	}
	for i, id := range s.queue {
		t, err := trackRef(ctx, q, id)
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, QueueItem{Index: i, TrackRef: *t})
	}
	return out, nil
}

// ── reads ───────────────────────────────────────────────────────────────────

func (o *Orchestrator) State(ctx context.Context, pid string) (State, error) {
	s, err := o.load(ctx, o.db, pid)
	if err != nil {
		return State{}, err
	}
	return o.state(ctx, o.db, pid, s)
}

func (o *Orchestrator) Queue(ctx context.Context, pid string) (Queue, error) {
	s, err := o.load(ctx, o.db, pid)
	if err != nil {
		return Queue{}, err
	}
	return o.queue(ctx, o.db, pid, s)
}

// States returns the state of every registered player.
func (o *Orchestrator) States(ctx context.Context) ([]State, error) {
	rows, err := o.db.QueryContext(ctx, `SELECT id FROM players ORDER BY created_at`)
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
	out := []State{}
	for _, id := range ids {
		st, err := o.State(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, nil
}

// ── transport ──────────────────────────────────────────────────────────────

type PlayRequest struct {
	TrackIDs   []string `json:"track_ids,omitempty"` // replace queue
	Index      *int     `json:"index,omitempty"`     // start / jump to index
	PositionMs int64    `json:"position_ms,omitempty"`
}

func load(s *session, idx int, pos int64) {
	s.idx, s.pos, s.state = idx, pos, Buffering
}

// Play replaces the queue (track_ids), jumps (index) or resumes.
func (o *Orchestrator) Play(ctx context.Context, pid string, r PlayRequest) (State, error) {
	return o.mutate(ctx, pid, len(r.TrackIDs) > 0, func(s *session) error {
		if r.PositionMs < 0 {
			return fmt.Errorf("%w: negative position", ErrBadRequest)
		}
		if len(r.TrackIDs) > 0 {
			s.queue = append([]string(nil), r.TrackIDs...)
			if s.shuffle {
				first := -1
				if r.Index != nil {
					first = *r.Index
				}
				s.reshuffle(first)
			}
		}
		if len(s.queue) == 0 {
			return ErrEmptyQueue
		}
		switch {
		case r.Index != nil:
			if *r.Index < 0 || *r.Index >= len(s.queue) {
				return fmt.Errorf("%w: index out of range", ErrBadRequest)
			}
			load(s, *r.Index, r.PositionMs)
		case len(r.TrackIDs) > 0:
			load(s, s.at(0), r.PositionMs)
		case s.state == Paused && s.idx >= 0:
			s.state = Playing // resume from stored position
		case s.state == Idle:
			idx := s.idx
			if idx < 0 {
				idx = s.at(0)
			}
			load(s, idx, r.PositionMs)
		}
		return nil
	})
}

func (o *Orchestrator) Pause(ctx context.Context, pid string) (State, error) {
	return o.mutate(ctx, pid, false, func(s *session) error {
		if s.state == Playing || s.state == Buffering {
			s.pos, s.state = o.position(s), Paused
		}
		return nil
	})
}

func (o *Orchestrator) Stop(ctx context.Context, pid string) (State, error) {
	return o.mutate(ctx, pid, false, func(s *session) error {
		s.state, s.pos = Idle, 0
		return nil
	})
}

func (o *Orchestrator) Seek(ctx context.Context, pid string, posMs int64) (State, error) {
	return o.mutate(ctx, pid, false, func(s *session) error {
		if s.idx < 0 {
			return ErrEmptyQueue
		}
		if posMs < 0 || (s.duration > 0 && posMs > s.duration) {
			return fmt.Errorf("%w: position out of range", ErrBadRequest)
		}
		s.pos = posMs
		if s.state == Playing {
			s.state = Buffering
		}
		return nil
	})
}

// Next advances; at the end of the queue it wraps (repeat=all) or stops.
func (o *Orchestrator) Next(ctx context.Context, pid string) (State, error) {
	return o.mutate(ctx, pid, false, func(s *session) error { return o.advance(s, false) })
}

func (o *Orchestrator) advance(s *session, ended bool) error {
	if len(s.queue) == 0 {
		return ErrEmptyQueue
	}
	next, wrap := s.nextIndex(ended)
	switch {
	case next < 0:
		s.state, s.pos = Idle, 0
	case wrap && s.shuffle:
		// new pass, new seed; avoid repeating the track that just played
		last := s.idx
		s.reshuffle(-1)
		if len(s.order) > 1 && s.order[0] == last {
			s.order[0], s.order[len(s.order)-1] = s.order[len(s.order)-1], s.order[0]
		}
		load(s, s.order[0], 0)
	default:
		load(s, next, 0)
	}
	return nil
}

// Prev restarts the current track if more than 3s in, else goes back one.
func (o *Orchestrator) Prev(ctx context.Context, pid string) (State, error) {
	return o.mutate(ctx, pid, false, func(s *session) error {
		if len(s.queue) == 0 {
			return ErrEmptyQueue
		}
		idx := s.idx
		if p := s.playPos(); o.position(s) <= 3000 && p > 0 {
			idx = s.at(p - 1)
		}
		load(s, max(idx, 0), 0)
		return nil
	})
}

// SetShuffle toggles shuffle. Enabling (or reshuffle=true while enabled)
// generates a new order with a fresh seed, keeping the current track first;
// disabling returns to natural queue order from the current track.
func (o *Orchestrator) SetShuffle(ctx context.Context, pid string, on, reshuffle bool) (State, error) {
	return o.mutate(ctx, pid, false, func(s *session) error {
		switch {
		case on && (!s.shuffle || reshuffle):
			s.shuffle = true
			s.reshuffle(s.idx)
		case !on:
			s.shuffle, s.order, s.seed = false, nil, 0
		}
		return nil
	})
}

func (o *Orchestrator) SetRepeat(ctx context.Context, pid, mode string) (State, error) {
	return o.mutate(ctx, pid, false, func(s *session) error {
		if mode != "off" && mode != "all" && mode != "one" {
			return fmt.Errorf("%w: repeat must be off|all|one", ErrBadRequest)
		}
		s.repeat = mode
		return nil
	})
}

// ── client reports ─────────────────────────────────────────────────────────

// Progress records the client's playhead; the first report after buffering
// moves the transport to playing. Only transitions are broadcast.
func (o *Orchestrator) Progress(ctx context.Context, pid string, posMs int64) error {
	o.mu.Lock()
	s, err := o.load(ctx, o.db, pid)
	o.mu.Unlock()
	if err != nil {
		return err
	}
	if s.state == Buffering {
		_, err := o.mutate(ctx, pid, false, func(s *session) error {
			if s.state == Buffering {
				s.state, s.pos = Playing, max(posMs, 0)
			}
			return nil
		})
		return err
	}
	if s.state != Playing {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	_, err = o.db.ExecContext(ctx, `UPDATE sessions SET position_ms=?, updated_at=? WHERE player_id=? AND state='playing'`,
		max(posMs, 0), o.now().UnixMilli(), pid)
	return err
}

// Ended is reported when the current track finished naturally. trackID
// guards against stale reports for a track that is no longer current.
func (o *Orchestrator) Ended(ctx context.Context, pid, trackID string) (State, error) {
	return o.mutate(ctx, pid, false, func(s *session) error {
		if s.idx < 0 || (trackID != "" && s.queue[s.idx] != trackID) || s.state == Idle {
			return nil
		}
		return o.advance(s, true)
	})
}

// ── queue ops ──────────────────────────────────────────────────────────────

type QueueOp struct {
	Op       string   `json:"op"` // replace | append | insert | remove | move | clear
	TrackIDs []string `json:"track_ids,omitempty"`
	Index    int      `json:"index,omitempty"` // insert position / remove index
	From     int      `json:"from,omitempty"`
	To       int      `json:"to,omitempty"`
}

func (o *Orchestrator) EditQueue(ctx context.Context, pid string, op QueueOp) (State, error) {
	return o.mutate(ctx, pid, true, func(s *session) error {
		n := len(s.queue)
		switch op.Op {
		case "replace":
			s.queue = append([]string(nil), op.TrackIDs...)
			s.idx, s.pos = -1, 0
			if s.shuffle {
				s.reshuffle(-1)
			}
			if len(s.queue) > 0 {
				s.idx = s.at(0)
				if s.state == Playing || s.state == Buffering {
					s.state = Buffering
				} else {
					s.state = Idle
				}
			} else {
				s.state = Idle
			}
		case "append":
			if len(op.TrackIDs) == 0 {
				return fmt.Errorf("%w: track_ids required", ErrBadRequest)
			}
			s.queue = append(s.queue, op.TrackIDs...)
			s.remap(identity, span(n, len(op.TrackIDs)), false)
			if s.idx < 0 {
				s.idx = s.at(0)
			}
		case "insert":
			if op.Index < 0 || op.Index > n || len(op.TrackIDs) == 0 {
				return fmt.Errorf("%w: bad insert", ErrBadRequest)
			}
			q := append([]string{}, s.queue[:op.Index]...)
			q = append(q, op.TrackIDs...)
			s.queue = append(q, s.queue[op.Index:]...)
			k := len(op.TrackIDs)
			s.remap(func(i int) int {
				if i >= op.Index {
					return i + k
				}
				return i
			}, span(op.Index, k), true)
			if s.idx < 0 {
				s.idx = s.at(0)
			} else if op.Index <= s.idx {
				s.idx += len(op.TrackIDs)
			}
		case "remove":
			if op.Index < 0 || op.Index >= n {
				return fmt.Errorf("%w: index out of range", ErrBadRequest)
			}
			s.queue = append(s.queue[:op.Index:op.Index], s.queue[op.Index+1:]...)
			// with shuffle, removing the current track continues at its successor in play order
			succ := -1
			if s.shuffle && op.Index == s.idx {
				if p := s.playPos(); p+1 < n {
					succ = s.order[p+1]
					if succ > op.Index {
						succ--
					}
				}
			}
			s.remap(func(i int) int {
				switch {
				case i == op.Index:
					return -1
				case i > op.Index:
					return i - 1
				}
				return i
			}, nil, false)
			if s.shuffle && op.Index == s.idx {
				if succ < 0 {
					s.idx, s.state, s.pos = -1, Idle, 0
					if len(s.queue) > 0 {
						s.idx = s.order[len(s.order)-1]
					}
				} else if s.state == Playing || s.state == Buffering {
					load(s, succ, 0)
				} else {
					s.idx, s.pos = succ, 0
				}
				break
			}
			switch {
			case op.Index < s.idx:
				s.idx--
			case op.Index == s.idx:
				if s.idx >= len(s.queue) {
					s.idx, s.state, s.pos = len(s.queue)-1, Idle, 0
				} else if s.state == Playing || s.state == Buffering {
					load(s, s.idx, 0)
				} else {
					s.pos = 0
				}
				if len(s.queue) == 0 {
					s.idx, s.state = -1, Idle
				}
			}
		case "move":
			if op.From < 0 || op.From >= n || op.To < 0 || op.To >= n {
				return fmt.Errorf("%w: index out of range", ErrBadRequest)
			}
			id := s.queue[op.From]
			q := append(append([]string{}, s.queue[:op.From]...), s.queue[op.From+1:]...)
			q = append(q[:op.To], append([]string{id}, q[op.To:]...)...)
			s.queue = q
			s.remap(func(i int) int {
				switch {
				case i == op.From:
					return op.To
				case op.From < i && i <= op.To:
					return i - 1
				case op.To <= i && i < op.From:
					return i + 1
				}
				return i
			}, nil, false)
			switch {
			case s.idx == op.From:
				s.idx = op.To
			case op.From < s.idx && op.To >= s.idx:
				s.idx--
			case op.From > s.idx && op.To <= s.idx:
				s.idx++
			}
		case "clear":
			s.queue, s.idx, s.state, s.pos = nil, -1, Idle, 0
			if s.shuffle {
				s.order = []int{}
			}
		default:
			return fmt.Errorf("%w: unknown op %q", ErrBadRequest, op.Op)
		}
		return nil
	})
}

func identity(i int) int { return i }

func span(from, k int) []int {
	out := make([]int, k)
	for i := range out {
		out[i] = from + i
	}
	return out
}

func orderChanged(before string, s *session) bool { return before != fmt.Sprint(s.shuffle, s.order) }
