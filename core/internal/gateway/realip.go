package gateway

import (
	"net"
	"net/http"
	"strings"
)

// TrustedProxies parses CIDRs (or bare IPs) that may set X-Forwarded-For /
// X-Real-IP. An empty list means the headers are never honoured.
type TrustedProxies struct {
	nets []*net.IPNet
}

func ParseTrustedProxies(cidrs []string) (*TrustedProxies, error) {
	tp := &TrustedProxies{}
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if !strings.Contains(c, "/") {
			if ip := net.ParseIP(c); ip != nil {
				if ip.To4() != nil {
					c += "/32"
				} else {
					c += "/128"
				}
			}
		}
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			return nil, err
		}
		tp.nets = append(tp.nets, n)
	}
	return tp, nil
}

func (tp *TrustedProxies) contains(ip net.IP) bool {
	if tp == nil {
		return false
	}
	for _, n := range tp.nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// realIP rewrites r.RemoteAddr to the leftmost untrusted hop in X-Forwarded-For
// (or X-Real-IP) when the direct peer is a trusted proxy. Otherwise the peer
// address is left unchanged.
func realIP(tp *TrustedProxies, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tp != nil && len(tp.nets) > 0 {
			peerHost, peerPort, err := net.SplitHostPort(r.RemoteAddr)
			if err == nil {
				peer := net.ParseIP(peerHost)
				if tp.contains(peer) {
					if ip := forwardedClient(r); ip != "" {
						if peerPort == "" {
							peerPort = "0"
						}
						r = r.Clone(r.Context())
						r.RemoteAddr = net.JoinHostPort(ip, peerPort)
					}
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func forwardedClient(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the left-most address (original client).
		parts := strings.Split(xff, ",")
		cand := strings.TrimSpace(parts[0])
		if ip := net.ParseIP(cand); ip != nil {
			return cand
		}
	}
	if xr := strings.TrimSpace(r.Header.Get("X-Real-IP")); xr != "" {
		if ip := net.ParseIP(xr); ip != nil {
			return xr
		}
	}
	return ""
}
