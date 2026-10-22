// Package discovery announces Core on the LAN via mDNS/DNS-SD (_yimeng._tcp).
package discovery

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/grandcat/zeroconf"
)

const ServiceType = "_yimeng._tcp"

type Options struct {
	Enabled bool
	Bind    string // host:port Core listens on; only the port is published
	Version string
	Name    string // instance name; default "Yimeng"
	Log     *slog.Logger
}

// Run registers a DNS-SD service until ctx ends. No-op when disabled.
func Run(ctx context.Context, o Options) error {
	if !o.Enabled {
		return nil
	}
	log := o.Log
	if log == nil {
		log = slog.Default()
	}
	_, portStr, err := net.SplitHostPort(o.Bind)
	if err != nil {
		return fmt.Errorf("mdns: bind %q: %w", o.Bind, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		return fmt.Errorf("mdns: bad port in %q", o.Bind)
	}
	name := strings.TrimSpace(o.Name)
	if name == "" {
		name = "Yimeng"
	}
	txt := []string{"path=/", "version=" + o.Version, "proto=http"}
	server, err := zeroconf.Register(name, ServiceType, "local.", port, txt, nil)
	if err != nil {
		return fmt.Errorf("mdns register: %w", err)
	}
	log.Info("mDNS announced", "service", ServiceType, "name", name, "port", port)
	<-ctx.Done()
	server.Shutdown()
	// give the goodbye packet a moment to leave
	time.Sleep(50 * time.Millisecond)
	return nil
}
