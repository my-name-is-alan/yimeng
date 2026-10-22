package recognize

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func loadF32(t *testing.T, name string) []float32 {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name+".f32"))
	if err != nil {
		t.Fatal(err)
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}

func TestExtractPeaksTone1kPrimary(t *testing.T) {
	pcm := loadF32(t, "tone1k")
	got := ExtractPeaks(pcm)
	want := loadPeaks(t, "tone1k")
	if math.Abs(float64(got.Duration-want.Duration)) > 1e-3 {
		t.Fatalf("duration %v want %v", got.Duration, want.Duration)
	}
	var n256 int
	var magSum float64
	for _, p := range got.Peaks {
		if p.Bin == 256 {
			n256++
			magSum += float64(p.Mag)
			if math.Abs(float64(p.Mag-18.04042)) > 0.05 {
				t.Fatalf("mag %v", p.Mag)
			}
		}
	}
	if n256 < 50 {
		t.Fatalf("too few primary peaks: %d (total %d)", n256, len(got.Peaks))
	}
}

func TestExtractPeaksSilence(t *testing.T) {
	pcm := loadF32(t, "zero")
	got := ExtractPeaks(pcm)
	if len(got.Peaks) != 0 {
		t.Fatalf("silence yielded %d peaks", len(got.Peaks))
	}
}
