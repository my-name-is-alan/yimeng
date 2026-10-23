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
	wantType, wantFormat := "image/jpeg", "jpeg"
	if WebPEncodingAvailable() {
		wantType, wantFormat = "image/webp", "webp"
	}
	if err != nil || ct != wantType || (p3 == p1) == WebPEncodingAvailable() {
		t.Fatal(err, ct)
	}
	f, err = os.Open(p3)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	decoded, format, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if format != wantFormat || decoded.Bounds().Dx() != 100 || decoded.Bounds().Dy() != 50 {
		t.Fatalf("encoded thumbnail: format=%s bounds=%v", format, decoded.Bounds())
	}
}
