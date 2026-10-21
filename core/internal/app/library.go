// Package app holds use-case orchestration between the Gateway and services.
package app

import (
	"context"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

// Library is the scan use case consumed by the Gateway.
type Library struct {
	Scanner *library.Scanner
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
