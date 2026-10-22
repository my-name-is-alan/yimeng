package netease

import "testing"

func TestSplitPhone(t *testing.T) {
	for in, want := range map[string]string{"13312345678": "86/13312345678", "+8613312345678": "86/13312345678", "+86 133-1234-5678": "86/13312345678", "+85291234567890": "852/91234567890"} {
		cc, p, err := splitPhone(in)
		if err != nil || cc+"/"+p != want {
			t.Errorf("%s: %s/%s %v", in, cc, p, err)
		}
	}
	if _, _, err := splitPhone("abc"); err == nil {
		t.Error("bad phone accepted")
	}
}
