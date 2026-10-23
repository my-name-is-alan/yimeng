package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// SplitConfig is the user-editable artist splitting policy, stored in
// settings under SettingArtistSplit.
type SplitConfig struct {
	// Exceptions are names never split, matched case-insensitively anywhere
	// in the tag (e.g. "AC/DC", "Simon & Garfunkel").
	Exceptions []string `json:"exceptions"`
	// SplitAmpersand / SplitComma enable "&" and "," as separators (off by default).
	SplitAmpersand bool `json:"split_ampersand"`
	SplitComma     bool `json:"split_comma"`
	// ExtraSeparators are additional literal separators, e.g. " x ", " vs. ", "＆".
	ExtraSeparators []string `json:"extra_separators"`
}

const SettingArtistSplit = "library.artist_split"

func DefaultSplitConfig() SplitConfig {
	return SplitConfig{Exceptions: []string{"AC/DC"}, ExtraSeparators: []string{}}
}

var ErrInvalidSettings = errors.New("invalid settings")

func (c *SplitConfig) Validate() error {
	if len(c.Exceptions) > 1000 || len(c.ExtraSeparators) > 20 {
		return fmt.Errorf("%w: too many entries", ErrInvalidSettings)
	}
	clean := func(in []string, max int, trim bool) ([]string, error) {
		out := []string{}
		seen := map[string]bool{}
		for _, s := range in {
			if trim {
				s = strings.TrimSpace(s)
			}
			if strings.TrimSpace(s) == "" {
				continue
			}
			if len(s) > max || strings.ContainsRune(s, 0) {
				return nil, fmt.Errorf("%w: entry %q too long", ErrInvalidSettings, s)
			}
			if k := strings.ToLower(s); !seen[k] {
				seen[k] = true
				out = append(out, s)
			}
		}
		return out, nil
	}
	var err error
	if c.Exceptions, err = clean(c.Exceptions, 200, true); err != nil {
		return err
	}
	c.ExtraSeparators, err = clean(c.ExtraSeparators, 8, false) // keep surrounding spaces: " x "
	return err
}

// Splitter splits artist tag values according to a SplitConfig.
type Splitter struct {
	cfg   SplitConfig
	sep   *regexp.Regexp
	exRes []*regexp.Regexp
}

var featRe = regexp.MustCompile(`(?i)\s*[\(\[（]?\s*\b(?:feat\.?|ft\.?|featuring)\s+`)

func NewSplitter(c SplitConfig) *Splitter {
	alts := []string{`;`, `；`, `、`, `／`, `/`}
	if c.SplitAmpersand {
		alts = append(alts, `&`, `＆`)
	}
	if c.SplitComma {
		alts = append(alts, `,`, `，`)
	}
	for _, e := range c.ExtraSeparators {
		alts = append(alts, regexp.QuoteMeta(e))
	}
	s := &Splitter{cfg: c, sep: regexp.MustCompile(`\s*(?:` + strings.Join(alts, "|") + `)\s*`)}
	for _, e := range c.Exceptions {
		s.exRes = append(s.exRes, regexp.MustCompile(`(?i)`+regexp.QuoteMeta(e)))
	}
	return s
}

var defaultSplitter = NewSplitter(DefaultSplitConfig())

// SplitArtists uses the default policy.
func SplitArtists(s string) []ArtistCredit { return defaultSplitter.Split(s) }

// Split splits a tag value into individual artists; text after
// "feat." / "ft." / "featuring" is credited with role "feat".
func (sp *Splitter) Split(s string) []ArtistCredit {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	// Protect exceptions with placeholders so separators inside them survive.
	var saved []string
	for _, re := range sp.exRes {
		s = re.ReplaceAllStringFunc(s, func(m string) string {
			saved = append(saved, m)
			return fmt.Sprintf("\x00%d\x00", len(saved)-1)
		})
	}
	restore := func(x string) string {
		for i, v := range saved {
			x = strings.ReplaceAll(x, fmt.Sprintf("\x00%d\x00", i), v)
		}
		return x
	}
	main, feat := s, ""
	if loc := featRe.FindStringIndex(s); loc != nil && loc[0] > 0 {
		main, feat = s[:loc[0]], s[loc[1]:]
		feat = strings.TrimRight(strings.TrimSpace(feat), ")]）")
	}
	var out []ArtistCredit
	seen := map[string]bool{}
	add := func(part, role string) {
		for _, p := range sp.sep.Split(part, -1) {
			n := strings.TrimSpace(restore(p))
			k := normKey(n)
			if k == "" || seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, ArtistCredit{Name: n, Role: role})
		}
	}
	add(main, "primary")
	add(feat, "feat")
	return out
}

// SplitConfig returns the active policy.
func (s *Scanner) SplitConfig() SplitConfig { return s.splitter().cfg }

func (s *Scanner) splitter() *Splitter {
	if sp := s.split.Load(); sp != nil {
		return sp
	}
	return defaultSplitter
}

// LoadSettings reads persisted settings (call once at startup).
func (s *Scanner) LoadSettings(ctx context.Context) error {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, SettingArtistSplit).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		s.split.Store(defaultSplitter)
		return nil
	}
	if err != nil {
		return err
	}
	c := DefaultSplitConfig()
	if err := json.Unmarshal([]byte(v), &c); err != nil {
		return fmt.Errorf("settings %s: %w", SettingArtistSplit, err)
	}
	if err := c.Validate(); err != nil {
		return err
	}
	s.split.Store(NewSplitter(c))
	var rootsJSON string
	err = s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='library.roots'`).Scan(&rootsJSON)
	if err == nil {
		var roots []string
		if json.Unmarshal([]byte(rootsJSON), &roots) == nil && len(roots) > 0 {
			s.opts.Roots = roots
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return nil
}

// SetSplitConfig validates, persists and applies a new policy, then re-merges
// the canonical layer so the change is visible immediately.
func (s *Scanner) SetSplitConfig(ctx context.Context, c SplitConfig) (SplitConfig, error) {
	if c.Exceptions == nil {
		c.Exceptions = []string{}
	}
	if c.ExtraSeparators == nil {
		c.ExtraSeparators = []string{}
	}
	if err := c.Validate(); err != nil {
		return c, err
	}
	b, _ := json.Marshal(c)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES (?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, SettingArtistSplit, string(b)); err != nil {
		return c, err
	}
	s.split.Store(NewSplitter(c))
	return c, s.Merge(ctx)
}
