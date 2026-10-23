package local

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func near(t *testing.T, name string, got int64, want time.Duration) {
	nearTol(t, name, got, want, 60*time.Millisecond)
}

func nearTol(t *testing.T, name string, got int64, want, tol time.Duration) {
	t.Helper()
	if d := time.Duration(got)*time.Millisecond - want; d < -tol || d > tol {
		t.Errorf("%s: duration %dms, want ~%v", name, got, want)
	}
}

func TestReadTaggedFixtures(t *testing.T) {
	for _, f := range []string{"tagged.mp3", "tagged.flac", "tagged.m4a", "tagged.opus"} {
		t.Run(f, func(t *testing.T) {
			tr, err := ReadFile(filepath.Join("testdata", f))
			if err != nil {
				t.Fatal(err)
			}
			if !tr.HasTags || tr.Title != "梦里花" || tr.Artist != "测试艺人" || tr.Album != "译梦测试专辑" {
				t.Errorf("tags: %+v", tr)
			}
			if tr.AlbumArtist != "Various Artists" || tr.Genre != "Pop" || tr.Year != 2024 {
				t.Errorf("albumartist/genre/year: %+v", tr)
			}
			if tr.TrackNo != 3 || tr.DiscNo != 1 || tr.TrackTotal != 10 {
				t.Errorf("track/disc: %d/%d", tr.TrackNo, tr.DiscNo)
			}
			near(t, f, tr.DurationMs, time.Second)
			if f != "tagged.opus" && (tr.Picture == nil || tr.Picture.Ext != "png" || len(tr.Picture.Data) == 0) {
				t.Errorf("picture missing: %+v", tr.Picture)
			}
		})
	}
}

func TestNoTagsFallsBackToFilename(t *testing.T) {
	tr, err := ReadFile(filepath.Join("testdata", "notags.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	if tr.Title != "notags" || tr.Format != "mp3" {
		t.Fatalf("%+v", tr)
	}
	// CBR without Xing is an estimate; encoder padding adds a few frames.
	nearTol(t, "cbr", tr.DurationMs, 2*time.Second, 250*time.Millisecond)
}

func TestWavDuration(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.wav")
	if err := os.WriteFile(p, MakeWAV(8000, 1500*time.Millisecond), 0o644); err != nil {
		t.Fatal(err)
	}
	tr, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	near(t, "wav", tr.DurationMs, 1500*time.Millisecond)
}

func TestSupported(t *testing.T) {
	if !Supported("/a/B.FLAC") || Supported("/a/cover.jpg") {
		t.Fatal("Supported")
	}
}

// MakeWAV builds a mono 8-bit silent PCM WAV (helper shared by tests).
func MakeWAV(rate int, d time.Duration) []byte {
	n := int(float64(rate) * d.Seconds())
	var b bytes.Buffer
	w := func(v any) { binary.Write(&b, binary.LittleEndian, v) }
	b.WriteString("RIFF")
	w(uint32(36 + n))
	b.WriteString("WAVEfmt ")
	w(uint32(16))
	w(uint16(1))
	w(uint16(1))
	w(uint32(rate))
	w(uint32(rate))
	w(uint16(1))
	w(uint16(8))
	b.WriteString("data")
	w(uint32(n))
	b.Write(bytes.Repeat([]byte{128}, n))
	return b.Bytes()
}

func TestProbeStreamInfo(t *testing.T) {
	want := map[string]Info{
		"tagged.flac": {Codec: "flac", SampleRate: 8000, BitDepth: 16, Channels: 1, Lossless: true},
		"tagged.mp3":  {Codec: "mp3", SampleRate: 22050, Channels: 1},
		"tagged.m4a":  {Codec: "aac", SampleRate: 22050, Channels: 1},
		"tagged.opus": {Codec: "opus", SampleRate: 48000, Channels: 1},
	}
	for f, w := range want {
		tr, err := ReadFile(filepath.Join("testdata", f))
		if err != nil {
			t.Fatal(err)
		}
		a := tr.Audio
		if a.Codec != w.Codec || a.SampleRate != w.SampleRate || a.BitDepth != w.BitDepth || a.Channels != w.Channels ||
			a.Lossless != w.Lossless || a.BitrateKbps <= 0 {
			t.Errorf("%s: %+v want %+v", f, a, w)
		}
	}
	p := filepath.Join(t.TempDir(), "x.wav")
	os.WriteFile(p, MakeWAV(8000, time.Second), 0o644)
	tr, _ := ReadFile(p)
	if tr.Audio.Codec != "pcm" || tr.Audio.BitDepth != 8 || tr.Audio.SampleRate != 8000 || tr.Audio.BitrateKbps != 64 || !tr.Audio.Lossless {
		t.Errorf("wav %+v", tr.Audio)
	}
}
