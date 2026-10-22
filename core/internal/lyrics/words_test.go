package lyrics

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"testing"
)

func TestParseYRC(t *testing.T) {
	src := "{\"t\":0,\"c\":[{\"tx\":\"作词\"}]}\n[1000,2000](1000,500,0)晴(1500,500,0)天\n[3000,1000](3000,1000,0)好"
	ls := ParseYRC(src)
	if len(ls) != 2 || ls[0].Text != "晴天" || ls[0].Words[1].TMs != 1500 {
		t.Fatalf("%+v", ls)
	}
}

func TestKRCRoundTrip(t *testing.T) {
	text := "[ar:x]\n[language:" + base64.StdEncoding.EncodeToString([]byte(`{"content":[{"type":1,"lyricContent":[["Sunny day"],["Good"]]},{"type":0,"lyricContent":[["qing","tian"],["hao"]]}]}`)) + "]\n[1000,2000]<0,500,0>晴<500,500,0>天\n[3000,1000]<0,1000,0>好\n"
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	zw.Write([]byte(text))
	zw.Close()
	body := z.Bytes()
	for i := range body {
		body[i] ^= krcKey[i%len(krcKey)]
	}
	dec, err := DecodeKRC(base64.StdEncoding.EncodeToString(append([]byte("krc1"), body...)))
	if err != nil || dec != text {
		t.Fatalf("decode %v %q", err, dec)
	}
	ls := ParseKRCText(dec)
	if len(ls) != 2 || ls[0].Words[1].TMs != 1500 || ls[0].Text != "晴天" {
		t.Fatalf("%+v", ls)
	}
	tr, ro := KRCExtras(dec)
	if tr[0] != "Sunny day" || ro[0] != "qing tian" {
		t.Fatalf("%v %v", tr, ro)
	}
}

func TestMerge(t *testing.T) {
	base := Source{Provider: "lrclib", Lines: []Line{{1000, "晴天"}, {3000, "好"}}}
	ne := Source{Provider: "netease", Words: []WordLine{{TMs: 1100, Text: "晴天", Words: []Word{{1100, 400, "晴"}, {1500, 400, "天"}}}, {TMs: 3050, Text: "好", Words: []Word{{3050, 900, "好"}}}},
		Trans: []Line{{1000, "Sunny"}, {3000, "Good"}}}
	kg := Source{Provider: "kugou", Words: []WordLine{{TMs: 990, Text: "晴天", Words: []Word{{990, 500, "晴"}, {1490, 500, "天"}}}, {TMs: 3000, Text: "好", Words: []Word{{3000, 1000, "好"}}}},
		Roma: []Line{{1000, "qing tian"}, {3000, "hao"}}}
	r := Merge([]Source{base, ne, kg})
	if r.Sources["words"] != "kugou" || r.Sources["trans"] != "netease" || r.Sources["roma"] != "kugou" || r.Sources["text"] != "lrclib" {
		t.Fatalf("sources %v", r.Sources)
	}
	if l := r.Lines[0]; l.Trans != "Sunny" || l.Roma != "qing tian" || len(l.Words) != 2 || l.Words[0].TMs != 1000 || l.Words[1].TMs != 1500 {
		t.Fatalf("line %+v", l)
	}
}

func TestMarkMeta(t *testing.T) {
	ls := []MergedLine{{Text: "稻香 - 周杰伦"}, {Text: "词：周杰伦"}, {Text: "作曲 : 周杰伦"}, {Text: "编曲：黄雨勋"}, {Text: "对这个世界如果你有太多的抱怨"}, {Text: "曲：x"}}
	MarkMeta(ls, "稻香")
	for i, want := range []bool{true, true, true, true, false, false} {
		if ls[i].Meta != want {
			t.Errorf("line %d %q meta=%v", i, ls[i].Text, ls[i].Meta)
		}
	}
}
