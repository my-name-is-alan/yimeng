package gateway

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

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
