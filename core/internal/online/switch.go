package online

import (
	"context"
	"sort"
	"strings"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

// SwitchPrefs is a listener's cross-platform source switching preference
// (D50): when a track is preview-only/unavailable on its platform, play the
// same recording from another platform the server is logged into with full
// entitlement. Only legitimate, entitled sources are ever used.
type SwitchPrefs struct {
	Enabled  bool     `json:"enabled"`
	Priority []string `json:"priority"` // platform ids, most preferred first
}

// DefaultSwitch is on, registration order.
var DefaultSwitch = SwitchPrefs{Enabled: true, Priority: []string{}}

type switchKey struct{}

// WithSwitch attaches the caller's switching preference to ctx.
func WithSwitch(ctx context.Context, p SwitchPrefs) context.Context {
	return context.WithValue(ctx, switchKey{}, p)
}

func switchFrom(ctx context.Context) SwitchPrefs {
	if p, ok := ctx.Value(switchKey{}).(SwitchPrefs); ok {
		return p
	}
	return DefaultSwitch
}

func (p SwitchPrefs) key() string {
	if !p.Enabled {
		return "off"
	}
	return strings.Join(p.Priority, ",")
}

// rank is the position of origin in the priority (unlisted last).
func (p SwitchPrefs) rank(origin string) int {
	for i, id := range p.Priority {
		if id == origin {
			return i
		}
	}
	return 1 << 20
}

// ordered sorts platform ids by the preference (unlisted keep their order).
func (p SwitchPrefs) ordered(ids []string) []string {
	out := append([]string(nil), ids...)
	sort.SliceStable(out, func(i, j int) bool { return p.rank(out[i]) < p.rank(out[j]) })
	return out
}

// switchMinScore: title+artist match, duration within ~3 s (0.9) or ISRC (1).
const switchMinScore = 0.9

// Alternative is the same recording on another platform.
type Alternative struct {
	TrackID    string      `json:"track_id"`
	Origin     string      `json:"origin"`
	Title      string      `json:"title"`
	ArtistName string      `json:"artist_name"`
	DurationMs int64       `json:"duration_ms"`
	Playable   Entitlement `json:"playable,omitempty"`
	Score      float64     `json:"score"`
	Method     string      `json:"method"` // isrc | meta
}

// Alternatives finds the same song on the other enabled, logged-in
// platforms (ordered by the caller's priority, fully entitled first).
func (s *Service) Alternatives(ctx context.Context, id string) ([]Alternative, error) {
	if s.Isolated {
		return []Alternative{}, nil
	}
	o, _, err := split(id)
	if err != nil {
		return nil, err
	}
	t, err := s.Track(ctx, id)
	if err != nil {
		return nil, err
	}
	p := switchFrom(ctx)
	out := []Alternative{}
	for _, other := range p.ordered(s.Reg.EnabledIDs()) {
		if other == o || !s.Reg.LoggedIn(other) {
			continue
		}
		// title + artist first; platforms spell artists differently (e.g.
		// "SNH48周诗雨"), so also try the title alone.
		var cands []Track
		for _, q := range []string{strings.TrimSpace(t.Title + " " + t.ArtistName), t.Title} {
			res := s.Search(ctx, q, 10, []string{other})
			if len(res) > 0 && res[0].Result != nil {
				cands = append(cands, res[0].Result.Tracks...)
			}
		}
		var mine []Alternative
		dup := map[string]bool{}
		for _, c := range cands {
			if dup[c.ID] {
				continue
			}
			dup[c.ID] = true
			if c.Playable == EntUnavailable {
				continue
			}
			sc, method := library.MatchScore(library.MatchQuery{ISRC: t.ISRC, Title: t.Title, Artist: t.ArtistName, DurationMs: t.DurationMs},
				c.ISRC, c.Title, c.ArtistName, c.DurationMs)
			if sc < switchMinScore {
				continue
			}
			mine = append(mine, Alternative{TrackID: c.ID, Origin: other, Title: c.Title, ArtistName: c.ArtistName,
				DurationMs: c.DurationMs, Playable: c.Playable, Score: sc, Method: method})
		}
		sort.SliceStable(mine, func(i, j int) bool {
			if (mine[i].Playable == EntFull) != (mine[j].Playable == EntFull) {
				return mine[i].Playable == EntFull
			}
			return mine[i].Score > mine[j].Score
		})
		if len(mine) > 3 {
			mine = mine[:3]
		}
		out = append(out, mine...)
	}
	return out, nil
}
