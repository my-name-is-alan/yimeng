// Package safehttp provides HTTP transports for fetching URLs that come from
// outside (platform API responses, metadata providers, user input): they refuse
// to connect to loopback, private, link-local, CGNAT or unspecified addresses
// (SSRF guard), checked after DNS resolution on every dial, so redirects and
// DNS rebinding cannot reach the LAN or the host itself.
package safehttp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"sync/atomic"
	"syscall"
	"time"
)

// ErrBlocked is returned when a destination address is not public.
var ErrBlocked = errors.New("safehttp: destination address not allowed")

var allowPrivate atomic.Bool

// AllowPrivate disables the guard (tests that use httptest on 127.0.0.1).
func AllowPrivate(v bool) { allowPrivate.Store(v) }

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// Public reports whether ip is a globally routable unicast address.
func Public(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsValid() && ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() && !ip.IsMulticast() && !ip.IsUnspecified() && !cgnat.Contains(ip)
}

func control(_, address string, _ syscall.RawConn) error {
	if allowPrivate.Load() {
		return nil
	}
	ap, err := netip.ParseAddrPort(address)
	if err != nil || !Public(ap.Addr()) {
		return ErrBlocked
	}
	return nil
}

// Transport returns a transport whose every connection is checked. Proxies
// are not used (the check must apply to the real destination).
func Transport() *http.Transport {
	d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second, Control: control}
	return &http.Transport{
		DialContext:           func(ctx context.Context, n, a string) (net.Conn, error) { return d.DialContext(ctx, n, a) },
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		IdleConnTimeout:       60 * time.Second,
		MaxIdleConnsPerHost:   4,
	}
}
