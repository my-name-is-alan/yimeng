package gateway

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/auth"
	"github.com/my-name-is-alan/yimeng/core/internal/recognize"
)

func TestSniffAudio(t *testing.T) {
	if !sniffAudio([]byte("ID3.........x")) {
		t.Fatal("mp3 id3")
	}
	if !sniffAudio([]byte{0xff, 0xfb, 0x90, 0x00, 0, 0, 0, 0, 0, 0, 0, 0}) {
		t.Fatal("mp3 frame")
	}
	wav := append([]byte("RIFF....WAVE"), make([]byte, 0)...)
	if !sniffAudio(append(wav, make([]byte, 4)...)) {
		t.Fatal("wav")
	}
	m4a := []byte{0, 0, 0, 0x20, 'f', 't', 'y', 'p', 'M', '4', 'A', ' '}
	if !sniffAudio(m4a) {
		t.Fatal("m4a")
	}
	if sniffAudio([]byte("not audio!!")) {
		t.Fatal("false positive")
	}
}

func TestRecognizeUnsupportedWithoutAuth(t *testing.T) {
	if !recognize.Supported() {
		t.Fatal("supported")
	}
	d := Deps{}
	mux := http.NewServeMux()
	d.mountRecognize(mux)
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, _ := w.CreateFormFile("audio", "x.mp3")
	_, _ = part.Write([]byte("ID3....fake"))
	_ = w.Close()
	req := httptest.NewRequest("POST", "/api/v1/recognize", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d %s", rr.Code, rr.Body.String())
	}
}

func TestRecognizeLimiter(t *testing.T) {
	l := &recognizeLimiter{}
	for i := 0; i < recognizeRate; i++ {
		if !l.allow("u1") {
			t.Fatalf("denied early at %d", i)
		}
	}
	if l.allow("u1") {
		t.Fatal("should rate-limit")
	}
	if !l.allow("u2") {
		t.Fatal("other user blocked")
	}
}

// TestRecognizeRateLimitBeforeDecode ensures the 11th request in a minute for
// the same account is 429 and never reaches Online/ffmpeg (Online is nil →
// would be 503 if the limiter did not fire first).
func TestRecognizeRateLimitBeforeDecode(t *testing.T) {
	// Isolate the package-level limiter for this test account.
	key := recognizeAccountKey(auth.Device{UserID: "test-rl-before-decode"})
	recognizeRL.mu.Lock()
	delete(recognizeRL.hits, key)
	recognizeRL.mu.Unlock()

	d := Deps{} // Online == nil
	mux := http.NewServeMux()
	d.mountRecognize(mux)

	post := func() *httptest.ResponseRecorder {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		part, _ := w.CreateFormFile("audio", "x.m4a")
		_, _ = part.Write([]byte{0, 0, 0, 0x20, 'f', 't', 'y', 'p', 'M', '4', 'A', ' '})
		_ = w.Close()
		req := httptest.NewRequest("POST", "/api/v1/recognize", &buf)
		req.Header.Set("Content-Type", w.FormDataContentType())
		req = req.WithContext(withDevice(req.Context(), auth.Device{UserID: "test-rl-before-decode", ID: "dev_rl"}))
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		return rr
	}

	for i := 0; i < recognizeRate; i++ {
		rr := post()
		// Auth ok, limiter allows → Online nil → 503 unavailable (proves we got past the limiter).
		if rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("attempt %d: want 503 (no Online), got %d %s", i, rr.Code, rr.Body.String())
		}
	}
	rr := post()
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "rate_limited") {
		t.Fatalf("body %s", rr.Body.String())
	}
}

func TestRecognizeAccountKey(t *testing.T) {
	if recognizeAccountKey(auth.Device{UserID: "u", ID: "d"}) != "u:u" {
		t.Fatal("user preferred")
	}
	if recognizeAccountKey(auth.Device{ID: "d"}) != "d:d" {
		t.Fatal("device fallback")
	}
}

func TestReadRecognizeAudioDurationHintOptional(t *testing.T) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("duration_ms", "100")
	part, _ := w.CreateFormFile("audio", "x.m4a")
	_, _ = part.Write([]byte{0, 0, 0, 0x20, 'f', 't', 'y', 'p', 'M', '4', 'A', ' '})
	_ = w.Close()
	req := httptest.NewRequest("POST", "/api/v1/recognize", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	audio, hint, err := readRecognizeAudio(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(audio) < 8 || hint != 100 {
		t.Fatalf("audio=%d hint=%v", len(audio), hint)
	}
}
