package providers

import (
	"context"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

// NetEase and QQ are stubs: there is no official public lyrics/metadata API
// that can be used without reverse-engineering private mobile endpoints or
// scraping. Keeping the SPI ready for a future licensed integration.
type NetEase struct{}

func (NetEase) ID() string { return "netease" }
func (NetEase) Fetch(context.Context, library.Track) (string, error) {
	return "", ErrStub
}

type QQ struct{}

func (QQ) ID() string                                           { return "qq" }
func (QQ) Fetch(context.Context, library.Track) (string, error) { return "", ErrStub }
