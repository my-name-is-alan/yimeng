package metadata

import (
	"context"
	"errors"
	"io"
	"sync"
)

// Multi fans a lookup out to several providers (e.g. MusicBrainz + Spotify)
// and concatenates their candidates. Providers that are not configured
// (ErrUnavailable / ErrNotSupported) are skipped silently; the call only
// fails when every provider failed. Results stay suggestions: applying them
// is a local user edit (overrides), never a write by the provider.
type Multi []MetadataProvider

func (m Multi) ID() ProviderID { return "multi" }

func (m Multi) Capabilities() Capability {
	var c Capability
	for _, p := range m {
		c |= p.Capabilities()
	}
	return c
}

func (m Multi) Health(ctx context.Context) HealthState {
	for _, p := range m {
		if p.Health(ctx) == HealthOK {
			return HealthOK
		}
	}
	return HealthDown
}

func skippable(err error) bool {
	return errors.Is(err, ErrUnavailable) || errors.Is(err, ErrNotSupported) || errors.Is(err, ErrNotFound)
}

func fan[T any](ctx context.Context, m Multi, f func(MetadataProvider) ([]T, error)) ([]T, error) {
	res := make([][]T, len(m))
	errs := make([]error, len(m))
	var wg sync.WaitGroup
	for i, p := range m {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res[i], errs[i] = f(p)
		}()
	}
	wg.Wait()
	var out []T
	var first error
	ok := 0
	for i := range m {
		if errs[i] == nil {
			ok++
			out = append(out, res[i]...)
		} else if !skippable(errs[i]) && first == nil {
			first = errs[i]
		}
	}
	if ok == 0 && first != nil {
		return nil, first
	}
	if ok == 0 && len(out) == 0 {
		// everything skipped: unconfigured or nothing found
		for _, e := range errs {
			if errors.Is(e, ErrUnavailable) {
				continue
			}
			return nil, nil
		}
		return nil, ErrUnavailable
	}
	return out, nil
}

// LookupTrack queries every provider.
func (m Multi) LookupTrack(ctx context.Context, q TrackQuery) ([]TrackMatch, error) {
	return fan(ctx, m, func(p MetadataProvider) ([]TrackMatch, error) { return p.LookupTrack(ctx, q) })
}

func (m Multi) LookupAlbum(ctx context.Context, q AlbumQuery) ([]AlbumInfo, error) {
	return fan(ctx, m, func(p MetadataProvider) ([]AlbumInfo, error) { return p.LookupAlbum(ctx, q) })
}

func (m Multi) LookupArtist(ctx context.Context, q ArtistQuery) ([]ArtistInfo, error) {
	return fan(ctx, m, func(p MetadataProvider) ([]ArtistInfo, error) { return p.LookupArtist(ctx, q) })
}

func (m Multi) CoverArt(ctx context.Context, q AlbumQuery) ([]CoverCandidate, error) {
	return fan(ctx, m, func(p MetadataProvider) ([]CoverCandidate, error) { return p.CoverArt(ctx, q) })
}

// FetchCover routes to the provider that produced the candidate.
func (m Multi) FetchCover(ctx context.Context, c CoverCandidate) (io.ReadCloser, error) {
	for _, p := range m {
		if p.ID() == c.Provider {
			return p.FetchCover(ctx, c)
		}
	}
	return nil, ErrNotSupported
}

func (m Multi) Lyrics(ctx context.Context, q TrackQuery) (*Lyrics, error) {
	for _, p := range m {
		if l, err := p.Lyrics(ctx, q); err == nil {
			return l, nil
		}
	}
	return nil, ErrNotSupported
}
