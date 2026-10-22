package online

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCacheCoalescing(t *testing.T) {
	_, svc, _, _, _, _ := setup(t)
	c := svc.Cache
	ctx := context.Background()
	var calls atomic.Int32
	gate := make(chan struct{})
	fetch := func(ctx context.Context) (int, error) {
		calls.Add(1)
		<-gate
		return 42, nil
	}
	var wg sync.WaitGroup
	res := make([]int, 20)
	for i := range res {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v, err := cached(ctx, c, Key("fa", "album", "x"), time.Hour, fetch)
			if err != nil {
				t.Error(err)
			}
			res[i] = v
		}(i)
	}
	time.Sleep(50 * time.Millisecond)
	close(gate)
	wg.Wait()
	if n := calls.Load(); n != 1 {
		t.Fatalf("upstream calls = %d, want 1", n)
	}
	for _, v := range res {
		if v != 42 {
			t.Fatalf("got %v", res)
		}
	}
	if st, _ := c.Stats(ctx); st.Counters["coalesced"]+st.Counters["hits"] != 19 || st.Entries != 1 {
		t.Fatalf("stats %+v", st)
	}
}

func TestCacheStaleWhileRevalidate(t *testing.T) {
	_, svc, _, _, _, _ := setup(t)
	c := svc.Cache
	now := time.Unix(10000, 0)
	var mu sync.Mutex
	c.now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	ctx := context.Background()
	key := Key("fa", "charts", "all")
	n := 0
	fetch := func(ctx context.Context) (int, error) { n++; return n, nil }
	if v, _ := cached(ctx, c, key, time.Hour, fetch); v != 1 {
		t.Fatal(v)
	}
	if v, _ := cached(ctx, c, key, time.Hour, fetch); v != 1 || n != 1 {
		t.Fatal("fresh hit refetched")
	}
	mu.Lock()
	now = now.Add(2 * time.Hour) // past TTL, inside stale window
	mu.Unlock()
	if v, _ := cached(ctx, c, key, time.Hour, fetch); v != 1 {
		t.Fatalf("stale copy not served: %d", v)
	}
	c.Wait()
	if v, _ := cached(ctx, c, key, time.Hour, fetch); v != 2 {
		t.Fatalf("not revalidated: %d", v)
	}
	// stale + upstream failing → keep serving stale
	mu.Lock()
	now = now.Add(2 * time.Hour)
	mu.Unlock()
	bad := func(ctx context.Context) (int, error) { return 0, ErrRiskControl }
	if v, err := cached(ctx, c, key, time.Hour, bad); err != nil || v != 2 {
		t.Fatalf("stale-on-error %d %v", v, err)
	}
	c.Wait()
	if v, _ := cached(ctx, c, key, time.Hour, bad); v != 2 {
		t.Fatal("stale copy lost after failed refresh")
	}
	// cooling: no upstream call at all
	c.Cooling = func(string) bool { return true }
	called := false
	cached(ctx, c, key, time.Hour, func(ctx context.Context) (int, error) { called = true; return 9, nil })
	c.Wait()
	if called {
		t.Fatal("refreshed during cooldown")
	}
}

func TestCacheNegative(t *testing.T) {
	_, svc, _, _, _, _ := setup(t)
	c := svc.Cache
	ctx := context.Background()
	key := Key("fa", "album", "gone")
	calls := 0
	fetch := func(ctx context.Context) (Album, error) { calls++; return Album{}, ErrUnavailable }
	for i := 0; i < 3; i++ {
		if _, err := cached(ctx, c, key, time.Hour, fetch); !errors.Is(err, ErrUnavailable) {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("negative not cached: %d calls", calls)
	}
	// transient errors are not remembered
	k2 := Key("fa", "album", "flaky")
	calls = 0
	flaky := func(ctx context.Context) (Album, error) { calls++; return Album{}, ErrRiskControl }
	cached(ctx, c, k2, time.Hour, flaky)
	cached(ctx, c, k2, time.Hour, flaky)
	if calls != 2 {
		t.Fatalf("transient error cached")
	}
}

func TestCacheEvictionAndClear(t *testing.T) {
	_, svc, _, _, _, _ := setup(t)
	c := svc.Cache
	ctx := context.Background()
	c.MaxBytes = 4000
	big := make([]byte, 500)
	for i := range big {
		big[i] = 'a'
	}
	now := time.Unix(1000, 0)
	c.now = func() time.Time { return now }
	for i := 0; i < 20; i++ {
		now = now.Add(time.Second)
		c.Put(ctx, Key("fa", "track", string(rune('a'+i))), string(big), 0)
	}
	st, _ := c.Stats(ctx)
	if st.Bytes > c.MaxBytes || st.Entries == 0 {
		t.Fatalf("cap not enforced %+v", st)
	}
	var s string
	if c.Get(ctx, Key("fa", "track", "a"), &s) {
		t.Fatal("oldest entry survived eviction")
	}
	if !c.Get(ctx, Key("fa", "track", string(rune('a'+19))), &s) {
		t.Fatal("newest entry evicted")
	}
	c.Put(ctx, Key("fb", "charts", "all"), 1, 0)
	if n, _ := c.Clear(ctx, "fa", ""); n == 0 {
		t.Fatal("clear origin")
	}
	if st, _ := c.Stats(ctx); st.Entries != 1 || st.Kinds[0].Origin != "fb" {
		t.Fatalf("after clear %+v", st)
	}
}
