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
