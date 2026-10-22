package recognize

import "testing"

func TestMatchWindowsDeterministic(t *testing.T) {
	pcm := make([]float32, SampleRate*12)
	a := MatchWindows(pcm)
	b := MatchWindows(pcm)
	if len(a) != len(b) || len(a) < 2 {
		t.Fatalf("len a=%d b=%d", len(a), len(b))
	}
	for i := range a {
		if a[i].Label != b[i].Label || len(a[i].PCM) != len(b[i].PCM) {
			t.Fatalf("window %d diverged", i)
		}
	}
}

func TestExtractPeaksDeterministic(t *testing.T) {
	pcm := loadF32(t, "tone1k")
	a := ExtractPeaks(pcm)
	b := ExtractPeaks(pcm)
	if len(a.Peaks) != len(b.Peaks) || a.Duration != b.Duration {
		t.Fatalf("nondeterministic peaks")
	}
	for i := range a.Peaks {
		if a.Peaks[i] != b.Peaks[i] {
			t.Fatalf("peak %d: %+v vs %+v", i, a.Peaks[i], b.Peaks[i])
		}
	}
}
