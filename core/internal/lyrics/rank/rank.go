// Package rank scores lyrics candidates against what the client is playing.
// One scorer is shared by the public lyrics API (/api/v1/lyrics/*) and the
// web lyrics workbench so both show the same order and numbers.
package rank

import (
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/my-name-is-alan/yimeng/core/internal/lyrics/providers"
	"github.com/my-name-is-alan/yimeng/core/internal/online/anon"
)

// Query is what the client knows about the song. Any song works; it does
// not need to be in the library.
type Query struct {
	Title      string `json:"title"`
	Artist     string `json:"artist,omitempty"`
	Album      string `json:"album,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`
	ISRC       string `json:"isrc,omitempty"`
}

// Reliability (0..1) per source: how often its timing/text is right.
var Reliability = map[string]float64{
	"user": 1, "local": 1, "sidecar": 1, "embedded": 0.95,
	"lrclib": 1, "netease": 0.9, "qq": 0.9, "kugou": 0.7,
}

var artistSplit = regexp.MustCompile(`\s*(?:/|,|，|、|&|＆|;|；| x | X | feat\.? | ft\.? | with )\s*`)

func splitArtists(s string) []string {
	var out []string
	for _, a := range artistSplit.Split(s, -1) {
		if a = strings.TrimSpace(a); a != "" {
			out = append(out, a)
		}
	}
	return out
}

// artistSim is the best pairwise similarity between the artists on each side.
func artistSim(q, c string) float64 {
	best := anon.Sim(q, c)
	for _, a := range splitArtists(q) {
		for _, b := range splitArtists(c) {
			best = math.Max(best, anon.Sim(a, b))
		}
	}
	return best
}

func round1(x float64) float64 { return math.Round(x*10) / 10 }

// Score returns 0..100 and the breakdown for one candidate.
func Score(q Query, c providers.Candidate) (int, providers.ScoreBreakdown) {
	b := providers.ScoreBreakdown{DurationDeltaMs: -1, Penalty: 1}
	st := anon.Sim(q.Title, c.Title)
	b.Title = 40 * st
	if strings.TrimSpace(q.Artist) == "" || strings.TrimSpace(c.Artist) == "" {
		b.Artist = 15
	} else {
		b.Artist = 30 * artistSim(q.Artist, c.Artist)
	}
	if strings.TrimSpace(q.Album) == "" || strings.TrimSpace(c.Album) == "" {
		b.Album = 2.5
	} else {
		b.Album = 5 * anon.Sim(q.Album, c.Album)
	}
	if q.DurationMs > 0 && c.DurationMs > 0 {
		d := q.DurationMs - c.DurationMs
		if d < 0 {
			d = -d
		}
		b.DurationDeltaMs = d
		switch {
		case d <= 2000:
			b.Duration = 15
		case d <= 5000:
			b.Duration = 10
		case d <= 10000:
			b.Duration = 5
		}
	} else {
		b.Duration = 7.5
	}
	switch {
	case c.Synced && strings.TrimSpace(c.SyncedLyrics) != "":
		b.Synced = 5
	case c.Synced:
		b.Synced = 4
	case strings.TrimSpace(c.PlainLyrics) != "":
		b.Synced = 1
	}
	if strings.TrimSpace(c.Translation) != "" {
		b.Translation = 2
	}
	rel, ok := Reliability[strings.ToLower(c.Provider)]
	if !ok {
		rel = 0.6
	}
	b.Reliability = 3 * rel
	if st == 0 {
		b.Penalty = 0.3
	}
	sum := b.Title + b.Artist + b.Album + b.Duration + b.Synced + b.Translation + b.Reliability
	total := int(math.Round(sum * b.Penalty))
	total = max(0, min(100, total))
	b.Title, b.Artist, b.Album, b.Duration, b.Reliability = round1(b.Title), round1(b.Artist), round1(b.Album), round1(b.Duration), round1(b.Reliability)
	return total, b
}

// Rank scores every candidate in place and sorts best first (ties: synced,
// then more reliable source, then provider name for a stable order).
func Rank(q Query, cs []providers.Candidate) []providers.Candidate {
	for i := range cs {
		s, b := Score(q, cs[i])
		bb := b
		cs[i].Score, cs[i].Breakdown = s, &bb
	}
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if pa, pb := ownPriority(a.Provider), ownPriority(b.Provider); pa != pb {
			return pa > pb // lyrics the user bound / already on disk win ties
		}
		if a.Synced != b.Synced {
			return a.Synced
		}
		if a.Breakdown.Reliability != b.Breakdown.Reliability {
			return a.Breakdown.Reliability > b.Breakdown.Reliability
		}
		return a.Provider < b.Provider
	})
	return cs
}

func ownPriority(p string) int {
	switch p {
	case "user":
		return 2
	case "local", "sidecar", "embedded":
		return 1
	}
	return 0
}

// CapPerProvider keeps at most n candidates per provider (input must be
// ranked); LRCLIB in particular returns many near-identical uploads.
func CapPerProvider(cs []providers.Candidate, n int) []providers.Candidate {
	seen := map[string]int{}
	out := cs[:0]
	for _, c := range cs {
		if seen[c.Provider] < n {
			seen[c.Provider]++
			out = append(out, c)
		}
	}
	return out
}
