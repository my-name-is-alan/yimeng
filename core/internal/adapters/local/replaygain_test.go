package local

import (
	"testing"

	"github.com/dhowden/tag"
)

func TestReadGain(t *testing.T) {
	g := readGain(map[string]any{
		"replaygain_track_gain": "-6.54 dB", "REPLAYGAIN_TRACK_PEAK": "0.988", "txxx": &tag.Comm{Description: "REPLAYGAIN_ALBUM_GAIN", Text: "+1.20 dB"},
		"----:com.apple.iTunes:replaygain_album_peak": "1.02", "bogus": "x",
	})
	if *g.TrackGain != -6.54 || *g.TrackPeak != 0.988 || *g.AlbumGain != 1.2 || *g.AlbumPeak != 1.02 {
		t.Fatalf("%+v", g)
	}
	o := readGain(map[string]any{"R128_TRACK_GAIN": "-1280", "replaygain_track_gain": "nan dB"})
	if *o.TrackGain != 0 || o.AlbumGain != nil { // -5 dB (Q7.8) + 5
		t.Fatalf("%v", *o.TrackGain)
	}
	if bad := readGain(map[string]any{"replaygain_track_gain": "999 dB"}); !bad.Empty() {
		t.Fatal("out of range accepted")
	}
}
