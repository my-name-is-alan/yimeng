package gateway

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/auth"
	"github.com/my-name-is-alan/yimeng/core/internal/online"
	"github.com/my-name-is-alan/yimeng/core/internal/online/netease"
	"github.com/my-name-is-alan/yimeng/core/internal/recognize"
)

const (
	recognizeMaxBytes = 2 << 20 // 2 MiB
	recognizeMaxSec   = 15
	recognizeRate     = 10 // per account per minute
)

// recognizeLimiter is a simple per-account token bucket (10/min).
type recognizeLimiter struct {
	mu   sync.Mutex
	hits map[string][]time.Time
}

func (l *recognizeLimiter) allow(user string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.hits == nil {
		l.hits = map[string][]time.Time{}
	}
	now := time.Now()
	cut := now.Add(-time.Minute)
	xs := l.hits[user]
	i := 0
	for _, t := range xs {
		if t.After(cut) {
			xs[i] = t
			i++
		}
	}
	xs = xs[:i]
	if len(xs) >= recognizeRate {
		l.hits[user] = xs
		return false
	}
	l.hits[user] = append(xs, now)
	return true
}

var recognizeRL = &recognizeLimiter{}

func (d Deps) mountRecognize(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/recognize", d.recognize)
	mux.HandleFunc("POST /api/v1/recognize", d.recognize)
}

type recognizeMatchDTO struct {
	Title    string           `json:"title"`
	Artist   string           `json:"artist"`
	Album    string           `json:"album"`
	Cover    *online.CoverRef `json:"cover,omitempty"`
	Origin   string           `json:"origin"`
	TrackID  string           `json:"track_id,omitempty"`
	OffsetMs int              `json:"offset_ms"`
}

func (d Deps) recognize(w http.ResponseWriter, r *http.Request) {
	if !recognize.Supported() {
		writeErr(w, http.StatusServiceUnavailable, "fingerprint_unsupported", "听歌识曲指纹格式已变更，请升级 Core")
		return
	}
	dev, ok := DeviceFrom(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized", "login required")
		return
	}
	// Enforce 10/min per account BEFORE decoding (expensive ffmpeg + NetEase).
	if !recognizeRL.allow(recognizeAccountKey(dev)) {
		writeErr(w, http.StatusTooManyRequests, "rate_limited", "最多每分钟 10 次识曲")
		return
	}
	if d.Online == nil {
		writeErr(w, http.StatusServiceUnavailable, "unavailable", "online plugins not configured")
		return
	}
	p, _, err := d.Online.Reg.Enabled("netease")
	if err != nil {
		writeErr(w, http.StatusConflict, "not_logged_in", "请先在设置中登录网易云音乐")
		return
	}
	np, ok := p.(*netease.Plugin)
	if !ok {
		writeErr(w, http.StatusServiceUnavailable, "unavailable", "netease plugin unavailable")
		return
	}
	if _, err := np.Account(r.Context()); err != nil {
		writeErr(w, http.StatusConflict, "not_logged_in", "请先在设置中登录网易云音乐")
		return
	}

	audio, durHint, err := readRecognizeAudio(r)
	if err != nil {
		if err.Error() == "too_long" {
			writeErr(w, http.StatusRequestEntityTooLarge, "too_long", "音频须 ≤15 秒且 ≤2 MiB")
			return
		}
		writeErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	// Audio stays in memory only; ffmpeg reads from stdin (no temp file).

	ffmpeg := "ffmpeg"
	if d.Transcoder != nil {
		if f := d.Transcoder.FFmpeg(); f != "" {
			ffmpeg = f
		}
	}
	pcm, err := recognize.DecodePCM(r.Context(), ffmpeg, audio, recognizeMaxSec)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid_body", "无法解码音频（需要 m4a/aac/mp3/wav）")
		return
	}
	if float64(len(pcm))/recognize.SampleRate > recognizeMaxSec+0.5 {
		writeErr(w, http.StatusRequestEntityTooLarge, "too_long", "音频须 ≤15 秒且 ≤2 MiB")
		return
	}
	// duration_ms from the client is informational only (readRecognizeAudio still
	// parses it for logs/future use). Fingerprint duration is ALWAYS the
	// server-measured PCM length — overriding it with a short/wrong hint
	// (e.g. 100 ms) makes NetEase return no_match for an otherwise good clip.
	_ = durHint
	hits, err := matchFingerprintWindows(r.Context(), np, pcm)
	if err != nil {
		d.onlineErr(w, "recognize", err)
		return
	}
	if len(hits) == 0 {
		writeErr(w, http.StatusNotFound, "no_match", "没有识别到歌曲")
		return
	}
	out := make([]recognizeMatchDTO, 0, len(hits))
	for _, h := range hits {
		dto := recognizeMatchDTO{
			Title: h.Title, Artist: h.Artist, Album: h.Album,
			Origin: "netease", OffsetMs: h.OffsetMs,
			TrackID: fmt.Sprintf("netease:%d", h.SongID),
		}
		if h.CoverURL != "" {
			dto.Cover = &online.CoverRef{URL: d.imageURL(h.CoverURL), SourceURL: h.CoverURL}
		}
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": out})
}

func readRecognizeAudio(r *http.Request) (audio []byte, durationMs float64, err error) {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/") {
		if err := r.ParseMultipartForm(recognizeMaxBytes + 1<<20); err != nil {
			return nil, 0, fmt.Errorf("invalid multipart: %w", err)
		}
		if v := r.FormValue("duration_ms"); v != "" {
			fmt.Sscanf(v, "%f", &durationMs)
		}
		f, hdr, err := r.FormFile("audio")
		if err != nil {
			return nil, 0, fmt.Errorf("missing audio part")
		}
		defer f.Close()
		if hdr.Size > recognizeMaxBytes {
			return nil, 0, fmt.Errorf("too_long")
		}
		audio, err = io.ReadAll(io.LimitReader(f, recognizeMaxBytes+1))
		if err != nil {
			return nil, 0, err
		}
	} else {
		audio, err = io.ReadAll(io.LimitReader(r.Body, recognizeMaxBytes+1))
		if err != nil {
			return nil, 0, err
		}
	}
	if len(audio) == 0 {
		return nil, 0, fmt.Errorf("empty audio")
	}
	if len(audio) > recognizeMaxBytes {
		return nil, 0, fmt.Errorf("too_long")
	}
	if !sniffAudio(audio) {
		return nil, 0, fmt.Errorf("unsupported audio type (need m4a/aac/mp3/wav)")
	}
	return audio, durationMs, nil
}

func sniffAudio(b []byte) bool {
	if len(b) < 12 {
		return false
	}
	if bytes.HasPrefix(b, []byte("ID3")) || (b[0] == 0xff && b[1]&0xe0 == 0xe0) {
		return true // mp3
	}
	if bytes.HasPrefix(b, []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WAVE")) {
		return true
	}
	if bytes.HasPrefix(b, []byte("fLaC")) {
		return true
	}
	if bytes.HasPrefix(b, []byte("OggS")) {
		return true
	}
	// m4a/aac: ftyp box
	if len(b) > 8 && bytes.Equal(b[4:8], []byte("ftyp")) {
		return true
	}
	return false
}

func recognizeAccountKey(dev auth.Device) string {
	if dev.UserID != "" {
		return "u:" + dev.UserID
	}
	if dev.ID != "" {
		return "d:" + dev.ID
	}
	return "anon"
}

// matchFingerprintWindows tries a deterministic set of PCM windows until NetEase
// returns a hit. Duration sent upstream is always measured from that window.
func matchFingerprintWindows(ctx context.Context, np *netease.Plugin, pcm []float32) ([]netease.Match, error) {
	var lastErr error
	for _, w := range recognize.MatchWindows(pcm) {
		env := recognize.ExtractPeaks(w.PCM)
		if len(env.Peaks) == 0 {
			continue
		}
		fp, err := recognize.Pack(env)
		if err != nil {
			return nil, err
		}
		hits, err := np.MatchFingerprint(ctx, fp, float64(env.Duration))
		if err != nil {
			lastErr = err
			continue
		}
		if len(hits) > 0 {
			return hits, nil
		}
	}
	return nil, lastErr
}
