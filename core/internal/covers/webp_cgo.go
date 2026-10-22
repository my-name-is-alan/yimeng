//go:build cgo

package covers

import (
	"image"
	"io"

	"github.com/chai2010/webp"
)

const webpEncoder = true

func encodeWebP(w io.Writer, img image.Image) error {
	return webp.Encode(w, img, &webp.Options{Quality: 80})
}
