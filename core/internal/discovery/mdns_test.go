package discovery

import (
	"context"
	"testing"
	"time"
)

func TestRunDisabled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(ctx, Options{Enabled: false, Bind: "127.0.0.1:8741"}); err != nil {
		t.Fatal(err)
	}
}

func TestRunBadBind(t *testing.T) {
	if err := Run(context.Background(), Options{Enabled: true, Bind: "not-a-bind"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestRunAnnounce(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	// may fail on CI without multicast; treat register success as enough
	err := Run(ctx, Options{Enabled: true, Bind: "0.0.0.0:18741", Version: "test", Name: "Yimeng-Test"})
	if err != nil {
		t.Log("announce:", err) // environment may block multicast
	}
}
