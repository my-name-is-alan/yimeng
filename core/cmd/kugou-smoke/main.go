// Command kugou-smoke is an optional, manually-run live check of the Kugou
// plugin. It does nothing unless YIMENG_LIVE_SMOKE=1 is set. An optional
// session can be supplied with YIMENG_KUGOU_COOKIE ("token=…; userid=…",
// never printed). It reads public data and the account's own entitlement
// only; it never downloads audio.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/online"
	"github.com/my-name-is-alan/yimeng/core/internal/online/kugou"
)

func main() {
	if os.Getenv("YIMENG_LIVE_SMOKE") != "1" {
		fmt.Println("skipped: set YIMENG_LIVE_SMOKE=1 to call the live Kugou API")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c := online.NewClient("kugou", 60, nil)
	pl, err := kugou.New(kugou.Options{})(online.Env{HTTP: c, Config: online.Config{}})
	if err != nil {
		fail("init", err)
	}
	p := pl.(*kugou.Plugin)
	if ck := os.Getenv("YIMENG_KUGOU_COOKIE"); ck != "" {
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
	fmt.Printf("device registered: %v\n", c.Cookie(online.DevicePrefix+"dfid") != "")
	fmt.Printf("search %q: %d tracks %d albums %d artists %d playlists\n", q, len(r.Tracks), len(r.Albums), len(r.Artists), len(r.Playlists))
	for _, t := range r.Tracks {
		s, err := p.Resolve(ctx, native(t.ID), online.QHigh)
		state := string(s.Entitlement)
		if err != nil {
			state = "error: " + err.Error()
		} else if s.Entitlement == online.EntPreview {
			state += fmt.Sprintf(" [%d–%d ms]", s.PreviewStartMs, s.PreviewEndMs)
		} else if s.Entitlement == online.EntFull {
			state += fmt.Sprintf(" %s %dk", s.Format, s.BitrateKbps)
		}
		fmt.Printf("  %-24s %-10s vip=%-5v listing=%s resolve=%s\n", t.Title, t.ArtistName, t.VIP, t.Playable, state)
	}
	if len(r.Tracks) > 0 {
		if ts, err := p.Tracks(ctx, []string{native(r.Tracks[0].ID)}); err == nil && len(ts) == 1 {
			fmt.Printf("detail: %s / %s / %dms\n", ts[0].Title, ts[0].ArtistName, ts[0].DurationMs)
		} else {
			fmt.Printf("detail: %v\n", err)
		}
		if l, err := p.Lyrics(ctx, native(r.Tracks[0].ID)); err == nil {
			fmt.Printf("lyrics: synced=%v %d bytes\n", l.Synced, len(l.LRC))
		} else {
			fmt.Printf("lyrics: %v\n", err)
		}
	}
	if len(r.Albums) > 0 {
		a, err := p.Album(ctx, native(r.Albums[0].ID))
		fmt.Printf("album %q: %d tracks %v\n", a.Title, len(a.Tracks), errStr(err))
	}
	if len(r.Artists) > 0 {
		a, err := p.Artist(ctx, native(r.Artists[0].ID))
		fmt.Printf("artist %q: %d top tracks %v\n", a.Name, len(a.TopTracks), errStr(err))
	}
	ch, err := p.Charts(ctx)
	fmt.Printf("charts: %d %v\n", len(ch), errStr(err))
	if len(ch) > 0 {
		c0, err := p.Playlist(ctx, native(ch[0].ID))
		fmt.Printf("chart %q: %d tracks %v\n", c0.Name, len(c0.Tracks), errStr(err))
	}
	rt, err := p.RecommendTracks(ctx)
	fmt.Printf("everyday: %d %v\n", len(rt), errStr(err))
	rp, err := p.RecommendPlaylists(ctx)
	fmt.Printf("recommended playlists: %d %v\n", len(rp), errStr(err))
	if len(rp) > 0 {
		p0, err := p.Playlist(ctx, native(rp[0].ID))
		fmt.Printf("playlist %q: %d tracks %v\n", p0.Name, len(p0.Tracks), errStr(err))
	}
	if _, err := p.StartQR(ctx); err != nil {
		fmt.Printf("qr: %v\n", err)
	} else {
		fmt.Println("qr: challenge issued")
	}
	fmt.Println("ok")
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return "error: " + err.Error()
}

func native(id string) string { _, n, _ := strings.Cut(id, ":"); return n }

func fail(what string, err error) {
	fmt.Fprintf(os.Stderr, "%s: %v\n", what, err)
	os.Exit(1)
}
