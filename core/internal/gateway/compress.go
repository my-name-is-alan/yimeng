package gateway

import (
	"bufio"
	"compress/gzip"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"sync"
)

// gzipMin is the smallest body worth compressing.
const gzipMin = 1024

var gzPool = sync.Pool{New: func() any { w, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed); return w }}

// compressible reports whether a Content-Type benefits from gzip
// (JSON / XML / text / JS / CSS / SVG); media and images never do.
func compressible(ct string) bool {
	mt, _, _ := mime.ParseMediaType(ct)
	switch {
	case mt == "", strings.HasPrefix(mt, "text/event-stream"):
		return false
	case strings.HasPrefix(mt, "text/"), strings.HasSuffix(mt, "json"), strings.HasSuffix(mt, "xml"),
		mt == "application/javascript", mt == "image/svg+xml", mt == "application/manifest+json":
		return true
	}
	return false
}

// compress gzips compressible responses for clients that accept it. The
// decision is made at WriteHeader time from the handler's Content-Type, so
// audio streams, images, Range responses, SSE and WebSocket upgrades pass
// through untouched. Bodies smaller than gzipMin are sent as-is.
func compress(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") ||
			r.Header.Get("Range") != "" || strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Add("Vary", "Accept-Encoding")
		gw := &gzWriter{ResponseWriter: w}
		defer gw.close()
		next.ServeHTTP(gw, r)
	})
}

type gzWriter struct {
	http.ResponseWriter
	status  int
	decided bool
	gz      *gzip.Writer
	buf     []byte // held until gzipMin bytes or close, to skip tiny bodies
}

func (g *gzWriter) WriteHeader(code int) {
	if g.status == 0 {
		g.status = code
	}
}

func (g *gzWriter) decide(final bool) {
	if g.decided {
		return
	}
	h := g.Header()
	code := g.status
	if code == 0 {
		code = http.StatusOK
	}
	ok := code == http.StatusOK && h.Get("Content-Encoding") == "" && h.Get("Content-Range") == "" && compressible(h.Get("Content-Type"))
	if ok && final && len(g.buf) < gzipMin {
		ok = false
	}
	if !ok && !final && len(g.buf) < gzipMin && compressible(h.Get("Content-Type")) && code == http.StatusOK && h.Get("Content-Encoding") == "" {
		return // keep buffering
	}
	g.decided = true
	if ok {
		h.Del("Content-Length")
		h.Set("Content-Encoding", "gzip")
		g.gz = gzPool.Get().(*gzip.Writer)
		g.gz.Reset(g.ResponseWriter)
	}
	g.ResponseWriter.WriteHeader(code)
	if len(g.buf) > 0 {
		if g.gz != nil {
			g.gz.Write(g.buf)
		} else {
			g.ResponseWriter.Write(g.buf)
		}
		g.buf = nil
	}
}

func (g *gzWriter) Write(p []byte) (int, error) {
	if !g.decided {
		g.buf = append(g.buf, p...)
		g.decide(false)
		return len(p), nil
	}
	if g.gz != nil {
		return g.gz.Write(p)
	}
	return g.ResponseWriter.Write(p)
}

func (g *gzWriter) close() {
	if !g.decided {
		if g.status == 0 && len(g.buf) == 0 {
			return // handler wrote nothing (e.g. hijacked)
		}
		g.decide(true)
	}
	if g.gz != nil {
		g.gz.Close()
		gzPool.Put(g.gz)
		g.gz = nil
	}
}

func (g *gzWriter) Flush() {
	if !g.decided {
		g.decide(true)
	}
	if g.gz != nil {
		g.gz.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (g *gzWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := g.ResponseWriter.(http.Hijacker); ok {
		g.decided = true
		return h.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

func (g *gzWriter) Unwrap() http.ResponseWriter { return g.ResponseWriter }
