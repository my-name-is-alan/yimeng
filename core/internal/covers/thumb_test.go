package covers

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestThumb(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "c.png")
	f, _ := os.Create(src)
	img := image.NewRGBA(image.Rect(0, 0, 400, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 400; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 80, 255})
		}
	}
	png.Encode(f, img)
	f.Close()
	c := &Cache{Dir: filepath.Join(dir, "thumbs")}
	p1, ct, err := c.Thumb(src, 100, "jpeg")
	if err != nil || ct != "image/jpeg" {
		t.Fatal(err, ct)
	}
	p2, _, err := c.Thumb(src, 100, "jpeg")
	if err != nil || p1 != p2 {
		t.Fatal("cache miss")
	}
	p3, ct, err := c.Thumb(src, 100, "webp")
	if err != nil || ct != "image/webp" || p3 == p1 {
		t.Fatal(err, ct)
	}
}
