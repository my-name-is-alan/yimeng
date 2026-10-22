package library

import (
	"context"
	"path/filepath"
	"testing"
)

// ReplayGain (tag or analyzed) is exposed in the track audio object (D53).
func TestTrackAudioReplayGain(t *testing.T) {
	s, root, run := setup(t)
	copyFixture(t, "tagged.flac", filepath.Join(root, "A", "B", "01.flac"))
	run("")
	if _, err := s.db.Exec(`UPDATE source_tracks SET rg_track_gain=-6.5, rg_track_peak=0.9, rg_album_gain=-7, rg_album_peak=0.95, rg_source='analyzed'`); err != nil {
		t.Fatal(err)
	}
	p, err := s.ListTracks(context.Background(), ListParams{Limit: 10})
	if err != nil || len(p.Items) != 1 {
		t.Fatal(err, p)
	}
	tr, err := s.GetTrack(context.Background(), p.Items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	g := tr.Audio.ReplayGain
	if g == nil || *g.TrackGain != -6.5 || *g.TrackPeak != 0.9 || *g.AlbumGain != -7 || g.Source != "analyzed" {
		t.Fatalf("replaygain %+v", g)
	}
}
