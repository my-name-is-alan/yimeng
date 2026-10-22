//go:build !cgo

package covers

import (
	"errors"
	"image"
	"io"
)

// Pure-Go builds (CGO_ENABLED=0: Docker/NAS) serve JPEG when WebP is requested.
const webpEncoder = false

func encodeWebP(io.Writer, image.Image) error { return errors.New("webp encoding needs cgo") }
