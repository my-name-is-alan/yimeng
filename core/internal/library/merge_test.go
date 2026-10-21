package library

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSplitArtists(t *testing.T) {
	cases := map[string][]ArtistCredit{
		"周杰伦":                {{"周杰伦", "primary"}},
		"周杰伦、费玉清":            {{"周杰伦", "primary"}, {"费玉清", "primary"}},
		"A; B ;C":            {{"A", "primary"}, {"B", "primary"}, {"C", "primary"}},
		"A / B":              {{"A", "primary"}, {"B", "primary"}},
		"A feat. B":          {{"A", "primary"}, {"B", "feat"}},
		"A ft. B、C":          {{"A", "primary"}, {"B", "feat"}, {"C", "feat"}},
		"A (feat. B)":        {{"A", "primary"}, {"B", "feat"}},
		"AC/DC":              {{"AC/DC", "primary"}},
		"Simon & Garfunkel":  {{"Simon & Garfunkel", "primary"}},
		"Earth, Wind & Fire": {{"Earth, Wind & Fire", "primary"}},
		"A；A":                {{"A", "primary"}},
		"":                   nil,
	}
	for in, want := range cases {
		if got := SplitArtists(in); !reflect.DeepEqual(got, want) {
			t.Errorf("SplitArtists(%q) = %v, want %v", in, got, want)
		}
	}
}

func ids(t *testing.T, s *Scanner, q string) []string {
	rows, err := s.db.Query(q)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var x string
		rows.Scan(&x)
		out = append(out, x)
	}
	return out
}

func TestMergeIdempotentAndCatalog(t *testing.T) {
	s, root, run := setup(t)
	ctx := context.Background()
	alb := filepath.Join(root, "VA", "Album")
	copyFixture(t, "tagged.mp3", filepath.Join(alb, "01.mp3"))   // same title/artist/album/duration
	copyFixture(t, "tagged.flac", filepath.Join(alb, "01.flac")) // -> fuzzy-merged with the mp3
	copyFixture(t, "notags.mp3", filepath.Join(root, "x", "loose.mp3"))
	run("")

	var albums, tracks, links int
	s.db.QueryRow(`SELECT count(*) FROM albums`).Scan(&albums)
	s.db.QueryRow(`SELECT count(*) FROM tracks`).Scan(&tracks)
	s.db.QueryRow(`SELECT count(*) FROM track_links`).Scan(&links)
	if albums != 1 || tracks != 2 || links != 3 {
		t.Fatalf("albums=%d tracks=%d links=%d", albums, tracks, links)
	}
	var conf float64
	s.db.QueryRow(`SELECT min(confidence) FROM track_links`).Scan(&conf)
	if conf != ConfFuzzy {
		t.Fatalf("expected a fuzzy link, min conf %v", conf)
	}
	before := ids(t, s, `SELECT id FROM tracks ORDER BY id`)
	beforeAl := ids(t, s, `SELECT id FROM albums`)

	// Idempotent: full rescan + merge keeps IDs.
	j, _ := s.Start(ctx, StartRequest{Full: true})
	s.Wait()
	if j, _ = s.Get(ctx, j.ID); j.Status != "completed" {
		t.Fatal(j)
	}
	if after := ids(t, s, `SELECT id FROM tracks ORDER BY id`); !reflect.DeepEqual(before, after) {
		t.Fatalf("ids changed %v -> %v", before, after)
	}

	al, err := s.GetAlbum(ctx, beforeAl[0])
	if err != nil || al.Title != "译梦测试专辑" || al.ArtistName != "Various Artists" || al.Year != 2024 || al.TrackCount != 1 || !al.HasCover {
		t.Fatalf("%+v %v", al, err)
	}
	arts, _ := s.ListArtists(ctx, ListParams{})
	names := map[string]bool{}
	for _, a := range arts.Items {
		names[a.Name] = true
	}
	if !names["测试艺人"] || !names["Various Artists"] || !names[UnknownArtist] || arts.Total != 3 {
		t.Fatalf("artists %+v", arts)
	}
	tp, err := s.ListTracks(ctx, ListParams{Q: "梦里", Limit: 10})
	if err != nil || tp.Total != 1 || tp.Items[0].Format != "flac" || tp.Items[0].Artists[0].Name != "测试艺人" {
		t.Fatalf("%+v %v", tp, err)
	}
	if _, err := s.ListTracks(ctx, ListParams{Sort: "drop table"}); err != ErrBadSort {
		t.Fatal("sort whitelist")
	}
	pg, _ := s.ListTracks(ctx, ListParams{Limit: 1, Offset: 1, Sort: "title", Desc: true})
	if pg.Total != 2 || len(pg.Items) != 1 {
		t.Fatalf("pagination %+v", pg)
	}
	f, err := s.TrackFile(ctx, tp.Items[0].ID)
	if err != nil || filepath.Ext(f.Path) != ".flac" {
		t.Fatalf("%+v %v", f, err)
	}
	if _, err := s.AlbumCover(ctx, al.ID); err != nil {
		t.Fatal(err)
	}

	// Removal: delete the flac -> track remains via mp3; delete mp3 -> album gone.
	os.Remove(filepath.Join(alb, "01.flac"))
	run("")
	s.db.QueryRow(`SELECT count(*) FROM tracks`).Scan(&tracks)
	if tracks != 2 {
		t.Fatalf("tracks=%d", tracks)
	}
	os.Remove(filepath.Join(alb, "01.mp3"))
	run("")
	s.db.QueryRow(`SELECT count(*) FROM albums`).Scan(&albums)
	arts, _ = s.ListArtists(ctx, ListParams{})
	if albums != 0 || arts.Total != 1 {
		t.Fatalf("albums=%d artists=%+v", albums, arts)
	}
	_ = time.Second
}

func TestSafeFileRejectsOutsideRoots(t *testing.T) {
	s, root, _ := setup(t)
	os.MkdirAll(root, 0o755)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	os.WriteFile(outside, []byte("x"), 0o600)
	if _, err := s.safeFile(outside, true); err != ErrNotFound {
		t.Fatal("outside file allowed")
	}
	link := filepath.Join(root, "link.mp3")
	os.Symlink(outside, link)
	if _, err := s.safeFile(link, false); err != ErrNotFound {
		t.Fatal("symlink escape allowed")
	}
	if _, err := s.safeFile(root+"/../"+filepath.Base(root)+"/../x", false); err != ErrNotFound {
		t.Fatal("traversal")
	}
}
