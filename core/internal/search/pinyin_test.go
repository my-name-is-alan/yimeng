package search

import "testing"

func TestTextPinyin(t *testing.T) {
	s := Text("周杰伦")
	if s == "" || !contains(s, "zhou") {
		t.Fatal(s)
	}
	if !contains(s, "zjl") && !contains(s, "zhoujielun") {
		// initials zjl or full concatenated
		t.Log(s)
	}
}

func TestExpandQuery(t *testing.T) {
	ex := ExpandQuery("周")
	if len(ex) < 1 {
		t.Fatal(ex)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		stringIndex(s, sub) >= 0)
}

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestSortLetter(t *testing.T) {
	for in, want := range map[string]string{"晴天": "Q", "  apple": "A", "Zebra": "Z", "1999": "#", "": "#", "éclair": "#"} {
		if got := SortLetter(in); got != want {
			t.Errorf("%q: %q want %q", in, got, want)
		}
	}
}
