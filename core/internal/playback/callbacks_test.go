package playback

import (
	"context"
	"testing"
	"time"
)

func TestOnListenedRunsOutsideLockAfterStateEvents(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	must := mustFn(t)
	must(f.o.Play(ctx, "p1", PlayRequest{TrackIDs: []string{"t1", "t2"}}))
	if err := f.o.Progress(ctx, "p1", 0); err != nil {
		t.Fatal(err)
	}
	f.now = f.now.Add(10 * time.Second)
	ch, cancel := f.hub.Subscribe()
	defer cancel()
	called := false
	f.o.OnListened = func(l Listen) {
		called = true
		if !f.o.mu.TryLock() {
			t.Error("OnListened holds the global mutation mutex")
			return // avoid hanging a regression run on a re-entrant mutation
		}
		f.o.mu.Unlock()
		select {
		case e := <-ch:
			if e.Type != "player.state" || e.Data.(State).Transport != Idle {
				t.Errorf("callback ran before the committed state was published: %+v", e)
			}
		default:
			t.Error("missing committed state event before callback")
		}
		// A callback may coordinate another player without deadlocking.
		if _, err := f.o.SetRepeat(ctx, "p2", "all"); err != nil {
			t.Error(err)
		}
		if l.TrackID != "t1" || l.PlayedMs != 10000 {
			t.Errorf("listen snapshot changed: %+v", l)
		}
	}
	must(f.o.Stop(ctx, "p1"))
	if !called {
		t.Fatal("callback not invoked")
	}
}

func TestFailedMutationDoesNotCallOnListened(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	must := mustFn(t)
	must(f.o.Play(ctx, "p1", PlayRequest{TrackIDs: []string{"t1"}}))
	if err := f.o.Progress(ctx, "p1", 10000); err != nil {
		t.Fatal(err)
	}
	f.o.OnListened = func(Listen) { t.Error("failed mutation emitted a listen") }
	if _, err := f.o.Play(ctx, "p1", PlayRequest{TrackIDs: []string{"missing"}}); err == nil {
		t.Fatal("invalid track should fail")
	}
}
