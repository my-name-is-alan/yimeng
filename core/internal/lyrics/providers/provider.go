// Package providers holds remote lyrics backends. NetEase / QQ Music have no
// documented public metadata API we can call without scraping or private
// endpoints, so they remain stubs. LRCLIB (https://lrclib.net) is a public,
// no-auth lyrics API and is the only remote backend enabled by default.
package providers

import (
	"context"
	"errors"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

var (
	ErrNotFound = errors.New("lyrics provider: not found")
	ErrStub     = errors.New("lyrics provider: not implemented (no safe public API)")
)

// Provider fetches lyrics text for a track.
type Provider interface {
	ID() string
	Fetch(ctx context.Context, t library.Track) (text string, err error)
}

// Chain tries providers in order; first success wins.
type Chain []Provider

func (c Chain) Fetch(ctx context.Context, t library.Track) (string, string, error) {
	var last error
	for _, p := range c {
		text, err := p.Fetch(ctx, t)
		if err == nil && text != "" {
			return text, p.ID(), nil
		}
		if err != nil && !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrStub) {
			last = err
		}
	}
	if last != nil {
		return "", "", last
	}
	return "", "", ErrNotFound
}

// AsFetcher adapts Chain to library.LyricsFetcher.
func (c Chain) AsFetcher() library.LyricsFetcher {
	return func(ctx context.Context, t library.Track) (string, string, error) {
		return c.Fetch(ctx, t)
	}
}
