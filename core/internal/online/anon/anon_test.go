package anon

import (
	"context"
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/metadata"
	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

type fakeCat struct{ lyricsCalls int }

func (f *fakeCat) Search(ctx context.Context, q string, limit int) (online.SearchResult, error) {
	return online.SearchResult{
		Tracks: []online.Track{
			{ID: "qq:x1", Title: "晴天 (Live)", ArtistName: "周杰伦", DurationMs: 299000, Album: &online.Ref{Name: "无与伦比演唱会"}},
			{ID: "qq:x2", Title: "晴天", ArtistName: "周杰伦", DurationMs: 269000, ISRC: "TWK970300003", Year: 2003, Album: &online.Ref{Name: "葉惠美"}, CoverURL: "https://img/x2.jpg"},
			{ID: "qq:x3", Title: "七里香", ArtistName: "周杰伦", DurationMs: 299000},
		},
		Albums: []online.Album{{ID: "qq:album-1", Title: "葉惠美", ArtistName: "周杰伦", Year: 2003, CoverURL: "https://img/a.jpg"}},
	}, nil
}
func (f *fakeCat) Tracks(context.Context, []string) ([]online.Track, error) { return nil, nil }
func (f *fakeCat) Album(context.Context, string) (online.Album, error)      { return online.Album{}, nil }
func (f *fakeCat) Artist(context.Context, string) (online.Artist, error)    { return online.Artist{}, nil }
func (f *fakeCat) Playlist(context.Context, string) (online.Playlist, error) {
	return online.Playlist{}, nil
}
func (f *fakeCat) Lyrics(_ context.Context, id string) (online.Lyrics, error) {
	f.lyricsCalls++
	if id == "x3" {
		return online.Lyrics{}, nil
	}
	return online.Lyrics{LRC: "[00:01.50]故事的小黄花\n[00:05.00]从出生那年就飘着", Translation: ""}, nil
}

func TestAnonMatchAndLyrics(t *testing.T) {
	s := NewCatalog(metadata.QQMusic, "QQ音乐", &fakeCat{}, nil)
	ms, err := s.LookupTrack(context.Background(), metadata.TrackQuery{Title: "晴天", Artist: "周杰伦", Album: "叶惠美", DurationMs: 269500})
	if err != nil || len(ms) == 0 {
		t.Fatal(err)
	}
	if ms[0].ExternalID != "qq:x2" || ms[0].Score < 0.95 || ms[0].ISRC == "" || ms[0].Provider != metadata.QQMusic {
		t.Fatalf("best %+v", ms[0])
	}
	as, err := s.LookupAlbum(context.Background(), metadata.AlbumQuery{Title: "叶惠美", AlbumArtist: "周杰伦", Year: 2003})
	if err != nil || as[0].Score < 0.95 {
		t.Fatalf("%v %+v", err, as)
	}
	l, err := s.Lyrics(context.Background(), metadata.TrackQuery{Title: "晴天", Artist: "周杰伦"})
	if err != nil || len(l.Synced) != 2 || l.Synced[0].Text != "故事的小黄花" {
		t.Fatalf("%v %+v", err, l)
	}
	cs, err := s.SearchLyrics(context.Background(), "", "晴天", "周杰伦", 5)
	if err != nil || len(cs) != 2 || !cs[0].Synced || cs[0].Provider != "qq" {
		t.Fatalf("%v %+v", err, cs)
	}
	if Sim("叶惠美", "葉惠美") < 0.9 || Sim("晴天", "七里香") != 0 {
		t.Fatal("sim")
	}
}
