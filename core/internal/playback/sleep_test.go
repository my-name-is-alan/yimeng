package playback

import (
	"context"
	"testing"
	"time"
)

func TestSleepTimerFadeAndPause(t *testing.T) {
	f := newFixture(t)
	must := mustFn(t)
	ctx := context.Background()
	must(f.o.Play(ctx, "p1", PlayRequest{TrackIDs: []string{"t1", "t2"}}))
	f.o.Progress(ctx, "p1", 0)
	st := must(f.o.SetSleep(ctx, "p1", SleepOpts{Minutes: 10}))
	if st.Sleep == nil || st.Sleep.Until == nil || st.Sleep.FadeMs != 30000 || st.SleepUntil == nil {
		t.Fatalf("armed %+v", st.Sleep)
	}
	ch, cancel := f.hub.Subscribe()
	defer cancel()
	f.now = f.now.Add(9*time.Minute + 40*time.Second) // 20 s left: fade starts
	f.o.sleepTick(ctx)
	if e := <-ch; e.Type != "player.sleep.fade" {
		t.Fatalf("want fade, got %s", e.Type)
	}
	f.o.sleepTick(ctx) // fade is announced once
	f.now = f.now.Add(25 * time.Second)
	f.o.sleepTick(ctx)
	got := map[string]bool{}
	for len(ch) > 0 {
		got[(<-ch).Type] = true
	}
	if got["player.sleep.fade"] || !got["player.sleep"] {
		t.Fatalf("events %v", got)
	}
	if st, _ := f.o.State(ctx, "p1"); st.Transport != Paused || st.Sleep != nil {
		t.Fatalf("after timer %+v", st)
	}
	// clear
	must(f.o.SetSleep(ctx, "p1", SleepOpts{Minutes: 5}))
	if st := must(f.o.SetSleep(ctx, "p1", SleepOpts{})); st.Sleep != nil {
		t.Fatal("not cleared")
	}
}

func TestSleepEndOfTrack(t *testing.T) {
	f := newFixture(t)
	must := mustFn(t)
	ctx := context.Background()
	must(f.o.Play(ctx, "p1", PlayRequest{TrackIDs: []string{"t1", "t2"}}))
	f.o.Progress(ctx, "p1", 0)
	zero := 0
	st := must(f.o.SetSleep(ctx, "p1", SleepOpts{EndOfTrack: true, FadeSec: &zero}))
	if st.Sleep == nil || !st.Sleep.EndOfTrack || st.Sleep.Until != nil {
		t.Fatalf("armed %+v", st.Sleep)
	}
	st = must(f.o.Ended(ctx, "p1", "t1"))
	if st.Transport != Paused || st.Track == nil || st.Track.ID != "t2" || st.PositionMs != 0 || st.Sleep != nil {
		t.Fatalf("after end of track %+v", st)
	}
}

func TestSleepEndOfTrackFade(t *testing.T) {
	f := newFixture(t)
	must := mustFn(t)
	ctx := context.Background()
	must(f.o.Play(ctx, "p1", PlayRequest{TrackIDs: []string{"t1"}}))
	f.o.Progress(ctx, "p1", 0)
	must(f.o.SetSleep(ctx, "p1", SleepOpts{EndOfTrack: true}))
	ch, cancel := f.hub.Subscribe()
	defer cancel()
	f.now = f.now.Add(100 * time.Second) // 100 s of 200 s left: no fade yet
	f.o.sleepTick(ctx)
	if len(ch) != 0 {
		t.Fatal("early fade")
	}
	f.now = f.now.Add(75 * time.Second) // 25 s left
	f.o.sleepTick(ctx)
	if e := <-ch; e.Type != "player.sleep.fade" {
		t.Fatalf("want fade, got %s", e.Type)
	}
}
