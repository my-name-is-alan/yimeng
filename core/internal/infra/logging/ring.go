package logging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// Entry is one captured log record (for the web admin log viewer).
type Entry struct {
	Seq   uint64            `json:"seq"`
	Time  time.Time         `json:"time"`
	Level string            `json:"level"`
	Msg   string            `json:"msg"`
	Attrs map[string]string `json:"attrs,omitempty"`
}

// Ring keeps the last N records in memory. Attributes whose key looks secret are redacted.
type Ring struct {
	mu   sync.Mutex
	buf  []Entry
	next int
	full bool
	seq  uint64
}

func NewRing(n int) *Ring {
	if n <= 0 {
		n = 500
	}
	return &Ring{buf: make([]Entry, n)}
}

var secretKeys = []string{"token", "password", "secret", "cookie", "key", "authorization"}

func redact(k string) bool {
	k = strings.ToLower(k)
	for _, s := range secretKeys {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

func (r *Ring) add(e Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	e.Seq = r.seq
	r.buf[r.next] = e
	r.next = (r.next + 1) % len(r.buf)
	if r.next == 0 {
		r.full = true
	}
}

// Since returns records with Seq > after and level >= min, oldest first, at most limit.
func (r *Ring) Since(after uint64, min slog.Level, limit int) []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	var all []Entry
	if r.full {
		all = append(all, r.buf[r.next:]...)
	}
	all = append(all, r.buf[:r.next]...)
	out := []Entry{}
	for _, e := range all {
		var lv slog.Level
		lv.UnmarshalText([]byte(e.Level))
		if e.Seq > after && lv >= min {
			out = append(out, e)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// Wrap returns a handler that tees every enabled record into the ring.
func (r *Ring) Wrap(h slog.Handler) slog.Handler { return &ringHandler{h: h, r: r} }

type ringHandler struct {
	h     slog.Handler
	r     *Ring
	attrs []slog.Attr
	group string
}

func (h *ringHandler) Enabled(ctx context.Context, l slog.Level) bool { return h.h.Enabled(ctx, l) }

func (h *ringHandler) Handle(ctx context.Context, rec slog.Record) error {
	e := Entry{Time: rec.Time, Level: rec.Level.String(), Msg: rec.Message, Attrs: map[string]string{}}
	put := func(a slog.Attr) {
		k := a.Key
		if h.group != "" {
			k = h.group + "." + k
		}
		if redact(a.Key) {
			e.Attrs[k] = "[redacted]"
		} else {
			e.Attrs[k] = fmt.Sprint(a.Value.Resolve().Any())
		}
	}
	for _, a := range h.attrs {
		put(a)
	}
	rec.Attrs(func(a slog.Attr) bool { put(a); return true })
	if len(e.Attrs) == 0 {
		e.Attrs = nil
	}
	h.r.add(e)
	return h.h.Handle(ctx, rec)
}

func (h *ringHandler) WithAttrs(as []slog.Attr) slog.Handler {
	return &ringHandler{h: h.h.WithAttrs(as), r: h.r, attrs: append(append([]slog.Attr{}, h.attrs...), as...), group: h.group}
}

func (h *ringHandler) WithGroup(name string) slog.Handler {
	g := name
	if h.group != "" {
		g = h.group + "." + name
	}
	return &ringHandler{h: h.h.WithGroup(name), r: h.r, attrs: h.attrs, group: g}
}
