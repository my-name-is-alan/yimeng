package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

func TestLRCLIB(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("track_name") == "miss" {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"syncedLyrics":"[00:01.00]hi","plainLyrics":"hi"}`))
	}))
	defer srv.Close()
	p := &LRCLIB{Base: srv.URL, Client: srv.Client()}
	text, err := p.Fetch(context.Background(), library.Track{Title: "Song", ArtistName: "A"})
	if err != nil || text != "[00:01.00]hi" {
		t.Fatal(text, err)
	}
	if _, err := p.Fetch(context.Background(), library.Track{Title: "miss"}); err != ErrNotFound {
		t.Fatal(err)
	}
	if _, err := (NetEase{}).Fetch(context.Background(), library.Track{}); err != ErrStub {
		t.Fatal(err)
	}
}
