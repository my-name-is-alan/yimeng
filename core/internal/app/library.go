// Package app holds use-case orchestration between the Gateway and services.
package app

import (
	"context"
	"time"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
	"github.com/my-name-is-alan/yimeng/core/internal/playback"
)

// LocalLease builds P0 leases: the client fetches audio from Core's own
// Range endpoint with its device token (delivery "direct").
func LocalLease(sc *library.Scanner) playback.LeaseFunc {
	return func(ctx context.Context, trackID string) (playback.Lease, error) {
		f, err := sc.TrackFile(ctx, trackID)
		if err != nil {
			return playback.Lease{}, err
		}
		return playback.Lease{Delivery: playback.DeliveryDirect, URL: "/api/stream/" + trackID,
			Format: f.Format, ExpiresAt: time.Now().Add(6 * time.Hour).UTC()}, nil
	}
}

// Library exposes scan + catalog use cases to the Gateway. Catalog reads are
// promoted from the embedded Scanner.
type Library struct {
	*library.Scanner
	// BG is the long-lived context background scans run under (server lifetime).
	BG context.Context
}

func (l *Library) StartScan(_ context.Context, req library.StartRequest) (library.Job, error) {
	return l.Scanner.Start(l.BG, req)
}

func (l *Library) ScanStatus(ctx context.Context, id string) (library.Job, error) {
	return l.Scanner.Get(ctx, id)
}

func (l *Library) LatestScan(ctx context.Context) (library.Job, error) {
	return l.Scanner.Latest(ctx)
}
