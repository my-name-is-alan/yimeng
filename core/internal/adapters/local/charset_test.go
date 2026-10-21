package local

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// asLatin1 simulates what dhowden/tag returns for raw bytes in an ISO-8859-1 frame.
func asLatin1(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

func gbk(t *testing.T, s string) []byte {
	b, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFixLegacyCharset(t *testing.T) {
	cases := []struct{ in, want string }{
		{asLatin1(gbk(t, "周杰伦")), "周杰伦"},
		{asLatin1(gbk(t, "七里香（Live）")), "七里香（Live）"},
		{asLatin1(gbk(t, "王菲 - 红豆")), "王菲 - 红豆"},
		{asLatin1([]byte("梦里花")), "梦里花"}, // UTF-8 bytes flagged as Latin-1
		{"Café del Mar", "Café del Mar"},
		{"Motörhead", "Motörhead"},
		{"Sigur Rós", "Sigur Rós"},
		{"Beyoncé", "Beyoncé"},
		{"Mötley Crüe", "Mötley Crüe"},
		{asLatin1(gbk(t, "陶喆")), "陶喆"},
		{"plain ascii", "plain ascii"},
		{"已经是UTF-8", "已经是UTF-8"},
		{"", ""},
	}
	for _, c := range cases {
		if got := fixLegacyCharset(c.in); got != c.want {
			t.Errorf("fix(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestID3v1GBKFile(t *testing.T) {
	audio, err := os.ReadFile(filepath.Join("testdata", "notags.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	v1 := make([]byte, 128)
	copy(v1, "TAG")
	copy(v1[3:33], gbk(t, "晴天"))
	copy(v1[33:63], gbk(t, "周杰伦"))
	copy(v1[63:93], gbk(t, "叶惠美"))
	copy(v1[93:97], "2003")
	v1[127] = 255
	p := filepath.Join(t.TempDir(), "gbk.mp3")
	os.WriteFile(p, append(audio, v1...), 0o644)
	tr, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Title != "晴天" || tr.Artist != "周杰伦" || tr.Album != "叶惠美" {
		t.Fatalf("got %q / %q / %q (format %s)", tr.Title, tr.Artist, tr.Album, tr.TagFormat)
	}
}
