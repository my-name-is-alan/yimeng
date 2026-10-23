// Package lyrics parses LRC (synced) and plain lyrics.
package lyrics

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

type Line struct {
	TimeMs int64  `json:"time_ms"`
	Text   string `json:"text"`
}

type Lyrics struct {
	Synced   bool              `json:"synced"`
	OffsetMs int64             `json:"offset_ms,omitempty"` // already applied to Lines
	Lines    []Line            `json:"lines"`               // synced: sorted by time; plain: time 0
	Text     string            `json:"text"`                // plain text (timestamps stripped)
	Meta     map[string]string `json:"meta,omitempty"`      // ar, ti, al, by, ...
}

var (
	timeTag = regexp.MustCompile(`^\[(\d{1,3}):(\d{1,2})(?:[.:](\d{1,3}))?\]`)
	metaTag = regexp.MustCompile(`^\[([a-zA-Z#]+):([^\]]*)\]\s*$`)
	wordTag = regexp.MustCompile(`<\d{1,3}:\d{1,2}(?:[.:]\d{1,3})?>`) // enhanced LRC word timing
)

// Decode returns UTF-8 text: strips a BOM, and treats invalid UTF-8 as GB18030
// (common for Chinese .lrc files).
func Decode(b []byte) string {
	b = []byte(strings.TrimPrefix(string(b), "\ufeff"))
	if utf8.Valid(b) {
		return string(b)
	}
	if out, err := simplifiedchinese.GB18030.NewDecoder().Bytes(b); err == nil {
		return string(out)
	}
	return strings.ToValidUTF8(string(b), "")
}

func parseTime(m []string) int64 {
	min, _ := strconv.ParseInt(m[1], 10, 64)
	sec, _ := strconv.ParseInt(m[2], 10, 64)
	var frac int64
	if m[3] != "" {
		f := m[3]
		frac, _ = strconv.ParseInt(f, 10, 64)
		switch len(f) {
		case 1:
			frac *= 100
		case 2:
			frac *= 10
		}
	}
	return (min*60+sec)*1000 + frac
}

// Parse parses LRC; text without any time tags is returned as plain lyrics.
func Parse(src string) Lyrics {
	l := Lyrics{Lines: []Line{}, Meta: map[string]string{}}
	var plain []string
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "\n")
	for _, raw := range strings.Split(src, "\n") {
		line := strings.TrimSpace(raw)
		var times []int64
		for {
			m := timeTag.FindStringSubmatch(line)
			if m == nil {
				break
			}
			times = append(times, parseTime(m))
			line = strings.TrimSpace(line[len(m[0]):])
		}
		if len(times) == 0 {
			if m := metaTag.FindStringSubmatch(line); m != nil {
				k := strings.ToLower(m[1])
				v := strings.TrimSpace(m[2])
				if k == "offset" {
					l.OffsetMs, _ = strconv.ParseInt(strings.TrimPrefix(v, "+"), 10, 64)
				} else if v != "" {
					l.Meta[k] = v
				}
				continue
			}
			plain = append(plain, raw)
			continue
		}
		text := strings.TrimSpace(wordTag.ReplaceAllString(line, ""))
		for _, t := range times {
			l.Lines = append(l.Lines, Line{TimeMs: t, Text: text})
		}
		plain = append(plain, text)
	}
	if len(l.Lines) > 0 {
		l.Synced = true
		// LRC offset: positive = lyrics appear sooner
		for i := range l.Lines {
			l.Lines[i].TimeMs = max(l.Lines[i].TimeMs-l.OffsetMs, 0)
		}
		slices.SortStableFunc(l.Lines, func(a, b Line) int { return int(a.TimeMs - b.TimeMs) })
		var tx []string
		for _, ln := range l.Lines {
			tx = append(tx, ln.Text)
		}
		l.Text = strings.Join(tx, "\n")
	} else {
		l.Text = strings.TrimSpace(strings.Join(plain, "\n"))
		for _, p := range strings.Split(l.Text, "\n") {
			l.Lines = append(l.Lines, Line{Text: p})
		}
		l.OffsetMs = 0
	}
	if len(l.Meta) == 0 {
		l.Meta = nil
	}
	return l
}
