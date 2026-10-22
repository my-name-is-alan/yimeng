package lyrics

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Word-timed lyrics (D46). Native parsers for the platforms' formats:
//   - NetEase YRC:  [lineStart,lineDur](wordStart,wordDur,0)word…   (absolute ms)
//   - Kugou KRC:    "krc1" + XOR(key) + zlib; [lineStart,lineDur]<off,dur,0>word…
//     (offsets relative to the line); translation/roma in a [language:base64(json)] tag.
// Own implementation from the public format descriptions.

// Word is one timed syllable/word.
type Word struct {
	TMs  int64  `json:"t_ms"`
	DMs  int64  `json:"d_ms"`
	Text string `json:"text"`
}

// WordLine is one line with word timing.
type WordLine struct {
	TMs   int64  `json:"t_ms"`
	DMs   int64  `json:"d_ms"`
	Text  string `json:"text"`
	Words []Word `json:"words"`
}

var (
	yrcLine = regexp.MustCompile(`^\[(\d+),(\d+)\](.*)$`)
	yrcWord = regexp.MustCompile(`\((\d+),(\d+),\d+\)([^(]*)`)
	krcWord = regexp.MustCompile(`<(\d+),(\d+),\d+>([^<]*)`)
)

// ParseYRC parses NetEase word-timed lyrics.
func ParseYRC(src string) []WordLine {
	return parseTimed(src, yrcWord, false)
}

// ParseKRCText parses decoded KRC text (word offsets relative to the line).
func ParseKRCText(src string) []WordLine {
	return parseTimed(src, krcWord, true)
}

func parseTimed(src string, wre *regexp.Regexp, relative bool) []WordLine {
	var out []WordLine
	for _, raw := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		m := yrcLine.FindStringSubmatch(strings.TrimSpace(raw))
		if m == nil {
			continue
		}
		t, _ := strconv.ParseInt(m[1], 10, 64)
		d, _ := strconv.ParseInt(m[2], 10, 64)
		l := WordLine{TMs: t, DMs: d, Words: []Word{}}
		var sb strings.Builder
		for _, w := range wre.FindAllStringSubmatch(m[3], -1) {
			wt, _ := strconv.ParseInt(w[1], 10, 64)
			wd, _ := strconv.ParseInt(w[2], 10, 64)
			if relative {
				wt += t
			}
			l.Words = append(l.Words, Word{TMs: wt, DMs: wd, Text: w[3]})
			sb.WriteString(w[3])
		}
		l.Text = strings.TrimSpace(sb.String())
		if len(l.Words) > 0 {
			out = append(out, l)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].TMs < out[j].TMs })
	return out
}

var krcKey = []byte{0x40, 0x47, 0x61, 0x77, 0x5e, 0x32, 0x74, 0x47, 0x51, 0x36, 0x31, 0x2d, 0xce, 0xd2, 0x6e, 0x69}

// DecodeKRC turns a base64 KRC download into text.
func DecodeKRC(b64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", err
	}
	if len(raw) < 5 || string(raw[:4]) != "krc1" {
		return "", errors.New("not a krc file")
	}
	body := raw[4:]
	for i := range body {
		body[i] ^= krcKey[i%len(krcKey)]
	}
	zr, err := zlib.NewReader(bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer zr.Close()
	b, err := io.ReadAll(io.LimitReader(zr, 4<<20))
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(string(b), "\ufeff"), nil
}

var krcLang = regexp.MustCompile(`(?m)^\[language:([A-Za-z0-9+/=]+)\]`)

// KRCExtras returns per-line translation and romanisation from a KRC
// language tag (index-aligned with the timed lines).
func KRCExtras(src string) (trans, roma []string) {
	m := krcLang.FindStringSubmatch(src)
	if m == nil {
		return nil, nil
	}
	b, err := base64.StdEncoding.DecodeString(m[1])
	if err != nil {
		return nil, nil
	}
	var doc struct {
		Content []struct {
			Type         int        `json:"type"`
			LyricContent [][]string `json:"lyricContent"`
		} `json:"content"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return nil, nil
	}
	for _, c := range doc.Content {
		lines := make([]string, len(c.LyricContent))
		for i, ws := range c.LyricContent {
			sep := ""
			if c.Type == 0 {
				sep = " "
			}
			lines[i] = strings.TrimSpace(strings.Join(ws, sep))
		}
		switch c.Type {
		case 1:
			trans = lines
		case 0:
			roma = lines
		}
	}
	return trans, roma
}

// WordsToLRC renders word lines as plain LRC (line timing only).
func WordsToLRC(ls []WordLine) string {
	var sb strings.Builder
	for _, l := range ls {
		ms := l.TMs
		sb.WriteString("[" + pad(ms/60000) + ":" + pad(ms/1000%60) + "." + pad(ms%1000/10) + "]" + l.Text + "\n")
	}
	return sb.String()
}

func pad(n int64) string {
	if n < 10 {
		return "0" + strconv.FormatInt(n, 10)
	}
	return strconv.FormatInt(n, 10)
}

// ── merging ──────────────────────────────────────────────────────────────

// MergedLine is one output line of the merged lyrics API.
type MergedLine struct {
	TMs   int64  `json:"t_ms"`
	Text  string `json:"text"`
	Trans string `json:"trans,omitempty"`
	Roma  string `json:"roma,omitempty"`
	Words []Word `json:"words,omitempty"`
}

// Source is one candidate's lyric material for merging.
type Source struct {
	Provider string
	Lines    []Line     // line-timed text
	Trans    []Line     // translation LRC lines
	Roma     []Line     // romanisation LRC lines
	Words    []WordLine // word timing
}

// MergeResult tells which provider supplied each field.
type MergeResult struct {
	Lines   []MergedLine      `json:"lines"`
	Sources map[string]string `json:"sources"` // text|words|trans|roma → provider
}

// WordPriority orders word-timing sources (Kugou KRC > QQ QRC > NetEase YRC).
var WordPriority = map[string]int{"kugou": 0, "qq": 1, "netease": 2}

const alignMs = 1200

func norm(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Merge builds lines from the base source (best text match, srcs[0]) and
// picks words, translation and romanisation independently: each from the
// highest-priority source that has it, aligned to base lines by time.
func Merge(srcs []Source) MergeResult {
	res := MergeResult{Lines: []MergedLine{}, Sources: map[string]string{}}
	if len(srcs) == 0 {
		return res
	}
	base := srcs[0]
	lines := base.Lines
	if len(lines) == 0 && len(base.Words) > 0 {
		for _, w := range base.Words {
			lines = append(lines, Line{TimeMs: w.TMs, Text: w.Text})
		}
	}
	for _, l := range lines {
		res.Lines = append(res.Lines, MergedLine{TMs: l.TimeMs, Text: l.Text})
	}
	if len(res.Lines) == 0 {
		return res
	}
	res.Sources["text"] = base.Provider

	// words: priority order, then first that aligns with most lines
	ws := append([]Source(nil), srcs...)
	sort.SliceStable(ws, func(i, j int) bool { return prio(ws[i].Provider) < prio(ws[j].Provider) })
	for _, s := range ws {
		if len(s.Words) == 0 {
			continue
		}
		hit := 0
		attach := make([][]Word, len(res.Lines))
		for i, ml := range res.Lines {
			if w, ok := nearestWords(s.Words, ml); ok {
				attach[i] = w
				hit++
			}
		}
		if hit*2 >= len(res.Lines) { // usable only if it covers at least half
			for i := range res.Lines {
				res.Lines[i].Words = attach[i]
			}
			res.Sources["words"] = s.Provider
			break
		}
	}
	pick := func(field string, get func(Source) []Line, set func(*MergedLine, string)) {
		for _, s := range srcs {
			ls := get(s)
			if len(ls) == 0 {
				continue
			}
			hit := 0
			vals := make([]string, len(res.Lines))
			for i, ml := range res.Lines {
				if v := nearestText(ls, ml.TMs); v != "" {
					vals[i] = v
					hit++
				}
			}
			if hit*3 >= len(res.Lines) {
				for i := range res.Lines {
					if vals[i] != "" && norm(vals[i]) != norm(res.Lines[i].Text) {
						set(&res.Lines[i], vals[i])
					}
				}
				res.Sources[field] = s.Provider
				return
			}
		}
	}
	pick("trans", func(s Source) []Line { return s.Trans }, func(m *MergedLine, v string) { m.Trans = v })
	pick("roma", func(s Source) []Line { return s.Roma }, func(m *MergedLine, v string) { m.Roma = v })
	return res
}

func prio(p string) int {
	if v, ok := WordPriority[p]; ok {
		return v
	}
	return 9
}

func nearestText(ls []Line, t int64) string {
	best, bd := "", int64(alignMs+1)
	for _, l := range ls {
		d := l.TimeMs - t
		if d < 0 {
			d = -d
		}
		if d < bd && strings.TrimSpace(l.Text) != "" {
			best, bd = strings.TrimSpace(l.Text), d
		}
	}
	return best
}

func nearestWords(ws []WordLine, ml MergedLine) ([]Word, bool) {
	nt := norm(ml.Text)
	for _, w := range ws {
		d := w.TMs - ml.TMs
		if d < 0 {
			d = -d
		}
		if d <= alignMs && (nt == "" || norm(w.Text) == nt) {
			// re-time to the base line: keep the source's word spacing
			shift := ml.TMs - w.TMs
			out := make([]Word, len(w.Words))
			for i, x := range w.Words {
				out[i] = Word{TMs: x.TMs + shift, DMs: x.DMs, Text: x.Text}
			}
			return out, true
		}
	}
	return nil, false
}
