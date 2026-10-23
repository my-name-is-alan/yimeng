package lyrics

import (
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestParseLRC(t *testing.T) {
	l := Parse("\ufeff[ti:Song]\n[ar:歌手]\n[offset:+500]\n[00:01.00][00:10.5]副歌\r\n[00:05.123]<00:05.12>逐<00:05.50>字\n\n[01:02]end\n")
	if !l.Synced || l.Meta["ar"] != "歌手" || l.OffsetMs != 500 || len(l.Lines) != 4 {
		t.Fatalf("%+v", l)
	}
	want := []Line{{500, "副歌"}, {4623, "逐字"}, {10000, "副歌"}, {61500, "end"}}
	for i, w := range want {
		if l.Lines[i] != w {
			t.Errorf("line %d: %+v want %+v", i, l.Lines[i], w)
		}
	}
	p := Parse("just\nplain words")
	if p.Synced || p.Text != "just\nplain words" || len(p.Lines) != 2 {
		t.Fatalf("%+v", p)
	}
	gbk, _ := simplifiedchinese.GBK.NewEncoder().String("[00:01.00]你好")
	if g := Parse(Decode([]byte(gbk))); g.Lines[0].Text != "你好" {
		t.Fatalf("%+v", g)
	}
}
