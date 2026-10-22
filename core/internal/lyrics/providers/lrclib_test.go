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

func TestLRCLIBSearch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search" || r.URL.Query().Get("q") != "晴天 周杰伦" {
			http.Error(w, "bad", 400)
			return
		}
		w.Write([]byte(`[{"id":7,"trackName":"晴天","artistName":"周杰伦","albumName":"叶惠美","duration":269.5,"syncedLyrics":"[00:01.00]故事"},{"id":8,"trackName":"x","plainLyrics":"p"}]`))
	}))
	defer srv.Close()
	c, err := (&LRCLIB{Base: srv.URL}).Search(t.Context(), "晴天 周杰伦", "", "")
	if err != nil || len(c) != 2 || !c[0].Synced || c[0].DurationMs != 269500 || c[1].Synced || c[1].PlainLyrics != "p" {
		t.Fatalf("%v %+v", err, c)
	}
}
