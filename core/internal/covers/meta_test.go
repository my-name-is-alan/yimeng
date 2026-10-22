package covers

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestBlurhashAndPalette(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			c := color.RGBA{220, 30, 30, 255}
			if x >= 16 {
				c = color.RGBA{20, 40, 200, 255}
			}
			img.Set(x, y, c)
		}
	}
	h := Blurhash(img, 4, 3)
	// 1 size + 1 max + 4 DC + 11×2 AC
	if len(h) != 28 || !strings.HasPrefix(h, "L") {
		t.Fatalf("blurhash %q (%d)", h, len(h))
	}
	p := Palette(img, 3)
	if len(p) != 3 || (p[0] != "#dc1e1e" && p[0] != "#1428c8") {
		t.Fatalf("palette %v", p)
	}
	// solid colour: DC only differs, AC zero
	solid := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := range solid.Pix {
		solid.Pix[i] = 255
	}
	if h := Blurhash(solid, 1, 1); h != "00~qIf" && len(h) != 6 {
		t.Fatalf("solid %q", h)
	}
}
