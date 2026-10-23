package local

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dhowden/tag"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// fixLegacyCharset repairs ID3 text that was written in a legacy Chinese
// codepage (GBK / GB18030) but flagged as ISO-8859-1 — very common for MP3s
// tagged by older Chinese software. dhowden/tag maps each Latin-1 byte to one
// rune (U+0000..U+00FF), so the original bytes can be recovered losslessly.
//
// Heuristic (conservative; real Latin-1 like "Café" or "Motörhead" survives):
//  1. every rune is <= U+00FF and at least one is >= 0x80 (else nothing to fix);
//  2. the recovered bytes are valid UTF-8 with a non-Latin-1 rune -> it was
//     mis-flagged UTF-8, use it;
//  3. the double-byte pairs look like GB text: pairs whose trail byte is
//     ASCII (0x40..0x7E, legal in GBK but rare in real Chinese) must be
//     outnumbered 2:1 by GB2312-range pairs; this rejects "Motörhead";
//  4. otherwise decode as GB18030 (a superset of GBK/GB2312); accept only if
//     the result has no U+FFFD, contains at least one CJK ideograph, and every
//     non-ASCII rune is CJK / CJK punctuation / fullwidth form.
func fixLegacyCharset(s string) string {
	b, ok := latin1Bytes(s)
	if !ok {
		return s
	}
	if utf8.Valid(b) {
		u := string(b)
		for _, r := range u {
			if r > 0xFF {
				return u
			}
		}
	}
	if !plausibleGB(b) {
		return s
	}
	dec, err := simplifiedchinese.GB18030.NewDecoder().Bytes(b)
	if err != nil || !utf8.Valid(dec) {
		return s
	}
	out := string(dec)
	hasHan := false
	for _, r := range out {
		switch {
		case r == utf8.RuneError:
			return s
		case r < 0x80:
		case unicode.Is(unicode.Han, r):
			hasHan = true
		case isCJKAux(r):
		default:
			return s
		}
	}
	if !hasHan {
		return s
	}
	return out
}

// latin1Bytes converts s back to bytes if every rune is in U+0000..U+00FF and
// at least one is non-ASCII.
// ID3v1 text comes back from dhowden/tag as raw (invalid UTF-8) bytes; use those directly.
func latin1Bytes(s string) ([]byte, bool) {
	if !utf8.ValidString(s) {
		return []byte(s), true
	}
	high := false
	b := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xFF {
			return nil, false
		}
		if r >= 0x80 {
			high = true
		}
		b = append(b, byte(r))
	}
	return b, high
}

func isCJKAux(r rune) bool {
	return (r >= 0x3000 && r <= 0x303F) || // CJK symbols & punctuation
		(r >= 0xFF00 && r <= 0xFFEF) || // fullwidth forms
		(r >= 0x3040 && r <= 0x30FF) || // kana (GBK includes them)
		(r >= 0x2010 && r <= 0x2027) || // dashes, quotes, ellipsis
		r == 0x00B7 || r == 0x00D7 || // middle dot, multiplication
		(r >= 0x2160 && r <= 0x217F) || // roman numerals
		(r >= 0x2460 && r <= 0x24FF) // circled numbers
}

func isID3(f tag.Format) bool {
	return f == tag.ID3v1 || f == tag.ID3v2_2 || f == tag.ID3v2_3 || f == tag.ID3v2_4
}

func fixTrackCharset(t *Track) {
	for _, p := range []*string{&t.Title, &t.Artist, &t.Album, &t.AlbumArtist, &t.Composer, &t.Genre} {
		*p = strings.TrimSpace(fixLegacyCharset(*p))
	}
}

// plausibleGB walks b as GBK double-byte sequences and scores them.
func plausibleGB(b []byte) bool {
	good, bad := 0, 0
	for i := 0; i < len(b); i++ {
		c := b[i]
		if c < 0x80 {
			continue
		}
		if c == 0x80 || c == 0xFF || i+1 >= len(b) {
			return false
		}
		t := b[i+1]
		switch {
		case c >= 0xA1 && c <= 0xF7 && t >= 0xA1 && t <= 0xFE:
			good++
		case t >= 0x40 && t <= 0x7E:
			bad++
		case t >= 0x80 && t != 0xFF:
			good++ // GBK extension area (e.g. 喆, 堃)
		default:
			return false
		}
		i++
	}
	return good > 0 && good >= 2*bad
}
