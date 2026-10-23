//go:build live

package qq

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

func TestLiveQQHot(t *testing.T) {
	c := online.NewClient("qq", 6000, nil)
	p, err := New(Options{})(online.Env{HTTP: c})
	if err != nil {
		t.Fatal(err)
	}
	pl := p.(*Plugin)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := pl.mu(ctx, map[string]mreq{"req_0": {
		"music.musicsearch.HotkeyService", "GetHotkeyForQQMusicMobile",
		map[string]any{"search_id": strconv.FormatInt(pl.now().UnixNano(), 10)},
	}})
	fmt.Printf("err=%v raw=%s\n", err, string(out["req_0"]))
	if len(out["req_0"]) > 800 {
		fmt.Println(string(out["req_0"][:800]))
	}
	var top map[string]json.RawMessage
	json.Unmarshal(out["req_0"], &top)
	fmt.Printf("keys=%v\n", keys(top))
}

func keys(m map[string]json.RawMessage) []string {
	var k []string
	for x := range m {
		k = append(k, x)
	}
	return k
}

func TestLiveHotSearch(t *testing.T) {
	c := online.NewClient("qq", 6000, nil)
	p, err := New(Options{})(online.Env{HTTP: c})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	items, err := p.(*Plugin).HotSearch(ctx)
	if err != nil || len(items) == 0 {
		t.Fatalf("hot err=%v n=%d", err, len(items))
	}
	t.Logf("n=%d first=%+v", len(items), items[0])
}
