package runtimecfg

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestStorePutValidateApply(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	db.Exec(`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT)`)
	st := &Store{DB: db}
	ctx := context.Background()
	if _, err := st.Load(ctx, Settings{WatchMode: "auto", RescanInterval: "1h"}); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	st.OnChange(func(old, cur Settings) {
		calls.Add(1)
		if old.WatchMode != "auto" || cur.WatchMode != "poll" {
			t.Errorf("apply %v -> %v", old, cur)
		}
	})
	for _, bad := range []string{`{"watch_mode":"sometimes"}`, `{"rescan_interval":"5s"}`, `{"musicbrainz_enabled":true}`,
		`{"fill_sources":["spotify","bogus"]}`, `{"fill_per_minute":1000}`, `{"musicbrainz_contact":"not a contact"}`} {
		if _, err := st.Put(ctx, []byte(bad)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: %v", bad, err)
		}
	}
	got, err := st.Put(ctx, []byte(`{"watch_mode":"poll","rescan_interval":"30m","musicbrainz_enabled":true,"musicbrainz_contact":"me@example.org","fill_sources":["QQ","spotify","qq"],"fill_per_minute":10}`))
	if err != nil || got.Rescan() != 30*time.Minute || len(got.FillSources) != 2 || got.FillSources[0] != "qq" || calls.Load() != 1 {
		t.Fatalf("%v %+v %d", err, got, calls.Load())
	}
	// persisted: a new store loads it over the defaults
	st2 := &Store{DB: db}
	got2, _ := st2.Load(ctx, Settings{WatchMode: "auto"})
	if got2.WatchMode != "poll" || got2.MusicBrainzContact != "me@example.org" || got2.FillPerMinute != 10 {
		t.Fatalf("%+v", got2)
	}
}

func TestSupervisorRestart(t *testing.T) {
	var s Supervisor
	var running atomic.Int32
	run := func(ctx context.Context) { running.Add(1); <-ctx.Done(); running.Add(-1) }
	parent, cancel := context.WithCancel(context.Background())
	s.Restart(parent, run)
	s.Restart(parent, run)
	time.Sleep(20 * time.Millisecond)
	if running.Load() != 1 {
		t.Fatalf("running %d", running.Load())
	}
	cancel()
	s.Wait()
	if running.Load() != 0 {
		t.Fatal("not stopped")
	}
}
