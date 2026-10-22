// Package search builds searchable text (lowercase + pinyin + initials) for
// Chinese-friendly library queries.
package search

import (
	"strings"
	"unicode"

	"github.com/mozillazg/go-pinyin"
)

var args = pinyin.NewArgs()

func init() {
	args.Style = pinyin.Normal
	args.Heteronym = false
}

// Text returns lowercased original plus full pinyin and first-letter initials.
func Text(parts ...string) string {
	var b strings.Builder
	seen := map[string]struct{}{}
	add := func(s string) {
		s = strings.TrimSpace(strings.ToLower(s))
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(s)
	}
	for _, p := range parts {
		add(p)
		py, ini := pinyinOf(p)
		add(py)
		add(ini)
	}
	return b.String()
}

func pinyinOf(s string) (full, initials string) {
	var words, letters []string
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			syll := pinyin.SinglePinyin(r, args)
			if len(syll) == 0 || syll[0] == "" {
				continue
			}
			words = append(words, syll[0])
			letters = append(letters, syll[0][:1])
		}
	}
	return strings.Join(words, ""), strings.Join(letters, "")
}

// ExpandQuery returns the original query plus pinyin forms so Latin input can
// match Chinese titles stored with pinyin in search_text.
func ExpandQuery(q string) []string {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil
	}
	out := []string{q}
	py, ini := pinyinOf(q)
	if py != "" && !strings.EqualFold(py, q) {
		out = append(out, py)
	}
	if ini != "" && ini != py {
		out = append(out, ini)
	}
	// Also treat ASCII query as-is (already in out[0]).
	return uniq(out)
}

func uniq(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
