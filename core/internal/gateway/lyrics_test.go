package gateway

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

func TestLyricsAPI(t *testing.T) {
	e := newEnv(t)
	tr := flacTrack(t, e)
	f, err := e.sc.TrackFile(t.Context(), tr.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res, body := e.do("GET", "/api/tracks/"+tr.ID+"/lyrics", "", e.token); res.StatusCode != 404 || !json.Valid(body) {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	// embedded plain lyrics
	e.sc.DB().Exec(`UPDATE source_tracks SET lyrics='plain line' WHERE id IN (SELECT source_track_id FROM track_links WHERE track_id=?)`, tr.ID)
	var l library.TrackLyrics
	_, body := e.do("GET", "/api/tracks/"+tr.ID+"/lyrics", "", e.token)
	json.Unmarshal(body, &l)
	if l.Source != "embedded" || l.Synced || l.Text != "plain line" {
		t.Fatalf("%s", body)
	}
	// synced sidecar wins, no rescan needed
	lrc := filepath.Join(filepath.Dir(f.Path), "tagged.lrc")
	os.WriteFile(lrc, []byte("[00:01.50]第一句\n[00:03.00]second"), 0o644)
	_, body = e.do("GET", "/api/tracks/"+tr.ID+"/lyrics", "", e.token)
	l = library.TrackLyrics{}
	json.Unmarshal(body, &l)
	if l.Source != "sidecar" || !l.Synced || len(l.Lines) != 2 || l.Lines[0].TimeMs != 1500 || l.Lines[0].Text != "第一句" {
		t.Fatalf("%s", body)
	}
	// a symlinked sidecar pointing outside the roots is ignored
	os.Remove(lrc)
	outside := filepath.Join(t.TempDir(), "secret.lrc")
	os.WriteFile(outside, []byte("[00:01]secret"), 0o644)
	os.Symlink(outside, lrc)
	_, body = e.do("GET", "/api/tracks/"+tr.ID+"/lyrics", "", e.token)
	if l = (library.TrackLyrics{}); json.Unmarshal(body, &l) != nil || l.Source != "embedded" {
		t.Fatalf("symlink escape: %s", body)
	}
}
