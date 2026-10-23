package gateway

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// RateLimits holds optional per-client and global token buckets.
type RateLimits struct {
	PerClient rate.Limit // tokens/s; 0 = off
	Burst     int
	Global    rate.Limit // 0 = off

	mu      sync.Mutex
	clients map[string]*rate.Limiter
	global  *rate.Limiter
}

func NewRateLimits(perClient float64, burst int, global float64) *RateLimits {
	if burst <= 0 {
		burst = 1
	}
	rl := &RateLimits{PerClient: rate.Limit(perClient), Burst: burst, clients: map[string]*rate.Limiter{}}
	if global > 0 {
		gb := burst
		if global*2 > float64(gb) {
			gb = int(global * 2)
		}
		if gb < 1 {
			gb = 1
		}
		rl.Global = rate.Limit(global)
		rl.global = rate.NewLimiter(rl.Global, gb)
	}
	return rl
}

func (rl *RateLimits) limiter(key string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if lim, ok := rl.clients[key]; ok {
		return lim
	}
	lim := rate.NewLimiter(rl.PerClient, rl.Burst)
	rl.clients[key] = lim
	// opportunistic prune when map grows large
	if len(rl.clients) > 10_000 {
		rl.clients = map[string]*rate.Limiter{key: lim}
	}
	return lim
}

func (rl *RateLimits) allow(key string) (ok bool, retryAfter time.Duration) {
	if rl == nil {
		return true, 0
	}
	if rl.global != nil && !rl.global.Allow() {
		return false, time.Second
	}
	if rl.PerClient <= 0 {
		return true, 0
	}
	lim := rl.limiter(key)
	if lim.Allow() {
		return true, 0
	}
	res := lim.Reserve()
	d := res.Delay()
	res.Cancel() // don't consume; we rejected
	if d < time.Second {
		d = time.Second
	}
	return false, d
}

// rateLimit rejects with 429 when the client or global budget is exhausted.
// /healthz is exempt. Client key is the (possibly rewritten) peer IP; when an
// Authorization bearer is present its first 16 chars are preferred so a NAT
// does not share one bucket across devices.
func rateLimit(rl *RateLimits, next http.Handler) http.Handler {
	if rl == nil || (rl.PerClient <= 0 && rl.global == nil) {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		key := clientIP(r)
		if h := r.Header.Get("Authorization"); len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
			tok := strings.TrimSpace(h[7:])
			if len(tok) > 16 {
				key = tok[:16]
			} else if tok != "" {
				key = tok
			}
		}
		ok, wait := rl.allow(key)
		if !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds()+0.999)))
			writeErr(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
			return
		}
		next.ServeHTTP(w, r)
	})
}
