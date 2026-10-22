package rank

import (
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/lyrics/providers"
)

func TestRankPrefersExactSyncedCloseDuration(t *testing.T) {
	q := Query{Title: "晴天", Artist: "周杰伦", Album: "叶惠美", DurationMs: 269000}
	cs := []providers.Candidate{
		{Provider: "kugou", Title: "晴天 (Live)", Artist: "周杰伦", DurationMs: 300000, Synced: true, SyncedLyrics: "[00:01.00]a"},
		{Provider: "netease", Title: "晴天", Artist: "周杰伦", Album: "叶惠美", DurationMs: 269500, Synced: true, SyncedLyrics: "[00:01.00]a", Translation: "[00:01.00]b"},
		{Provider: "lrclib", Title: "Sunny Day", Artist: "Someone", DurationMs: 269000, PlainLyrics: "x"},
	}
	r := Rank(q, cs)
	if r[0].Provider != "netease" || r[0].Score < 95 {
		t.Fatalf("best = %s %d", r[0].Provider, r[0].Score)
	}
	if r[2].Provider != "lrclib" || r[2].Score > 30 {
		t.Fatalf("worst = %s %d", r[2].Provider, r[2].Score)
	}
	if r[0].Breakdown == nil || r[0].Breakdown.Translation != 2 || r[0].Breakdown.DurationDeltaMs != 500 {
		t.Fatalf("breakdown %+v", r[0].Breakdown)
	}
}

func TestPinyinAndMultiArtist(t *testing.T) {
	q := Query{Title: "qing tian", Artist: "Jay Chou"}
	s, b := Score(q, providers.Candidate{Provider: "qq", Title: "晴天", Artist: "周杰伦", Synced: true})
	if b.Title < 30 || s < 50 {
		t.Fatalf("pinyin title not matched: %d %+v", s, b)
	}
	_, b2 := Score(Query{Title: "x", Artist: "A"}, providers.Candidate{Title: "x", Artist: "B / A"})
	if b2.Artist != 30 {
		t.Fatalf("multi-artist %v", b2.Artist)
	}
}
