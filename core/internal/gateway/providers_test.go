package gateway

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/metadata"
)

type fakeMP struct{}

func (fakeMP) ID() metadata.ProviderID                     { return "musicbrainz" }
func (fakeMP) Capabilities() metadata.Capability           { return metadata.CapTrackTags }
func (fakeMP) Health(context.Context) metadata.HealthState { return metadata.HealthOK }
func (fakeMP) LookupTrack(context.Context, metadata.TrackQuery) ([]metadata.TrackMatch, error) {
	return nil, metadata.ErrNotSupported
}
func (fakeMP) LookupAlbum(context.Context, metadata.AlbumQuery) ([]metadata.AlbumInfo, error) {
	return nil, metadata.ErrNotSupported
}
func (fakeMP) LookupArtist(context.Context, metadata.ArtistQuery) ([]metadata.ArtistInfo, error) {
	return nil, metadata.ErrNotSupported
}
func (fakeMP) CoverArt(context.Context, metadata.AlbumQuery) ([]metadata.CoverCandidate, error) {
	return nil, metadata.ErrNotSupported
}
func (fakeMP) FetchCover(context.Context, metadata.CoverCandidate) (io.ReadCloser, error) {
	return nil, metadata.ErrNotSupported
}
func (fakeMP) Lyrics(context.Context, metadata.TrackQuery) (*metadata.Lyrics, error) {
	return nil, metadata.ErrNotSupported
}

func TestListProviders(t *testing.T) {
	e := newEnv(t)
	reg := metadata.NewRegistry()
	reg.Put(fakeMP{})
	// rebuild router with Providers — newEnv already started; hit empty first
	res, body := e.do("GET", "/api/metadata/providers", "", e.token)
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode, string(body))
	}
	var out struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	// default env has nil Providers → empty list
	if len(out.Items) != 0 {
		t.Fatalf("want empty, got %v", out.Items)
	}
}
