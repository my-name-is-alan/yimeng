package gateway

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompress(t *testing.T) {
	big := strings.Repeat(`{"a":"bbbbbbbbbb"},`, 200)
	h := compress(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/big":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, big)
		case "/small":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"ok":true}`)
		case "/audio":
			w.Header().Set("Content-Type", "audio/flac")
			io.WriteString(w, big)
		}
	}))
	get := func(p string, hdr ...string) *http.Response {
		r := httptest.NewRequest("GET", p, nil)
		r.Header.Set("Accept-Encoding", "gzip")
		for i := 0; i+1 < len(hdr); i += 2 {
			r.Header.Set(hdr[i], hdr[i+1])
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Result()
	}
	res := get("/big")
	if res.Header.Get("Content-Encoding") != "gzip" {
		t.Fatal("big not gzipped")
	}
	zr, _ := gzip.NewReader(res.Body)
	if b, _ := io.ReadAll(zr); string(b) != big {
		t.Fatal("roundtrip")
	}
	for _, c := range [][]string{{"/small"}, {"/audio"}, {"/big", "Range", "bytes=0-10"}} {
		if res := get(c[0], c[1:]...); res.Header.Get("Content-Encoding") != "" {
			t.Errorf("%v gzipped", c)
		}
	}
	if b, _ := io.ReadAll(get("/small").Body); string(b) != `{"ok":true}` {
		t.Errorf("small body %q", b)
	}
}
