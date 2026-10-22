package kugou

import (
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

func TestSkeletonManifest(t *testing.T) {
	p, err := New()(online.Env{})
	if err != nil {
		t.Fatal(err)
	}
	m := p.Manifest()
	if m.ID != "kugou" || !m.Skeleton || len(m.Capabilities) != 0 {
		t.Fatalf("%+v", m)
	}
	if _, ok := p.(online.Streamer); ok {
		t.Fatal("skeleton must not stream")
	}
}
