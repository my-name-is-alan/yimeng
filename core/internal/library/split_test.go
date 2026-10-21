package library

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSplitterConfig(t *testing.T) {
	sp := NewSplitter(SplitConfig{
		Exceptions:      []string{"Simon & Garfunkel", "Earth, Wind & Fire", "AC/DC"},
		SplitAmpersand:  true,
		SplitComma:      true,
		ExtraSeparators: []string{" x "},
	})
	cases := map[string][]ArtistCredit{
		"A & B":                        {{"A", "primary"}, {"B", "primary"}},
		"A, B":                         {{"A", "primary"}, {"B", "primary"}},
		"A x B":                        {{"A", "primary"}, {"B", "primary"}},
		"simon & garfunkel":            {{"simon & garfunkel", "primary"}},
		"Earth, Wind & Fire & Chicago": {{"Earth, Wind & Fire", "primary"}, {"Chicago", "primary"}},
		"AC/DC、周杰伦 feat. A & B":        {{"AC/DC", "primary"}, {"周杰伦", "primary"}, {"A", "feat"}, {"B", "feat"}},
		"Max":                          {{"Max", "primary"}}, // " x " needs spaces
	}
	for in, want := range cases {
		if got := sp.Split(in); !reflect.DeepEqual(got, want) {
			t.Errorf("Split(%q) = %v, want %v", in, got, want)
		}
	}
	bad := SplitConfig{ExtraSeparators: []string{"waytoolongseparator"}}
	if err := bad.Validate(); err == nil {
		t.Fatal("want validation error")
	}
}

func TestSetSplitConfigPersistsAndRemerges(t *testing.T) {
	s, root, run := setup(t)
	ctx := context.Background()
	copyFixture(t, "notags.mp3", filepath.Join(root, "x.mp3"))
	run("")
	// Retag via DB to simulate "A & B" artist and re-merge.
	s.db.Exec(`UPDATE source_tracks SET artist='Alpha & Beta'`)
	s.Merge(ctx)
	if n := artistCount(t, s); n != 1 {
		t.Fatalf("default should not split &: %d", n)
	}
	if _, err := s.SetSplitConfig(ctx, SplitConfig{SplitAmpersand: true}); err != nil {
		t.Fatal(err)
	}
	if n := artistCount(t, s); n != 2 {
		t.Fatalf("after enabling &: %d", n)
	}
	// persisted: a fresh scanner loads it
	s2 := NewScanner(s.db, s.opts)
	if err := s2.LoadSettings(ctx); err != nil || !s2.SplitConfig().SplitAmpersand {
		t.Fatalf("not persisted: %+v %v", s2.SplitConfig(), err)
	}
}

func artistCount(t *testing.T, s *Scanner) int {
	var n int
	s.db.QueryRow(`SELECT count(*) FROM artists`).Scan(&n)
	return n
}

func TestCompilationGrouping(t *testing.T) {
	s, root, run := setup(t)
	dir := filepath.Join(root, "Hits 2024")
	copyFixture(t, "tagged.mp3", filepath.Join(dir, "1.mp3"))
	copyFixture(t, "tagged.flac", filepath.Join(dir, "2.flac"))
	copyFixture(t, "tagged.m4a", filepath.Join(root, "Other", "3.m4a"))
	run("")
	// Simulate a compilation: no album artist, different track artists, same album.
	s.db.Exec(`UPDATE source_tracks SET album_artist=NULL, album='Hits', title=path`)
	s.db.Exec(`UPDATE source_tracks SET artist='Singer A' WHERE path LIKE '%1.mp3'`)
	s.db.Exec(`UPDATE source_tracks SET artist='Singer B' WHERE path LIKE '%2.flac'`)
	s.db.Exec(`UPDATE source_tracks SET artist='Singer C' WHERE path LIKE '%3.m4a'`)
	if err := s.Merge(context.Background()); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.db.Query(`SELECT artist_name, track_count FROM albums ORDER BY artist_name`)
	defer rows.Close()
	var got []string
	for rows.Next() {
		var a string
		var n int
		rows.Scan(&a, &n)
		got = append(got, a+":"+string(rune('0'+n)))
	}
	if !reflect.DeepEqual(got, []string{"Singer C:1", "Various Artists:2"}) {
		t.Fatalf("albums %v", got)
	}
	_ = os.Remove
}
