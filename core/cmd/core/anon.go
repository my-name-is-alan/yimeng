package main

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/lyrics/providers"
	"github.com/my-name-is-alan/yimeng/core/internal/metadata"
	"github.com/my-name-is-alan/yimeng/core/internal/online"
	"github.com/my-name-is-alan/yimeng/core/internal/online/anon"
	"github.com/my-name-is-alan/yimeng/core/internal/online/kugou"
	"github.com/my-name-is-alan/yimeng/core/internal/online/netease"
	"github.com/my-name-is-alan/yimeng/core/internal/online/qq"
)

// anonSources builds the no-login metadata/lyrics sources for 网易云, QQ音乐
// and 酷狗 (independent of the streaming plugins and their accounts).
func anonSources(log *slog.Logger) map[string]*anon.Source {
	hc := &http.Client{Timeout: 12 * time.Second}
	out := map[string]*anon.Source{}
	for _, x := range []struct {
		id metadata.ProviderID
		f  online.Factory
	}{{metadata.NetEase, netease.New(netease.Options{})}, {metadata.QQMusic, qq.New(qq.Options{})}, {metadata.Kugou, kugou.New(kugou.Options{})}} {
		s, err := anon.New(x.id, x.f, 60, hc)
		if err != nil {
			log.Warn("anonymous source unavailable", "id", x.id, "err", err)
			continue
		}
		out[string(x.id)] = s
	}
	return out
}

// anonLyrics adapts an anonymous source to the background lyrics chain
// (local tracks only; used only when the track has no lyrics).
type anonLyrics struct{ s *anon.Source }

func (a anonLyrics) ID() string { return string(a.s.ID()) }

func (a anonLyrics) Fetch(ctx context.Context, t library.Track) (string, error) {
	cands, err := a.s.SearchLyrics(ctx, "", t.Title, t.ArtistName, 3)
	if err != nil {
		return "", err
	}
	for _, c := range cands {
		if c.Score < 0.85 || (t.DurationMs > 0 && c.DurationMs > 0 && absMs(t.DurationMs-c.DurationMs) > 5000) {
			continue
		}
		if c.LRC != "" {
			return c.LRC, nil
		}
		if c.Plain != "" {
			return c.Plain, nil
		}
	}
	return "", nil
}

func absMs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

// lyricsSearch merges LRCLIB with the anonymous platform sources for the
// lyrics workbench; each source is bounded so one slow platform can't stall it.
func lyricsSearch(lrc *providers.LRCLIB, srcs map[string]*anon.Source) func(ctx context.Context, q, title, artist string) ([]providers.Candidate, error) {
	return func(ctx context.Context, q, title, artist string) ([]providers.Candidate, error) {
		var mu sync.Mutex
		var all []providers.Candidate
		var firstErr error
		ok := 0
		var wg sync.WaitGroup
		run := func(f func(context.Context) ([]providers.Candidate, error)) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				cctx, cancel := context.WithTimeout(ctx, 12*time.Second)
				defer cancel()
				cs, err := f(cctx)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					if firstErr == nil {
						firstErr = err
					}
					return
				}
				ok++
				all = append(all, cs...)
			}()
		}
		run(func(c context.Context) ([]providers.Candidate, error) {
			cs, err := lrc.Search(c, q, title, artist)
			for i := range cs {
				if cs[i].Provider == "" {
					cs[i].Provider = "lrclib"
				}
			}
			return cs, err
		})
		ids := make([]string, 0, len(srcs))
		for id := range srcs {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			s := srcs[id]
			run(func(c context.Context) ([]providers.Candidate, error) {
				hits, err := s.SearchLyrics(c, q, title, artist, 4)
				out := make([]providers.Candidate, 0, len(hits))
				for _, h := range hits {
					out = append(out, providers.Candidate{Ref: h.Ref, Provider: h.Provider, Title: h.Title, Artist: h.Artist, Album: h.Album,
						DurationMs: h.DurationMs, Synced: h.Synced, SyncedLyrics: h.LRC, PlainLyrics: h.Plain, Translation: h.Translation})
				}
				return out, err
			})
		}
		wg.Wait()
		if ok == 0 && firstErr != nil {
			return nil, firstErr
		}
		if all == nil {
			all = []providers.Candidate{}
		}
		return all, nil
	}
}
