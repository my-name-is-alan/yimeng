//go:build live

package netease

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

func TestLiveHotEndpoints(t *testing.T) {
	c := online.NewClient("netease", 6000, nil)
	p, err := New(Options{})(online.Env{HTTP: c})
	if err != nil {
		t.Fatal(err)
	}
	pl := p.(*Plugin)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	candidates := []struct {
		kind string
		uri  string
		data map[string]any
	}{
		{"weapi", "/api/search/hot", map[string]any{"type": 1111}},
		{"weapi", "/api/search/hot/detail", map[string]any{}},
		{"weapi", "/api/search/hotdetail", map[string]any{}},
		{"eapi", "/api/search/hot", map[string]any{"type": 1111}},
		{"eapi", "/api/search/hot/detail", map[string]any{}},
		{"eapi", "/api/search/hotdetail", map[string]any{}},
		{"weapi", "/api/search/suggest/keyword", map[string]any{}},
		{"eapi", "/api/search/defaultkeyword/get", map[string]any{}},
	}
	for _, cnd := range candidates {
		var body []byte
		var err error
		if cnd.kind == "weapi" {
			body, err = pl.weapiRaw(ctx, cnd.uri, cnd.data)
		} else {
			body, err = pl.eapiRaw(ctx, cnd.uri, cnd.data)
		}
		snip := string(body)
		if len(snip) > 300 {
			snip = snip[:300] + "..."
		}
		fmt.Printf("%s %s err=%v body=%s\n", cnd.kind, cnd.uri, err, snip)
	}
}

func TestLiveHotSearch(t *testing.T) {
	c := online.NewClient("netease", 6000, nil)
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
