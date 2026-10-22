// Package runtimecfg holds the server settings that the web console can
// change at runtime (persisted in the settings table, applied without a
// restart): file watching, the periodic rescan interval, MusicBrainz
// enrichment (enable + contact) and the background metadata fill sources.
package runtimecfg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
	"sync"
	"time"
)

const key = "server.runtime"

var ErrInvalid = errors.New("invalid settings")

// FillSourceIDs are the selectable background-fill sources (in default order).
var FillSourceIDs = []string{"spotify", "netease", "qq", "kugou", "musicbrainz"}

// Settings is the editable set. JSON durations are strings ("1h", "0").
type Settings struct {
	WatchMode          string   `json:"watch_mode"`      // auto | fsnotify | poll | off
	RescanInterval     string   `json:"rescan_interval"` // Go duration, "0" = never
	MusicBrainzEnabled bool     `json:"musicbrainz_enabled"`
	MusicBrainzContact string   `json:"musicbrainz_contact"`
	FillSources        []string `json:"fill_sources"`    // ordered; empty = background fill off
	FillPerMinute      int      `json:"fill_per_minute"` // 1..120
}

func (s Settings) Rescan() time.Duration {
	d, _ := time.ParseDuration(s.RescanInterval)
	return d
}

// Validate normalises and checks s.
func (s *Settings) Validate() error {
	s.WatchMode = strings.ToLower(strings.TrimSpace(s.WatchMode))
	switch s.WatchMode {
	case "auto", "fsnotify", "poll", "off":
	default:
		return fmt.Errorf("%w: watch_mode must be auto, fsnotify, poll or off", ErrInvalid)
	}
	if strings.TrimSpace(s.RescanInterval) == "" || s.RescanInterval == "0" {
		s.RescanInterval = "0s"
	}
	d, err := time.ParseDuration(s.RescanInterval)
	if err != nil || d < 0 || (d > 0 && d < time.Minute) || d > 7*24*time.Hour {
		return fmt.Errorf("%w: rescan_interval must be 0 or between 1m and 168h", ErrInvalid)
	}
	s.RescanInterval = d.String()
	s.MusicBrainzContact = strings.TrimSpace(s.MusicBrainzContact)
	if s.MusicBrainzContact != "" {
		_, e1 := mail.ParseAddress(s.MusicBrainzContact)
		u, e2 := url.Parse(s.MusicBrainzContact)
		if len(s.MusicBrainzContact) > 200 || (e1 != nil && (e2 != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "")) {
			return fmt.Errorf("%w: musicbrainz_contact must be an email address or http(s) URL", ErrInvalid)
		}
	}
	if s.MusicBrainzEnabled && s.MusicBrainzContact == "" {
		return fmt.Errorf("%w: MusicBrainz needs a contact (email or URL) for its User-Agent policy", ErrInvalid)
	}
	seen := map[string]bool{}
	var src []string
	for _, id := range s.FillSources {
		id = strings.ToLower(strings.TrimSpace(id))
		ok := false
		for _, k := range FillSourceIDs {
			ok = ok || k == id
		}
		if !ok {
			return fmt.Errorf("%w: unknown fill source %q", ErrInvalid, id)
		}
		if !seen[id] {
			seen[id] = true
			src = append(src, id)
		}
	}
	s.FillSources = src
	if s.FillSources == nil {
		s.FillSources = []string{}
	}
	if s.FillPerMinute == 0 {
		s.FillPerMinute = 20
	}
	if s.FillPerMinute < 1 || s.FillPerMinute > 120 {
		return fmt.Errorf("%w: fill_per_minute must be 1..120", ErrInvalid)
	}
	return nil
}

// Store loads/saves Settings and notifies appliers on change.
type Store struct {
	DB *sql.DB

	mu       sync.Mutex
	cur      Settings
	appliers []func(old, cur Settings)
}

// Load reads persisted settings over defaults (from the config file / env).
func (st *Store) Load(ctx context.Context, def Settings) (Settings, error) {
	cur := def
	var raw string
	err := st.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&raw)
	if err == nil {
		var saved Settings
		if json.Unmarshal([]byte(raw), &saved) == nil && saved.Validate() == nil {
			cur = saved
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return def, err
	}
	if cur.Validate() != nil { // defaults from env may be partial
		cur.FillPerMinute = 20
		if cur.MusicBrainzContact == "" {
			cur.MusicBrainzEnabled = false
		}
		_ = cur.Validate()
	}
	st.mu.Lock()
	st.cur = cur
	st.mu.Unlock()
	return cur, nil
}

// OnChange registers an applier (called synchronously after Put).
func (st *Store) OnChange(f func(old, cur Settings)) {
	st.mu.Lock()
	st.appliers = append(st.appliers, f)
	st.mu.Unlock()
}

func (st *Store) Get() Settings {
	st.mu.Lock()
	defer st.mu.Unlock()
	c := st.cur
	c.FillSources = append([]string{}, c.FillSources...)
	return c
}

// Put merges a JSON patch, validates, persists and applies.
func (st *Store) Put(ctx context.Context, patch []byte) (Settings, error) {
	next := st.Get()
	if err := json.Unmarshal(patch, &next); err != nil {
		return Settings{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := next.Validate(); err != nil {
		return Settings{}, err
	}
	b, _ := json.Marshal(next)
	if _, err := st.DB.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, string(b)); err != nil {
		return Settings{}, err
	}
	st.mu.Lock()
	old := st.cur
	st.cur = next
	apps := append([]func(old, cur Settings){}, st.appliers...)
	st.mu.Unlock()
	for _, f := range apps {
		f(old, next)
	}
	return st.Get(), nil
}

// Supervisor restarts a long-running loop with a fresh context.
type Supervisor struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// Restart stops the current run (waiting for it) and starts run (nil = stop only).
func (s *Supervisor) Restart(parent context.Context, run func(ctx context.Context)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
		<-s.done
		s.cancel, s.done = nil, nil
	}
	if run == nil || parent.Err() != nil {
		return
	}
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	s.cancel, s.done = cancel, done
	go func() {
		defer close(done)
		run(ctx)
	}()
}

// Wait blocks until the current run ends (after its parent is cancelled).
func (s *Supervisor) Wait() {
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	if done != nil {
		<-done
	}
}
