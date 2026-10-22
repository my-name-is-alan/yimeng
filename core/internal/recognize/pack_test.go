package recognize

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func loadPeaks(t *testing.T, name string) Envelope {
	t.Helper()
	var j struct {
		Duration float32 `json:"duration"`
		Peaks    []struct {
			Bin  uint32  `json:"bin"`
			Time uint32  `json:"time"`
			Mag  float32 `json:"mag"`
		} `json:"peaks"`
	}
	b, err := os.ReadFile(filepath.Join("testdata", name+".peaks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &j); err != nil {
		t.Fatal(err)
	}
	e := Envelope{Duration: j.Duration}
	for _, p := range j.Peaks {
		e.Peaks = append(e.Peaks, Peak{Bin: p.Bin, Time: p.Time, Mag: p.Mag})
	}
	return e
}

func TestPackRoundTrip(t *testing.T) {
	for _, name := range []string{"zero", "tone1k", "short"} {
		e := loadPeaks(t, name)
		ct, err := Pack(e)
		if err != nil {
			t.Fatal(err)
		}
		back, err := Unpack(ct)
		if err != nil {
			t.Fatal(err)
		}
		if back.Duration != e.Duration || len(back.Peaks) != len(e.Peaks) {
			t.Fatalf("%s: got dur=%v n=%d want dur=%v n=%d", name, back.Duration, len(back.Peaks), e.Duration, len(e.Peaks))
		}
		for i := range e.Peaks {
			if back.Peaks[i] != e.Peaks[i] {
				t.Fatalf("%s peak[%d]: got %+v want %+v", name, i, back.Peaks[i], e.Peaks[i])
			}
		}
	}
}

func TestUnpackWasmVectors(t *testing.T) {
	for _, name := range []string{"zero", "tone1k", "short"} {
		raw, err := os.ReadFile(filepath.Join("testdata", name+".fp"))
		if err != nil {
			t.Fatal(err)
		}
		got, err := Unpack(raw)
		if err != nil {
			t.Fatal(name, err)
		}
		want := loadPeaks(t, name)
		if got.Duration != want.Duration || len(got.Peaks) != len(want.Peaks) {
			t.Fatalf("%s: unpack wasm: dur=%v n=%d want dur=%v n=%d", name, got.Duration, len(got.Peaks), want.Duration, len(want.Peaks))
		}
		for i := range want.Peaks {
			if got.Peaks[i] != want.Peaks[i] {
				t.Fatalf("%s peak[%d]: got %+v want %+v", name, i, got.Peaks[i], want.Peaks[i])
			}
		}
	}
}

func TestSupported(t *testing.T) {
	if !Supported() {
		t.Fatal("version mismatch")
	}
}
