package gateway

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/transcode"
)

func flacTrack(t *testing.T, e *env) library.Track {
	t.Helper()
	if res, body := e.do("POST", "/api/library/scan", "", e.token); res.StatusCode != 202 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	e.sc.Wait()
	var tp library.Page[library.Track]
	_, body := e.do("GET", "/api/tracks?limit=50", "", e.token)
	json.Unmarshal(body, &tp)
	for _, tr := range tp.Items {
		if tr.Format == "flac" {
			if tr.Stream == nil || !tr.Stream.Lossless || tr.Stream.SampleRate == 0 {
				t.Fatalf("stream info missing: %s", body)
			}
			return tr
		}
	}
	t.Fatalf("no flac track: %s", body)
	return library.Track{}
}

func TestStreamOriginalConditional(t *testing.T) {
	e := newEnv(t)
	tr := flacTrack(t, e)
	u := "/api/stream/" + tr.ID
	res, full := e.do("GET", u, "", e.token)
	etag := res.Header.Get("ETag")
	if res.StatusCode != 200 || etag == "" || res.Header.Get("Last-Modified") == "" || res.Header.Get("X-Stream-Profile") != "original" {
		t.Fatalf("%d %v", res.StatusCode, res.Header)
	}
	if res, b := e.do("GET", u, "", e.token, "Range", "bytes=0-9", "If-Range", etag); res.StatusCode != 206 || string(b) != string(full[:10]) {
		t.Fatalf("if-range match %d", res.StatusCode)
	}
	if res, b := e.do("GET", u, "", e.token, "Range", "bytes=0-9", "If-Range", `"stale"`); res.StatusCode != 200 || len(b) != len(full) {
		t.Fatalf("if-range mismatch %d", res.StatusCode)
	}
	if res, _ := e.do("GET", u, "", e.token, "If-None-Match", etag); res.StatusCode != 304 {
		t.Fatalf("inm %d", res.StatusCode)
	}
	if res, b := e.do("HEAD", u, "", e.token); res.StatusCode != 200 || len(b) != 0 || res.ContentLength != int64(len(full)) {
		t.Fatalf("head %d %d", res.StatusCode, res.ContentLength)
	}
	for _, q := range []string{"format=wav", "format=mp3&bitrate=999", "format=flac&bitrate=320", "bitrate=128",
		"format=-f%20null", "format=mp3&bitrate=128k"} {
		if res, _ := e.do("GET", u+"?"+q, "", e.token); res.StatusCode != 400 {
			t.Errorf("%s: %d", q, res.StatusCode)
		}
	}
	// flac -> flac is pointless: original served
	if res, _ := e.do("GET", u+"?format=flac", "", e.token); e.tc != nil && res.Header.Get("X-Stream-Profile") != "original" {
		t.Fatal(res.Header)
	}
}

func TestStreamTranscode(t *testing.T) {
	e := newEnv(t)
	tr := flacTrack(t, e)
	u := "/api/stream/" + tr.ID + "?format=mp3&bitrate=128"
	if e.tc == nil || !e.tc.Supports(mustProfile("mp3", "128")) {
		if res, body := e.do("GET", u, "", e.token); res.StatusCode != 400 || !strings.Contains(string(body), "format_unavailable") {
			t.Fatalf("%d %s", res.StatusCode, body)
		}
		t.Skip("ffmpeg/libmp3lame unavailable")
	}
	// HEAD before anything is cached does not start a transcode
	if res, _ := e.do("HEAD", u, "", e.token); res.StatusCode != 200 || res.Header.Get("Accept-Ranges") != "none" {
		t.Fatalf("head %d %v", res.StatusCode, res.Header)
	}
	// first request: progressive, Range ignored
	res, first := e.do("GET", u, "", e.token, "Range", "bytes=0-9")
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "audio/mpeg" || res.Header.Get("Accept-Ranges") != "none" ||
		res.Header.Get("X-Stream-Profile") != "mp3-128" || len(first) < 1000 {
		t.Fatalf("progressive %d %v len=%d", res.StatusCode, res.Header, len(first))
	}
	etag := res.Header.Get("ETag")
	// then cached: Range works
	deadline := time.Now().Add(5 * time.Second)
	for {
		res, _ = e.do("HEAD", u, "", e.token)
		if res.Header.Get("Accept-Ranges") == "bytes" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("never cached")
		}
		time.Sleep(20 * time.Millisecond)
	}
	res, b := e.do("GET", u, "", e.token, "Range", "bytes=0-99", "If-Range", etag)
	if res.StatusCode != 206 || string(b) != string(first[:100]) || res.Header.Get("ETag") != etag {
		t.Fatalf("cached range %d", res.StatusCode)
	}
	if res, _ := e.do("GET", u, "", e.token, "If-None-Match", etag); res.StatusCode != 304 {
		t.Fatalf("inm %d", res.StatusCode)
	}
}

func TestStreamQualityDefault(t *testing.T) {
	e := newEnv(t)
	tr := flacTrack(t, e)
	res, body := e.do("GET", "/api/devices/me/stream-quality", "", e.token)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"format":"original"`) || !strings.Contains(string(body), `"available"`) {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	for _, bad := range []string{`{"format":"wav"}`, `{"format":"mp3","bitrate_kbps":1}`, `{"format":"flac","bitrate_kbps":320}`, `{"x":1}`} {
		if res, _ := e.do("PUT", "/api/devices/me/stream-quality", bad, e.token); res.StatusCode != 400 {
			t.Errorf("%s: %d", bad, res.StatusCode)
		}
	}
	res, body = e.do("PUT", "/api/devices/me/stream-quality", `{"format":"opus"}`, e.token)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"format":"opus","bitrate_kbps":160`) {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	res, _ = e.do("HEAD", "/api/stream/"+tr.ID, "", e.token)
	want := "opus-160"
	if !e.tc.Supports(mustProfile("opus", "")) {
		want = "original" // default kept, falls back silently
	}
	if res.Header.Get("X-Stream-Profile") != want {
		t.Fatalf("device default not applied: %v", res.Header)
	}
	// explicit query wins
	if res, _ = e.do("HEAD", "/api/stream/"+tr.ID+"?format=original", "", e.token); res.Header.Get("X-Stream-Profile") != "original" {
		t.Fatal(res.Header)
	}
}

func mustProfile(f, b string) transcode.Profile {
	p, err := transcode.ParseProfile(f, b)
	if err != nil {
		panic(err)
	}
	return p
}
