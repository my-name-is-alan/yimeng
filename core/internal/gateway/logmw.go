package gateway

import (
	"bufio"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"
)

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *statusWriter) WriteHeader(c int) {
	if w.status == 0 {
		w.status = c
	}
	w.ResponseWriter.WriteHeader(c)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		w.status = http.StatusSwitchingProtocols
		return h.Hijack()
	}
	return nil, nil, errors.New("hijack not supported")
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// accessLog logs one structured line per request. The query string is never
// logged (it may carry access_token); the device id is logged when known.
func accessLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		holder := &logHolder{}
		r = r.WithContext(context.WithValue(r.Context(), logKey{}, holder))
		defer func() {
			lvl := slog.LevelDebug
			if sw.status >= 500 {
				lvl = slog.LevelWarn
			}
			attrs := []any{"method", r.Method, "path", r.URL.Path, "status", sw.status, "bytes", sw.bytes,
				"dur_ms", time.Since(start).Milliseconds(), "remote", clientIP(r)}
			if holder.device != "" {
				attrs = append(attrs, "device", holder.device)
			}
			log.Log(r.Context(), lvl, "http", attrs...)
		}()
		next.ServeHTTP(sw, r)
	})
}

type logKey struct{}
type logHolder struct{ device string }

func noteDevice(ctx context.Context, id string) {
	if h, ok := ctx.Value(logKey{}).(*logHolder); ok {
		h.device = id
	}
}
