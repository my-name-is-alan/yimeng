package recognize

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// DecodePCM converts an audio file (m4a/aac/mp3/wav/…) to mono f32le @ 8 kHz
// via ffmpeg. Input is written to a short-lived temp file (mp4/m4a need a
// seekable source); the file is removed before return. The process is bounded
// by timeout and argument-list only (no shell).
func DecodePCM(ctx context.Context, ffmpeg string, audio []byte, maxSec float64) ([]float32, error) {
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	if maxSec <= 0 {
		maxSec = 15
	}
	tmp, err := os.CreateTemp("", "ym-recognize-*.audio")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(audio); err != nil {
		tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg,
		"-hide_banner", "-loglevel", "error",
		"-i", tmpPath,
		"-t", fmt.Sprintf("%.3f", maxSec),
		"-ac", "1", "-ar", fmt.Sprintf("%d", SampleRate),
		"-f", "f32le", "pipe:1",
	)
	var stdout, stderr bytesBuf
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg: %w (%s)", err, stderr.String())
	}
	b := stdout.Bytes()
	if len(b)%4 != 0 {
		return nil, fmt.Errorf("ffmpeg: odd pcm length %d", len(b))
	}
	if len(b) == 0 {
		return nil, fmt.Errorf("ffmpeg: empty pcm from %s", filepath.Base(tmpPath))
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out, nil
}

// tiny buffer to avoid importing bytes solely for the name clash with audio []byte.
type bytesBuf struct{ b []byte }

func (w *bytesBuf) Write(p []byte) (int, error) {
	w.b = append(w.b, p...)
	return len(p), nil
}
func (w *bytesBuf) Bytes() []byte  { return w.b }
func (w *bytesBuf) String() string { return string(w.b) }
