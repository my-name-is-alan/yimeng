package gateway

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

func TestDownloadFilename(t *testing.T) {
	n := downloadFilename(library.Track{Title: "Hello/World?", ArtistName: "A:B"}, "flac")
	if !strings.HasSuffix(n, ".flac") || strings.ContainsAny(n, `/<>:"\|?*`) {
		t.Fatal(n)
	}
	cd := contentDisposition("测.flac")
	if !strings.Contains(cd, "attachment;") || !strings.Contains(cd, "filename*=") {
		t.Fatal(cd)
	}
}

func TestDownloadAPI(t *testing.T) {
	e := newEnv(t)
	if res, body := e.do("POST", "/api/library/scan", "", e.token); res.StatusCode != 202 {
		t.Fatal(res.StatusCode, string(body))
	}
	deadline := time.Now().Add(10 * time.Second)
	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	for time.Now().Before(deadline) {
		_, body := e.do("GET", "/api/tracks?limit=5", "", e.token)
		_ = json.Unmarshal(body, &page)
		if len(page.Items) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(page.Items) == 0 {
		t.Fatal("no tracks after scan")
	}
	id := page.Items[0].ID
	res, body := e.do("GET", "/api/tracks/"+id+"/download", "", e.token)
	if res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	cd := res.Header.Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") {
		t.Fatal(cd)
	}
	if res.Header.Get("X-Download-Profile") != "original" {
		t.Fatal(res.Header.Get("X-Download-Profile"))
	}
	if len(body) < 100 {
		t.Fatalf("short body %d", len(body))
	}
}
