package library

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestRootSnapshotsDoNotAlias(t *testing.T) {
	root := t.TempDir()
	input := []string{root}
	s := NewScanner(nil, Options{Roots: input})
	input[0] = "modified input"
	got := s.Roots()
	got[0] = "modified output"
	if s.Roots()[0] != root {
		t.Fatal("root configuration escaped through an alias")
	}
	if !s.underRoot(filepath.Join(root, "song.mp3")) || s.underRoot(root+"-sibling") {
		t.Fatal("root containment boundary")
	}
	volumeRoot := filepath.VolumeName(root) + string(filepath.Separator)
	if rootOf([]string{volumeRoot}, filepath.Join(root, "song.mp3")) != volumeRoot {
		t.Fatal("filesystem root must contain descendants")
	}
}

func TestLoadRootsWithoutArtistSplitSetting(t *testing.T) {
	s, root, _ := setup(t)
	next := filepath.Join(root, "new")
	if err := s.SetLibraryRoots(context.Background(), []string{next, next + string(filepath.Separator)}); err != nil {
		t.Fatal(err)
	}
	restarted := NewScanner(s.db, Options{Roots: []string{root}})
	if err := restarted.LoadSettings(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := restarted.Roots(); !reflect.DeepEqual(got, []string{next}) {
		t.Fatalf("roots were not restored/normalized: %v", got)
	}
	if err := restarted.SetLibraryRoots(context.Background(), []string{"relative"}); err == nil {
		t.Fatal("relative root accepted")
	}
}

func TestFailedRootUpdateKeepsSnapshot(t *testing.T) {
	s, root, _ := setup(t)
	if _, err := s.db.Exec(`CREATE TRIGGER fail_root_update BEFORE INSERT ON settings
		WHEN NEW.key='library.roots' BEGIN SELECT RAISE(ABORT, 'settings write failed'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLibraryRoots(context.Background(), []string{filepath.Join(root, "new")}); err == nil {
		t.Fatal("expected persistence failure")
	}
	if got := s.Roots(); !reflect.DeepEqual(got, []string{root}) {
		t.Fatalf("failed write changed runtime configuration: %v", got)
	}
}

func TestConcurrentRootUpdatesAndReaders(t *testing.T) {
	s, root, _ := setup(t)
	ctx := context.Background()
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	var wg sync.WaitGroup
	for _, target := range []string{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 30; i++ {
				if err := s.SetLibraryRoots(ctx, []string{target}); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				got := s.Roots()
				if len(got) != 1 || (got[0] != root && got[0] != a && got[0] != b) {
					t.Errorf("torn root snapshot: %v", got)
					return
				}
				s.underRoot(filepath.Join(a, "song.mp3"))
				s.rootOf(filepath.Join(b, "song.mp3"))
			}
		}()
	}
	wg.Wait()
	var raw string
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key='library.roots'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var persisted []string
	if err := json.Unmarshal([]byte(raw), &persisted); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.Roots(), persisted) {
		t.Fatalf("runtime/database diverged: runtime=%v persisted=%v", s.Roots(), persisted)
	}
}
