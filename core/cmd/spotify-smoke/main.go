// Command spotify-smoke runs a read-only client-credentials smoke test of the
// Spotify metadata provider. Credentials are read from the environment
// (YIMENG_SPOTIFY_CLIENT_ID / _SECRET, optional _API_BASE_URL / _AUTH_BASE_URL)
// and are never printed; neither is the access token.
//
//	-dump DIR  also writes raw Web API responses (never the token endpoint) for fixtures.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/adapters/spotify"
	"github.com/my-name-is-alan/yimeng/core/internal/metadata"
)

type dumper struct {
	dir string
	n   int
}

var unsafe = regexp.MustCompile(`[^a-zA-Z0-9]+`)

func (d *dumper) RoundTrip(r *http.Request) (*http.Response, error) {
	res, err := http.DefaultTransport.RoundTrip(r)
	if err != nil || d.dir == "" || strings.Contains(r.URL.Path, "/api/token") || !strings.Contains(res.Header.Get("Content-Type"), "json") {
		return res, err
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	res.Body = io.NopCloser(bytes.NewReader(b))
	d.n++
	name := fmt.Sprintf("%02d_%s.json", d.n, strings.Trim(unsafe.ReplaceAllString(r.URL.Path, "_"), "_"))
	os.WriteFile(filepath.Join(d.dir, name), b, 0o600)
	return res, nil
}

func main() {
	dump := flag.String("dump", "", "directory for raw API responses")
	flag.Parse()
	id, secret := os.Getenv("YIMENG_SPOTIFY_CLIENT_ID"), os.Getenv("YIMENG_SPOTIFY_CLIENT_SECRET")
	if id == "" || secret == "" {
		fmt.Println("credentials not set in env")
		os.Exit(2)
	}
	if *dump != "" {
		os.MkdirAll(*dump, 0o700)
	}
	p := spotify.New(spotify.Config{
		APIBase: os.Getenv("YIMENG_SPOTIFY_API_BASE_URL"), AuthBase: os.Getenv("YIMENG_SPOTIFY_AUTH_BASE_URL"),
		HTTP:  &http.Client{Timeout: 20 * time.Second, Transport: &dumper{dir: *dump}},
		Creds: func() (string, string, bool) { return id, secret, true }, UserAgent: "yimeng-smoke",
	})
	ctx := context.Background()
	fmt.Println("health:", p.Health(ctx))
	ts, err := p.LookupTrack(ctx, metadata.TrackQuery{Title: "晴天", Artist: "周杰伦", DurationMs: 269000})
	fmt.Println("tracks:", len(ts), err)
	for i, t := range ts {
		if i < 3 {
			fmt.Printf("  %.2f %s — %s [%s] %s isrc=%s pop=%d genre=%q cover=%v\n", t.Score, t.Title, t.Artist, t.Album, t.ReleaseDate, t.ISRC, t.Popularity, t.Genre, t.CoverURL != "")
		}
	}
	if len(ts) > 0 && ts[0].ISRC != "" {
		byISRC, err := p.LookupTrack(ctx, metadata.TrackQuery{ISRC: ts[0].ISRC})
		fmt.Println("isrc lookup:", len(byISRC), err)
	}
	as, err := p.LookupAlbum(ctx, metadata.AlbumQuery{Title: "叶惠美", AlbumArtist: "周杰伦"})
	fmt.Println("albums:", len(as), err)
	for i, a := range as {
		if i < 3 {
			fmt.Printf("  %s — %s %s label=%q genres=%v upc=%v pop=%d tracks=%d cover=%v\n", a.Title, a.AlbumArtist, a.ReleaseDate, a.Label, a.Genres, a.UPC != "", a.Popularity, a.TrackCount, a.CoverURL != "")
		}
	}
	ar, err := p.LookupArtist(ctx, metadata.ArtistQuery{Name: "周杰伦"})
	fmt.Println("artists:", len(ar), err)
	for i, a := range ar {
		if i < 2 {
			fmt.Printf("  %s genres=%v pop=%d followers=%d images=%d\n", a.Name, a.Genres, a.Popularity, a.Followers, len(a.ImageURLs))
		}
	}
	cs, err := p.CoverArt(ctx, metadata.AlbumQuery{Title: "Abbey Road", AlbumArtist: "The Beatles"})
	fmt.Println("covers:", len(cs), err)
	if len(cs) > 0 {
		rc, err := p.FetchCover(ctx, cs[0])
		if err == nil {
			n, _ := io.Copy(io.Discard, rc)
			rc.Close()
			fmt.Println("  fetched cover bytes:", n)
		} else {
			fmt.Println("  fetch:", err)
		}
	}
}
