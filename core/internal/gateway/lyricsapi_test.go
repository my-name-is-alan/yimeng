package gateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/lyrics/providers"
)

func TestPublicLyricsAPI(t *testing.T) {
	lyricsAPI = &lyricsAPIState{cache: map[string]lyricsCacheEntry{}, rl: NewRateLimits(0.5, 10, 0)}
	calls := 0
	e := newEnvWith(t, func(d *Deps, _ *sql.DB, _ *library.Scanner) {
		d.LyricsSearch = func(_ context.Context, q, title, artist string) ([]providers.Candidate, error) {
			calls++
			if title == "无此歌" {
				return nil, nil
			}
			return []providers.Candidate{
				{Ref: "qq:1", Provider: "qq", Title: "晴天 (Live)", Artist: "周杰伦", DurationMs: 320000, Synced: true, SyncedLyrics: "[00:01.00]a"},
				{Ref: "netease:2", Provider: "netease", Title: "晴天", Artist: "周杰伦", DurationMs: 269000, Synced: true,
					SyncedLyrics: "[00:01.00]故事的小黄花\n[00:03.00]从出生那年就飘着", Translation: "[00:01.00]Little yellow flower"},
				{ID: 9, Provider: "lrclib", Title: "Other", Artist: "X", PlainLyrics: "p"},
			}, nil
		}
	})
	if res, _ := e.do("GET", "/api/v1/lyrics/search?title=晴天", "", ""); res.StatusCode != 401 {
		t.Fatal("must require a token", res.StatusCode)
	}
	if res, _ := e.do("GET", "/api/v1/lyrics/search", "", e.token); res.StatusCode != 400 {
		t.Fatal("title required", res.StatusCode)
	}
	res, body := e.do("GET", "/api/v1/lyrics/search?title=晴天&artist=周杰伦&duration=269", "", e.token)
	var s struct {
		Found bool
		Items []struct {
			ID, Provider   string
			Score          int
			HasTranslation bool                      `json:"has_translation"`
			Breakdown      *providers.ScoreBreakdown `json:"score_breakdown"`
			Preview        []string
		}
	}
	json.Unmarshal(body, &s)
	if res.StatusCode != 200 || !s.Found || len(s.Items) != 3 || s.Items[0].ID != "netease:2" || !s.Items[0].HasTranslation ||
		s.Items[0].Breakdown == nil || len(s.Items[0].Preview) != 2 || s.Items[0].Score < s.Items[1].Score || s.Items[1].Score < s.Items[2].Score {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	// best: cached (no second upstream call), lines carry translations
	res, body = e.do("GET", "/api/v1/lyrics/best?title=晴天&artist=周杰伦&duration=269", "", e.token)
	var b struct {
		Found    bool
		Provider string
		Lines    []LyricsBestLine
	}
	json.Unmarshal(body, &b)
	if res.StatusCode != 200 || !b.Found || b.Provider != "netease" || len(b.Lines) != 2 || b.Lines[0].Trans != "Little yellow flower" || res.Header.Get("X-Yimeng-Cache") != "hit" || calls != 1 {
		t.Fatalf("%d calls=%d %s", res.StatusCode, calls, body)
	}
	// none: 200 with a message for the app to show
	res, body = e.do("GET", "/api/v1/lyrics/best?title=无此歌", "", e.token)
	var n struct {
		Found   bool
		Message string
	}
	json.Unmarshal(body, &n)
	if res.StatusCode != 200 || n.Found || n.Message != "暂无歌词" {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	// rate limit on cache misses
	limited := false
	for i := 0; i < 15; i++ {
		if r, _ := e.do("GET", "/api/v1/lyrics/search?title=t"+string(rune('a'+i)), "", e.token); r.StatusCode == 429 {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("expected 429 after burst")
	}
}
