package covers

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/my-name-is-alan/yimeng/core/internal/infra/safehttp"
	"image"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/image/draw"
)

// Meta is the placeholder data apps show before the image loads.
type Meta struct {
	Blurhash string   `json:"blurhash"`
	Colors   []string `json:"colors"` // 3 × "#rrggbb", most dominant first
}

// MetaCached returns the meta for src if it was computed before.
func (c *Cache) MetaCached(src string) (Meta, bool) {
	var m Meta
	p, err := c.metaPath(src)
	if err != nil {
		return m, false
	}
	b, err := os.ReadFile(p)
	if err != nil || json.Unmarshal(b, &m) != nil {
		return m, false
	}
	return m, true
}

// Meta computes (or reads) blurhash + dominant colours for src.
func (c *Cache) Meta(src string) (Meta, error) {
	if m, ok := c.MetaCached(src); ok {
		return m, nil
	}
	p, err := c.metaPath(src)
	if err != nil {
		return Meta{}, err
	}
	img, err := decodeFile(src)
	if err != nil {
		return Meta{}, err
	}
	small := image.NewRGBA(image.Rect(0, 0, 32, 32))
	draw.ApproxBiLinear.Scale(small, small.Bounds(), img, img.Bounds(), draw.Src, nil)
	m := Meta{Blurhash: Blurhash(small, 4, 3), Colors: Palette(small, 3)}
	os.MkdirAll(filepath.Dir(p), 0o755)
	if b, err := json.Marshal(m); err == nil {
		os.WriteFile(p+".tmp", b, 0o644)
		os.Rename(p+".tmp", p)
	}
	return m, nil
}

func (c *Cache) metaPath(src string) (string, error) {
	st, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	key := hashKey(src, st.ModTime().UnixNano(), st.Size(), 0, "meta")
	return filepath.Join(c.Dir, "meta", key[:2], key+".json"), nil
}

// ETag returns a strong validator for a (thumb) file.
func ETag(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return ""
	}
	h := sha1.Sum([]byte(fmt.Sprintf("%s\x00%d\x00%d", path, st.ModTime().UnixNano(), st.Size())))
	return `"` + hex.EncodeToString(h[:10]) + `"`
}

// ErrRemote is returned when a remote cover cannot be fetched.
var ErrRemote = errors.New("remote cover unavailable")

var remoteClient = &http.Client{Timeout: 15 * time.Second, Transport: safehttp.Transport()}

// ── blurhash (https://blurha.sh algorithm, own implementation) ───────────

const b83 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz#$%*+,-.:;=?@[]^_{|}~"

func enc83(v, n int) string {
	out := make([]byte, n)
	for i := 1; i <= n; i++ {
		d := (v / int(math.Pow(83, float64(n-i)))) % 83
		out[i-1] = b83[d]
	}
	return string(out)
}

func toLinear(v uint8) float64 {
	f := float64(v) / 255
	if f <= 0.04045 {
		return f / 12.92
	}
	return math.Pow((f+0.055)/1.055, 2.4)
}

func toSRGB(v float64) int {
	v = math.Max(0, math.Min(1, v))
	if v <= 0.0031308 {
		return int(v*12.92*255 + 0.5)
	}
	return int((1.055*math.Pow(v, 1/2.4)-0.055)*255 + 0.5)
}

func signPow(v, e float64) float64 { return math.Copysign(math.Pow(math.Abs(v), e), v) }

// Blurhash encodes img with cx × cy components.
func Blurhash(img image.Image, cx, cy int) string {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return ""
	}
	lin := make([][3]float64, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			lin[y*w+x] = [3]float64{toLinear(uint8(r >> 8)), toLinear(uint8(g >> 8)), toLinear(uint8(bl >> 8))}
		}
	}
	factors := make([][3]float64, 0, cx*cy)
	for j := 0; j < cy; j++ {
		for i := 0; i < cx; i++ {
			norm := 2.0
			if i == 0 && j == 0 {
				norm = 1
			}
			var f [3]float64
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					basis := math.Cos(math.Pi*float64(i)*float64(x)/float64(w)) * math.Cos(math.Pi*float64(j)*float64(y)/float64(h))
					p := lin[y*w+x]
					f[0] += basis * p[0]
					f[1] += basis * p[1]
					f[2] += basis * p[2]
				}
			}
			s := norm / float64(w*h)
			factors = append(factors, [3]float64{f[0] * s, f[1] * s, f[2] * s})
		}
	}
	var sb strings.Builder
	sb.WriteString(enc83((cx-1)+(cy-1)*9, 1))
	maxV := 1.0
	ac := factors[1:]
	if len(ac) > 0 {
		actual := 0.0
		for _, f := range ac {
			actual = math.Max(actual, math.Max(math.Abs(f[0]), math.Max(math.Abs(f[1]), math.Abs(f[2]))))
		}
		q := int(math.Max(0, math.Min(82, math.Floor(actual*166-0.5))))
		maxV = float64(q+1) / 166
		sb.WriteString(enc83(q, 1))
	} else {
		sb.WriteString(enc83(0, 1))
	}
	dc := factors[0]
	sb.WriteString(enc83(toSRGB(dc[0])<<16+toSRGB(dc[1])<<8+toSRGB(dc[2]), 4))
	for _, f := range ac {
		q := func(v float64) int { return int(math.Max(0, math.Min(18, math.Floor(signPow(v/maxV, 0.5)*9+9.5)))) }
		sb.WriteString(enc83(q(f[0])*361+q(f[1])*19+q(f[2]), 2))
	}
	return sb.String()
}

// Palette returns n dominant, mutually distinct colours (#rrggbb).
func Palette(img image.Image, n int) []string {
	type bucket struct {
		r, g, b, n float64
	}
	bk := map[int]*bucket{}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := img.At(x, y).RGBA()
			if a < 0x8000 {
				continue
			}
			r8, g8, b8 := int(r>>8), int(g>>8), int(bl>>8)
			k := (r8>>4)<<8 | (g8>>4)<<4 | b8>>4
			e := bk[k]
			if e == nil {
				e = &bucket{}
				bk[k] = e
			}
			e.r += float64(r8)
			e.g += float64(g8)
			e.b += float64(b8)
			e.n++
		}
	}
	list := make([]*bucket, 0, len(bk))
	for _, e := range bk {
		list = append(list, e)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].n > list[j].n })
	var picked [][3]float64
	for _, e := range list {
		c := [3]float64{e.r / e.n, e.g / e.n, e.b / e.n}
		ok := true
		for _, p := range picked {
			if math.Abs(p[0]-c[0])+math.Abs(p[1]-c[1])+math.Abs(p[2]-c[2]) < 90 {
				ok = false
				break
			}
		}
		if ok {
			picked = append(picked, c)
			if len(picked) == n {
				break
			}
		}
	}
	if len(picked) == 0 {
		picked = append(picked, [3]float64{40, 40, 40})
	}
	for len(picked) < n { // pad with shades of the dominant colour
		base := picked[0]
		k := 0.55
		if len(picked) == 2 {
			k = 1.35
		}
		picked = append(picked, [3]float64{math.Min(255, base[0]*k), math.Min(255, base[1]*k), math.Min(255, base[2]*k)})
	}
	out := make([]string, n)
	for i, c := range picked[:n] {
		out[i] = fmt.Sprintf("#%02x%02x%02x", int(c[0]+0.5), int(c[1]+0.5), int(c[2]+0.5))
	}
	return out
}

// RemoteCached returns the disk copy of a remote cover if already fetched.
func (c *Cache) RemoteCached(u string) (string, bool) {
	u = normURL(u)
	out := c.remotePath(Key(u))
	_, err := os.Stat(out)
	return out, err == nil
}
