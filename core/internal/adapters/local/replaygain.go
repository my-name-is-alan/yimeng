package local

import (
	"math"
	"strconv"
	"strings"

	"github.com/dhowden/tag"
)

// Gain holds ReplayGain values in dB relative to the RG2 reference (-18 LUFS); peaks are linear.
type Gain struct {
	TrackGain *float64 `json:"track_gain,omitempty"`
	TrackPeak *float64 `json:"track_peak,omitempty"`
	AlbumGain *float64 `json:"album_gain,omitempty"`
	AlbumPeak *float64 `json:"album_peak,omitempty"`
}

func (g Gain) Empty() bool { return g.TrackGain == nil && g.AlbumGain == nil }

func parseDB(s string) (float64, bool) {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(strings.TrimSuffix(strings.ToLower(s), "db")), "lufs"))
	f, err := strconv.ParseFloat(strings.TrimPrefix(s, "+"), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < -60 || f > 60 {
		return 0, false
	}
	return f, true
}

func parsePeak(s string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsNaN(f) || f < 0 || f > 10 {
		return 0, false
	}
	return f, true
}

// readGain extracts REPLAYGAIN_* (Vorbis, ID3 TXXX, MP4 freeform) and
// R128_*_GAIN (Opus; Q7.8 dB relative to -23 LUFS → +5 dB to the RG reference).
func readGain(raw map[string]any) Gain {
	var g Gain
	set := func(dst **float64, v float64) {
		if *dst == nil {
			*dst = &v
		}
	}
	for k, v := range raw {
		key := strings.ToLower(k)
		var val string
		switch x := v.(type) {
		case string:
			val = x
		case []string:
			if len(x) > 0 {
				val = x[0]
			}
		case *tag.Comm:
			key, val = strings.ToLower(x.Description), x.Text
		case []byte:
			val = string(x)
		default:
			continue
		}
		// MP4 freeform keys look like "----:com.apple.iTunes:replaygain_track_gain"
		if i := strings.LastIndex(key, ":"); i >= 0 {
			key = key[i+1:]
		}
		switch key {
		case "replaygain_track_gain":
			if f, ok := parseDB(val); ok {
				set(&g.TrackGain, f)
			}
		case "replaygain_album_gain":
			if f, ok := parseDB(val); ok {
				set(&g.AlbumGain, f)
			}
		case "replaygain_track_peak":
			if f, ok := parsePeak(val); ok {
				set(&g.TrackPeak, f)
			}
		case "replaygain_album_peak":
			if f, ok := parsePeak(val); ok {
				set(&g.AlbumPeak, f)
			}
		case "r128_track_gain", "r128_album_gain":
			n, err := strconv.Atoi(strings.TrimSpace(val))
			if err != nil || n < -32768 || n > 32767 {
				continue
			}
			db := math.Round((float64(n)/256+5)*100) / 100
			if key == "r128_track_gain" {
				set(&g.TrackGain, db)
			} else {
				set(&g.AlbumGain, db)
			}
		}
	}
	return g
}
