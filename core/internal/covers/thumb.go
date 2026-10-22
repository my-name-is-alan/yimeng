// Package covers resizes cover art into a sized on-disk cache (pure Go).
package covers

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/chai2010/webp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	MaxSize     = 2048
	DefaultSize = 300
)

var allowed = map[string]bool{"jpeg": true, "jpg": true, "webp": true, "png": true}

type Cache struct {
	Dir string
	mu  sync.Mutex
}

// Thumb returns a resized cover. size is the long edge in pixels (1..2048);
// format is jpeg|webp|png (default jpeg).
func (c *Cache) Thumb(src string, size int, format string) (path, contentType string, err error) {
	if size <= 0 {
		size = DefaultSize
	}
	if size > MaxSize {
		size = MaxSize
	}
	format = strings.ToLower(format)
	if format == "" || format == "jpg" {
		format = "jpeg"
	}
	if !allowed[format] {
		return "", "", fmt.Errorf("format must be jpeg|webp|png")
	}
	st, err := os.Stat(src)
	if err != nil {
		return "", "", err
	}
	key := hashKey(src, st.ModTime().UnixNano(), st.Size(), size, format)
	dir := filepath.Join(c.Dir, key[:2])
	out := filepath.Join(dir, key+"."+fileExt(format))
	if _, err := os.Stat(out); err == nil {
		return out, mimeOf(format), nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := os.Stat(out); err == nil {
		return out, mimeOf(format), nil
	}
	img, err := decodeFile(src)
	if err != nil {
		return "", "", err
	}
	img = resize(img, size)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	tmp := out + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", "", err
	}
	if err := encodeImg(f, img, format); err != nil {
		f.Close()
		os.Remove(tmp)
		return "", "", err
	}
	f.Close()
	if err := os.Rename(tmp, out); err != nil {
		os.Remove(tmp)
		return "", "", err
	}
	return out, mimeOf(format), nil
}

func hashKey(src string, mtime, size int64, px int, format string) string {
	h := sha1.Sum([]byte(fmt.Sprintf("%s\x00%d\x00%d\x00%d\x00%s", src, mtime, size, px, format)))
	return hex.EncodeToString(h[:16])
}

func fileExt(f string) string {
	if f == "jpeg" {
		return "jpg"
	}
	return f
}

func mimeOf(f string) string {
	switch f {
	case "webp":
		return "image/webp"
	case "png":
		return "image/png"
	default:
		return "image/jpeg"
	}
}

func decodeFile(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(io.LimitReader(f, 32<<20))
	return img, err
}

func resize(src image.Image, longEdge int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= longEdge && h <= longEdge {
		return src
	}
	var nw, nh int
	if w >= h {
		nw, nh = longEdge, max(1, h*longEdge/w)
	} else {
		nh, nw = longEdge, max(1, w*longEdge/h)
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	return dst
}

func encodeImg(w io.Writer, img image.Image, format string) error {
	switch format {
	case "png":
		return png.Encode(w, img)
	case "webp":
		return webp.Encode(w, img, &webp.Options{Quality: 80})
	default:
		return jpeg.Encode(w, img, &jpeg.Options{Quality: 85})
	}
}

// ParseSize reads a size query value.
func ParseSize(s string) (int, error) {
	if s == "" {
		return DefaultSize, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("size must be a positive integer")
	}
	return n, nil
}
