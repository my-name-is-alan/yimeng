// Command netease-smoke is an optional, manually-run live check of the
// NetEase plugin against the real service. It does nothing unless
// YIMENG_LIVE_SMOKE=1 is set. An optional session can be supplied with
// YIMENG_NETEASE_COOKIE (never printed). It only reads public data and the
// account's own entitlement; it never downloads audio.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/online"
	"github.com/my-name-is-alan/yimeng/core/internal/online/netease"
)

func main() {
	if os.Getenv("YIMENG_LIVE_SMOKE") != "1" {
		fmt.Println("skipped: set YIMENG_LIVE_SMOKE=1 to call the live NetEase API")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c := online.NewClient("netease", 30, nil)
	pl, err := netease.New(netease.Options{})(online.Env{HTTP: c, Config: online.Config{}})
	if err != nil {
		fail("init", err)
	}
	p := pl.(*netease.Plugin)
	if ck := os.Getenv("YIMENG_NETEASE_COOKIE"); ck != "" {
		acc, err := p.LoginCookie(ctx, ck)
		if err != nil {
			fail("cookie login", err)
		}
		fmt.Printf("account: %s vip=%v\n", acc.Nickname, acc.VIP)
	}
	q := "晴天"
	if len(os.Args) > 1 {
		q = os.Args[1]
	}
	r, err := p.Search(ctx, q, 5)
	if err != nil {
		fail("search", err)
	}
	fmt.Printf("search %q: %d tracks %d albums %d artists %d playlists\n", q, len(r.Tracks), len(r.Albums), len(r.Artists), len(r.Playlists))
	for _, t := range r.Tracks {
		s, err := p.Resolve(ctx, nativeID(t.ID), online.QHigh)
		state := string(s.Entitlement)
		if err != nil {
			state = "error: " + err.Error()
		} else if s.Entitlement == online.EntPreview {
			state += fmt.Sprintf(" [%d–%d ms]", s.PreviewStartMs, s.PreviewEndMs)
		}
		fmt.Printf("  %-30s %-12s listing=%s resolve=%s\n", t.Title, t.ArtistName, t.Playable, state)
	}
	if ch, err := p.Charts(ctx); err != nil {
		fail("charts", err)
	} else {
		fmt.Printf("charts: %d\n", len(ch))
	}
	if l, err := p.Lyrics(ctx, nativeID(r.Tracks[0].ID)); err == nil {
		fmt.Printf("lyrics: synced=%v %d bytes\n", l.Synced, len(l.LRC))
	}
	fmt.Println("ok")
}

func nativeID(id string) string {
	for i := range id {
		if id[i] == ':' {
			return id[i+1:]
		}
	}
	return id
}

func fail(what string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", what, err)
	os.Exit(1)
}
