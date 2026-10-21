package playback

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/events"
	"github.com/my-name-is-alan/yimeng/core/internal/infra/db"
	"github.com/my-name-is-alan/yimeng/core/migrations"
)

type fixture struct {
	o   *Orchestrator
	hub *events.Hub
	now time.Time
}

func newFixture(t *testing.T) *fixture {
	ctx := context.Background()
	d, err := db.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := db.Migrate(ctx, d, migrations.FS); err != nil {
		t.Fatal(err)
	}
	d.Exec(`INSERT INTO players(id, name, created_at) VALUES ('p1','Mac',1),('p2','Phone',2)`)
	for i := 1; i <= 4; i++ {
		d.Exec(`INSERT INTO tracks(id, title, duration_ms, created_at, updated_at) VALUES (?,?,?,1,1)`,
			fmt.Sprintf("t%d", i), fmt.Sprintf("Song %d", i), 200000)
	}
	f := &fixture{hub: events.NewHub(), now: time.Unix(1000, 0)}
	f.o, err = NewOrchestrator(ctx, d, f.hub, func(_ context.Context, id string) (Lease, error) {
		return Lease{Delivery: DeliveryDirect, URL: "/api/stream/" + id, ExpiresAt: f.now.Add(time.Hour)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	f.o.now = func() time.Time { return f.now }
	return f
}

func mustFn(t *testing.T) func(State, error) State {
	return func(st State, err error) State {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
}

func idx(i int) *int { return &i }

func TestTransportStateMachine(t *testing.T) {
	f := newFixture(t)
	must := mustFn(t)
	ctx := context.Background()
	ch, cancel := f.hub.Subscribe()
	defer cancel()

	st := must(f.o.Play(ctx, "p1", PlayRequest{TrackIDs: []string{"t1", "t2", "t3"}}))
	if st.Transport != Buffering || st.CurrentIndex != 0 || st.Track.ID != "t1" || st.Lease == nil || st.Lease.URL != "/api/stream/t1" {
		t.Fatalf("%+v", st)
	}
	if e := <-ch; e.Type != "player.state" {
		t.Fatal(e.Type)
	}
	if e := <-ch; e.Type != "queue.updated" {
		t.Fatal(e.Type)
	}
	// client reports progress -> playing
	if err := f.o.Progress(ctx, "p1", 0); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(10 * time.Second)
	st, _ = f.o.State(ctx, "p1")
	if st.Transport != Playing || st.PositionMs != 10000 {
		t.Fatalf("extrapolation %+v", st)
	}
	st = must(f.o.Pause(ctx, "p1"))
	f.now = f.now.Add(time.Minute)
	if st, _ = f.o.State(ctx, "p1"); st.Transport != Paused || st.PositionMs != 10000 {
		t.Fatalf("paused %+v", st)
	}
	st = must(f.o.Play(ctx, "p1", PlayRequest{}))
	if st.Transport != Playing || st.PositionMs != 10000 {
		t.Fatalf("resume %+v", st)
	}
	st = must(f.o.Seek(ctx, "p1", 50000))
	if st.Transport != Buffering || st.PositionMs != 50000 {
		t.Fatalf("seek %+v", st)
	}
	if _, err := f.o.Seek(ctx, "p1", 999999); !errors.Is(err, ErrBadRequest) {
		t.Fatal("seek past end")
	}
	// prev while >3s in restarts the track
	st = must(f.o.Prev(ctx, "p1"))
	if st.CurrentIndex != 0 || st.PositionMs != 0 {
		t.Fatalf("prev restart %+v", st)
	}
	st = must(f.o.Next(ctx, "p1"))
	st = must(f.o.Next(ctx, "p1"))
	if st.CurrentIndex != 2 {
		t.Fatal(st.CurrentIndex)
	}
	st = must(f.o.Prev(ctx, "p1"))
	if st.CurrentIndex != 1 {
		t.Fatal("prev")
	}
	// ended: stale report ignored, then advance; end of queue -> idle
	st = must(f.o.Ended(ctx, "p1", "t1"))
	if st.CurrentIndex != 1 {
		t.Fatal("stale ended advanced")
	}
	must(f.o.Ended(ctx, "p1", "t2"))
	st = must(f.o.Ended(ctx, "p1", "t3"))
	if st.Transport != Idle || st.Lease != nil {
		t.Fatalf("end of queue %+v", st)
	}
	// repeat all wraps, repeat one repeats
	must(f.o.SetRepeat(ctx, "p1", "all"))
	must(f.o.Play(ctx, "p1", PlayRequest{Index: idx(2)}))
	if st = must(f.o.Ended(ctx, "p1", "t3")); st.CurrentIndex != 0 {
		t.Fatal("repeat all")
	}
	must(f.o.SetRepeat(ctx, "p1", "one"))
	if st = must(f.o.Ended(ctx, "p1", "")); st.CurrentIndex != 0 || st.Transport != Buffering {
		t.Fatal("repeat one")
	}
	if _, err := f.o.SetRepeat(ctx, "p1", "shuffle"); !errors.Is(err, ErrBadRequest) {
		t.Fatal("repeat validation")
	}
	st = must(f.o.Stop(ctx, "p1"))
	if st.Transport != Idle {
		t.Fatal("stop")
	}
	// players are independent
	if st, _ := f.o.State(ctx, "p2"); st.Transport != Idle || st.QueueLength != 0 {
		t.Fatal("p2 affected")
	}
	if _, err := f.o.Play(ctx, "nope", PlayRequest{}); !errors.Is(err, ErrPlayerNotFound) {
		t.Fatal("unknown player")
	}
	if _, err := f.o.Play(ctx, "p2", PlayRequest{}); !errors.Is(err, ErrEmptyQueue) {
		t.Fatal("empty queue")
	}
	if _, err := f.o.Play(ctx, "p2", PlayRequest{TrackIDs: []string{"ghost"}}); !errors.Is(err, ErrBadRequest) {
		t.Fatal("unknown track")
	}
}

func queueIDs(t *testing.T, f *fixture) ([]string, int) {
	q, err := f.o.Queue(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, it := range q.Items {
		ids = append(ids, it.ID)
	}
	return ids, q.CurrentIndex
}

func TestQueueOps(t *testing.T) {
	f := newFixture(t)
	must := mustFn(t)
	ctx := context.Background()
	must(f.o.Play(ctx, "p1", PlayRequest{TrackIDs: []string{"t1", "t2", "t3"}, Index: idx(1)}))
	steps := []struct {
		op      QueueOp
		want    string
		wantIdx int
	}{
		{QueueOp{Op: "append", TrackIDs: []string{"t4"}}, "[t1 t2 t3 t4]", 1},
		{QueueOp{Op: "insert", Index: 0, TrackIDs: []string{"t4"}}, "[t4 t1 t2 t3 t4]", 2},
		{QueueOp{Op: "remove", Index: 0}, "[t1 t2 t3 t4]", 1},
		{QueueOp{Op: "move", From: 1, To: 3}, "[t1 t3 t4 t2]", 3}, // current follows its track
		{QueueOp{Op: "move", From: 0, To: 3}, "[t3 t4 t2 t1]", 2},
		{QueueOp{Op: "remove", Index: 2}, "[t3 t4 t1]", 2}, // removing current -> next one
		{QueueOp{Op: "clear"}, "[]", -1},
	}
	for _, s := range steps {
		if _, err := f.o.EditQueue(ctx, "p1", s.op); err != nil {
			t.Fatalf("%+v: %v", s.op, err)
		}
		ids, ci := queueIDs(t, f)
		if fmt.Sprint(ids) != s.want || ci != s.wantIdx {
			t.Fatalf("after %+v: %v idx %d, want %s idx %d", s.op, ids, ci, s.want, s.wantIdx)
		}
	}
	for _, bad := range []QueueOp{{Op: "remove", Index: 5}, {Op: "move", From: 0, To: 9}, {Op: "explode"}, {Op: "append"}} {
		if _, err := f.o.EditQueue(ctx, "p1", bad); !errors.Is(err, ErrBadRequest) {
			t.Errorf("%+v accepted", bad)
		}
	}
}

func TestPersistAndRestart(t *testing.T) {
	f := newFixture(t)
	must := mustFn(t)
	ctx := context.Background()
	must(f.o.Play(ctx, "p1", PlayRequest{TrackIDs: []string{"t1", "t2"}, Index: idx(1), PositionMs: 5000}))
	f.o.Progress(ctx, "p1", 5000)
	o2, err := NewOrchestrator(ctx, f.o.db, events.NewHub(), nil)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := o2.State(ctx, "p1")
	if st.Transport != Paused || st.CurrentIndex != 1 || st.QueueLength != 2 || st.PositionMs != 5000 {
		t.Fatalf("after restart %+v", st)
	}
	// deleted tracks shrink the queue safely
	f.o.db.Exec(`DELETE FROM tracks WHERE id='t2'`)
	st, err = o2.State(ctx, "p1")
	if err != nil || st.QueueLength != 1 || st.CurrentIndex != 0 {
		t.Fatalf("%+v %v", st, err)
	}
}

func queueOf(t *testing.T, f *fixture, pid string) Queue {
	t.Helper()
	q, err := f.o.Queue(context.Background(), pid)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func isPerm(order []int, n int) bool { return validOrder(order, n) }

func TestShuffle(t *testing.T) {
	f := newFixture(t)
	must := mustFn(t)
	ctx := context.Background()
	must(f.o.Play(ctx, "p1", PlayRequest{TrackIDs: []string{"t1", "t2", "t3", "t4"}, Index: idx(1)}))

	ch, cancel := f.hub.Subscribe()
	defer cancel()
	st := must(f.o.SetShuffle(ctx, "p1", true, false))
	if !st.Shuffle || st.CurrentIndex != 1 || st.Track.ID != "t2" {
		t.Fatalf("%+v", st)
	}
	if e := <-ch; e.Type != "player.state" {
		t.Fatal(e.Type)
	}
	if e := <-ch; e.Type != "queue.updated" || !e.Data.(Queue).Shuffle {
		t.Fatalf("%+v", e)
	}
	q := queueOf(t, f, "p1")
	if q.PlayOrder[0] != 1 || !isPerm(q.PlayOrder, 4) || q.Items[0].ID != "t1" || q.Items[3].ID != "t4" {
		t.Fatalf("queue must be unchanged, current first in order: %+v", q)
	}
	order := q.PlayOrder
	if st.NextIndex != order[1] || st.Next == nil || st.NextLease == nil {
		t.Fatalf("next hint %+v order %v", st, order)
	}

	// persisted across a fresh orchestrator load
	o2, _ := NewOrchestrator(ctx, f.o.db, f.hub, nil)
	q2, _ := o2.Queue(ctx, "p1")
	if fmt.Sprint(q2.PlayOrder) != fmt.Sprint(order) {
		t.Fatalf("not persisted %v vs %v", q2.PlayOrder, order)
	}

	// walk the order: visits every track once, then stops (repeat off)
	seen := []int{1}
	for i := 0; i < 3; i++ {
		st = must(f.o.Next(ctx, "p1"))
		seen = append(seen, st.CurrentIndex)
	}
	if fmt.Sprint(seen) != fmt.Sprint(order) {
		t.Fatalf("walked %v want %v", seen, order)
	}
	if st.NextIndex != -1 {
		t.Fatal("last track must have no next with repeat off")
	}
	// prev goes back in play order
	if st = must(f.o.Prev(ctx, "p1")); st.CurrentIndex != order[2] {
		t.Fatalf("prev %d want %d", st.CurrentIndex, order[2])
	}
	must(f.o.Next(ctx, "p1"))

	// repeat one: ended replays; next hint = same
	must(f.o.SetRepeat(ctx, "p1", "one"))
	st = must(f.o.Ended(ctx, "p1", ""))
	if st.CurrentIndex != order[3] || st.NextIndex != order[3] {
		t.Fatalf("repeat one %+v", st)
	}
	// repeat all: wrap reshuffles with a new seed and does not repeat the last track
	must(f.o.SetRepeat(ctx, "p1", "all"))
	if st.NextIndex = must(f.o.State(ctx, "p1")).NextIndex; st.NextIndex != order[0] {
		t.Fatalf("wrap hint %d", st.NextIndex)
	}
	st = must(f.o.Next(ctx, "p1"))
	q = queueOf(t, f, "p1")
	if st.CurrentIndex == order[3] || q.PlayOrder[0] != st.CurrentIndex || !isPerm(q.PlayOrder, 4) {
		t.Fatalf("wrap: %+v %v", st, q.PlayOrder)
	}

	// disable: queue order resumes at current track
	cur := st.CurrentIndex
	st = must(f.o.SetShuffle(ctx, "p1", false, false))
	if st.Shuffle || st.CurrentIndex != cur {
		t.Fatalf("%+v", st)
	}
	if q = queueOf(t, f, "p1"); fmt.Sprint(q.PlayOrder) != "[0 1 2 3]" {
		t.Fatal(q.PlayOrder)
	}
	if cur < 3 && st.NextIndex != cur+1 {
		t.Fatalf("natural next %d", st.NextIndex)
	}
}

func TestShuffleQueueEdits(t *testing.T) {
	f := newFixture(t)
	must := mustFn(t)
	ctx := context.Background()
	must(f.o.Play(ctx, "p1", PlayRequest{TrackIDs: []string{"t1", "t2", "t3"}}))
	must(f.o.SetShuffle(ctx, "p1", true, false))
	ids := func() []string {
		q := queueOf(t, f, "p1")
		var out []string
		for _, i := range q.PlayOrder {
			out = append(out, q.Items[i].ID)
		}
		if !isPerm(q.PlayOrder, len(q.Items)) {
			t.Fatalf("bad order %v", q.PlayOrder)
		}
		return out
	}
	before := ids()
	if before[0] != "t1" {
		t.Fatal(before)
	}

	// insert = play next in shuffled order
	must(f.o.EditQueue(ctx, "p1", QueueOp{Op: "insert", Index: 0, TrackIDs: []string{"t4"}}))
	got := ids()
	if fmt.Sprint(got) != fmt.Sprint(append([]string{"t1", "t4"}, before[1:]...)) {
		t.Fatalf("insert %v (before %v)", got, before)
	}
	st := must(f.o.State(ctx, "p1"))
	if st.Track.ID != "t1" || st.Next.ID != "t4" {
		t.Fatalf("%+v", st)
	}

	// append lands somewhere upcoming, relative order of the rest kept
	must(f.o.EditQueue(ctx, "p1", QueueOp{Op: "append", TrackIDs: []string{"t2"}}))
	got = ids()
	if len(got) != 5 || got[0] != "t1" {
		t.Fatal(got)
	}

	// move keeps play order by identity
	beforeMove := ids()
	must(f.o.EditQueue(ctx, "p1", QueueOp{Op: "move", From: 0, To: 3}))
	if fmt.Sprint(ids()) != fmt.Sprint(beforeMove) {
		t.Fatalf("move changed play order %v vs %v", ids(), beforeMove)
	}

	// removing the current track continues at its play-order successor
	succ := beforeMove[1]
	cur := must(f.o.State(ctx, "p1")).CurrentIndex
	st = must(f.o.EditQueue(ctx, "p1", QueueOp{Op: "remove", Index: cur}))
	if st.Track.ID != succ || len(ids()) != 4 {
		t.Fatalf("remove current: %+v %v", st.Track, ids())
	}

	// replace reshuffles; clear empties
	st = must(f.o.EditQueue(ctx, "p1", QueueOp{Op: "replace", TrackIDs: []string{"t1", "t2", "t3", "t4"}}))
	if q := queueOf(t, f, "p1"); q.PlayOrder[0] != st.CurrentIndex || !isPerm(q.PlayOrder, 4) {
		t.Fatalf("%v %d", q.PlayOrder, st.CurrentIndex)
	}
	must(f.o.EditQueue(ctx, "p1", QueueOp{Op: "clear"}))
	if q := queueOf(t, f, "p1"); len(q.PlayOrder) != 0 || !q.Shuffle {
		t.Fatal(q)
	}
}

func TestShuffleShuffledHelper(t *testing.T) {
	a, b := shuffled(50, 7, 1), shuffled(50, 7, 2)
	if a[0] != 7 || b[0] != 7 || !validOrder(a, 50) || fmt.Sprint(a) == fmt.Sprint(b) {
		t.Fatal("seeded shuffle")
	}
	if fmt.Sprint(shuffled(50, 7, 1)) != fmt.Sprint(a) {
		t.Fatal("not deterministic per seed")
	}
}

func TestOnListened(t *testing.T) {
	f := newFixture(t)
	must := mustFn(t)
	ctx := context.Background()
	var got []Listen
	f.o.OnListened = func(l Listen) { got = append(got, l) }
	must(f.o.Play(ctx, "p1", PlayRequest{TrackIDs: []string{"t1", "t2", "t3"}}))
	f.o.Progress(ctx, "p1", 0)
	f.now = f.now.Add(100 * time.Second)
	must(f.o.Next(ctx, "p1")) // skip after 100s
	f.o.Progress(ctx, "p1", 0)
	must(f.o.Ended(ctx, "p1", "t2"))
	must(f.o.Seek(ctx, "p1", 0)) // seeking is not leaving
	must(f.o.Stop(ctx, "p1"))
	if len(got) != 2 || got[0].TrackID != "t1" || got[0].PlayedMs != 100000 || got[0].Completed ||
		got[1].TrackID != "t2" || !got[1].Completed || got[1].PlayedMs != 200000 {
		t.Fatalf("%+v", got)
	}
}
