package subsonic

import (
	"context"
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

func TestVisibleDropsPreviewAndUnavailable(t *testing.T) {
	h := &Handler{Decorate: func(_ context.Context, ts []library.Track) {
		for i := range ts {
			switch ts[i].ID {
			case "netease:1":
				ts[i].Entitlement = "preview"
			case "netease:2":
				ts[i].Entitlement = "unavailable"
			case "netease:3":
				ts[i].Entitlement = "full"
			}
		}
	}}
	in := []library.Track{{ID: "local1"}, {ID: "netease:1"}, {ID: "netease:2"}, {ID: "netease:3"}}
	got := h.visible(context.Background(), in)
	if len(got) != 2 || got[0].ID != "local1" || got[1].ID != "netease:3" {
		t.Fatalf("got %+v", got)
	}
}
