package metadata

import (
	"context"
	"io"
	"testing"
)

type stubProv struct{ id ProviderID }

func (s stubProv) ID() ProviderID                     { return s.id }
func (s stubProv) Capabilities() Capability           { return CapLyrics }
func (s stubProv) Health(context.Context) HealthState { return HealthOK }
func (s stubProv) LookupTrack(context.Context, TrackQuery) ([]TrackMatch, error) {
	return nil, ErrNotSupported
}
func (s stubProv) LookupAlbum(context.Context, AlbumQuery) ([]AlbumInfo, error) {
	return nil, ErrNotSupported
}
func (s stubProv) LookupArtist(context.Context, ArtistQuery) ([]ArtistInfo, error) {
	return nil, ErrNotSupported
}
func (s stubProv) CoverArt(context.Context, AlbumQuery) ([]CoverCandidate, error) {
	return nil, ErrNotSupported
}
func (s stubProv) FetchCover(context.Context, CoverCandidate) (io.ReadCloser, error) {
	return nil, ErrNotSupported
}
func (s stubProv) Lyrics(context.Context, TrackQuery) (*Lyrics, error) { return nil, ErrNotSupported }

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	r.RegisterFactory("stub", func(Deps) (MetadataProvider, error) { return stubProv{id: "stub"}, nil })
	p, err := r.Enable("stub", Deps{})
	if err != nil || p.ID() != "stub" {
		t.Fatal(p, err)
	}
	p2, err := r.Enable("stub", Deps{})
	if err != nil || p2 != p {
		t.Fatal("not idempotent")
	}
	if _, err := r.Enable("missing", Deps{}); err == nil {
		t.Fatal("expected error")
	}
	if len(r.All()) != 1 || len(r.IDs()) != 1 {
		t.Fatal(r.All(), r.IDs())
	}
	r.Put(stubProv{id: "other"})
	if _, ok := r.Get("other"); !ok {
		t.Fatal("get")
	}
}
