package netease

import (
	"testing"

	"github.com/my-name-is-alan/yimeng/core/internal/online"
)

func TestEntitlementMapping(t *testing.T) {
	cases := []struct {
		name string
		p    nePrivilege
		want online.Entitlement
	}{
		{"playable", nePrivilege{Pl: 320000}, online.EntFull},
		{"removed", nePrivilege{St: -200}, online.EntUnavailable},
		{"vip song, trial clip", nePrivilege{Fee: 1}, online.EntPreview},
		{"paid album, no trial (purchase required)", nePrivilege{Fee: 4}, online.EntUnavailable},
		{"paid album with trial privilege", nePrivilege{Fee: 4, FreeTrialPrivilege: neTrialPriv{ResConsumable: true}}, online.EntPreview},
		{"no rights", nePrivilege{Fee: 0}, online.EntUnavailable},
	}
	for _, c := range cases {
		if got := entitlement(&c.p); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
	// a URL response without a URL for a paid album is unavailable with a reason
	if s := streamFrom(neURL{Fee: 4, Code: 404}); s.Entitlement != online.EntUnavailable || s.Reason == "" {
		t.Fatalf("stream %+v", s)
	}
}
