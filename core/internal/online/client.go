package online

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Client is the per-account HTTP client handed to a plugin: cookie jar backed
// by the Vault, a token-bucket rate limiter and a risk-control backoff.
type Client struct {
	ID string
	HC *http.Client

	mu        sync.Mutex
	cookies   map[string]string
	dirty     bool
	limiter   *rate.Limiter
	strikes   int
	coolUntil time.Time
	now       func() time.Time
}

func NewClient(id string, perMinute int, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	c := &Client{ID: id, HC: hc, cookies: map[string]string{}, now: time.Now}
	c.SetRate(perMinute)
	return c
}

// SetRate sets requests per minute (burst = min(rate, 10)).
func (c *Client) SetRate(perMinute int) {
	if perMinute <= 0 {
		perMinute = 60
	}
	burst := min(perMinute, 10)
	c.mu.Lock()
	c.limiter = rate.NewLimiter(rate.Limit(float64(perMinute)/60), burst)
	c.mu.Unlock()
}

// Do sends req with the jar's cookies (unless the request sets its own Cookie
// header), waits for the limiter and stores Set-Cookie values in the jar.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	until, lim := c.coolUntil, c.limiter
	c.mu.Unlock()
	if c.now().Before(until) {
		return nil, fmt.Errorf("%w: cooling down until %s", ErrRiskControl, until.Format(time.TimeOnly))
	}
	if err := lim.Wait(req.Context()); err != nil {
		return nil, err
	}
	res, err := c.HC.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	if res.StatusCode == http.StatusTooManyRequests {
		c.Penalize()
	}
	if sc := res.Cookies(); len(sc) > 0 {
		c.mu.Lock()
		for _, k := range sc {
			if k.Value == "" || (!k.Expires.IsZero() && k.Expires.Before(c.now())) || k.MaxAge < 0 {
				if _, ok := c.cookies[k.Name]; ok {
					delete(c.cookies, k.Name)
					c.dirty = true
				}
				continue
			}
			if c.cookies[k.Name] != k.Value {
				c.cookies[k.Name] = k.Value
				c.dirty = true
			}
		}
		c.mu.Unlock()
	}
	return res, nil
}

// Penalize starts/extends an exponential cooldown (30s, 1m, 2m … max 15m).
func (c *Client) Penalize() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.strikes++
	d := 30 * time.Second << min(c.strikes-1, 5)
	if d > 15*time.Minute {
		d = 15 * time.Minute
	}
	c.coolUntil = c.now().Add(d)
}

// Reward clears strikes after a successful call.
func (c *Client) Reward() {
	c.mu.Lock()
	c.strikes = 0
	c.mu.Unlock()
}

// CooldownUntil reports an active cooldown (zero when none).
func (c *Client) CooldownUntil() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.now().After(c.coolUntil) {
		return time.Time{}
	}
	return c.coolUntil
}

func (c *Client) Cookie(name string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cookies[name]
}

func (c *Client) Cookies() map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]string, len(c.cookies))
	for k, v := range c.cookies {
		out[k] = v
	}
	return out
}

func (c *Client) SetCookie(name, value string) {
	c.mu.Lock()
	if c.cookies[name] != value {
		c.cookies[name] = value
		c.dirty = true
	}
	c.mu.Unlock()
}

// ReplaceCookies sets the whole jar (login / restore from vault).
func (c *Client) ReplaceCookies(m map[string]string) {
	c.mu.Lock()
	c.cookies = map[string]string{}
	for k, v := range m {
		c.cookies[k] = v
	}
	c.dirty = true
	c.mu.Unlock()
}

// TakeDirty reports and clears the changed flag.
// MarkDirty forces the next Persist to write the jar.
func (c *Client) MarkDirty() {
	c.mu.Lock()
	c.dirty = true
	c.mu.Unlock()
}

func (c *Client) TakeDirty() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	d := c.dirty
	c.dirty = false
	return d
}

// CookieHeader renders the jar (sorted for stable tests), merged with extra.
func (c *Client) CookieHeader(extra map[string]string) string {
	m := c.Cookies()
	for k, v := range extra {
		if _, ok := m[k]; !ok {
			m[k] = v
		}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, "; ")
}

// ParseCookieString parses "a=1; b=2" (as pasted from a browser).
func ParseCookieString(s string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(s, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if !ok || k == "" || v == "" {
			continue
		}
		out[k] = v
	}
	return out
}

// withTimeout bounds plugin calls made by the Service.
func withTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, d)
}
