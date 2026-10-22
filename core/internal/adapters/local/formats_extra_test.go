package local

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSupportedExtras(t *testing.T) {
	for _, e := range []string{".aiff", ".aif", ".ape", ".dsf", ".dff"} {
		if !Supported("x" + e) {
			t.Fatal(e)
		}
	}
}

func TestAIFFProbe(t *testing.T) {
	rate := sane80Bytes(44100)
	comm := make([]byte, 18)
	binary.BigEndian.PutUint16(comm[0:2], 1)
	binary.BigEndian.PutUint32(comm[2:6], 44100)
	binary.BigEndian.PutUint16(comm[6:8], 16)
	copy(comm[8:18], rate)
	buf := &bytes.Buffer{}
	buf.WriteString("FORM")
	binary.Write(buf, binary.BigEndian, uint32(4+8+18))
	buf.WriteString("AIFF")
	buf.WriteString("COMM")
	binary.Write(buf, binary.BigEndian, uint32(18))
	buf.Write(comm)
	in, err := aiffProbe(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if in.SampleRate != 44100 || in.Channels != 1 || in.BitDepth != 16 {
		t.Fatalf("%+v", in)
	}
	if d := in.Duration; d < 900*time.Millisecond || d > 1100*time.Millisecond {
		t.Fatal(d)
	}
}

func sane80Bytes(hz float64) []byte {
	b := make([]byte, 10)
	if hz == 0 {
		return b
	}
	exp := 0
	m := hz
	for m >= 2 {
		m /= 2
		exp++
	}
	for m < 1 {
		m *= 2
		exp--
	}
	mant := uint64(m * math.Pow(2, 63))
	binary.BigEndian.PutUint16(b[0:2], uint16(exp+16383))
	binary.BigEndian.PutUint64(b[2:10], mant)
	return b
}

func TestDSFProbeFromTagSample(t *testing.T) {
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		gopath = filepath.Join(os.Getenv("HOME"), "go")
	}
	matches, _ := filepath.Glob(filepath.Join(gopath, "pkg/mod/github.com/dhowden/tag@*/testdata/with_tags/sample.dsf"))
	if len(matches) == 0 {
		t.Skip("no sample.dsf")
	}
	f, err := os.Open(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	in, err := dsfProbe(f)
	if err != nil {
		t.Fatal(err)
	}
	if in.Codec != "dsd" || in.Duration <= 0 || in.SampleRate <= 0 {
		t.Fatalf("%+v", in)
	}
}

func TestAPEProbeSynthetic(t *testing.T) {
	// Minimal MAC v3.98 descriptor+header — layout from Monkey's Audio SDK
	buf := make([]byte, 128)
	copy(buf[0:4], "MAC ")
	binary.LittleEndian.PutUint16(buf[4:6], 3990)          // version
	binary.LittleEndian.PutUint16(buf[8:10], 52)           // descriptor bytes
	binary.LittleEndian.PutUint32(buf[52+20:52+24], 44100) // sample rate in header
	binary.LittleEndian.PutUint32(buf[52+24:52+28], 1152)  // blocks per frame
	binary.LittleEndian.PutUint32(buf[52+28:52+32], 1152)  // final frame blocks
	binary.LittleEndian.PutUint32(buf[52+32:52+36], 2)     // total frames
	binary.LittleEndian.PutUint16(buf[52+36:52+38], 16)    // bits
	binary.LittleEndian.PutUint16(buf[52+38:52+40], 2)     // channels
	in, err := apeProbe(bytes.NewReader(buf))
	if err != nil {
		t.Fatal(err)
	}
	if in.Codec != "ape" || in.SampleRate != 44100 || in.Channels != 2 {
		t.Fatalf("%+v", in)
	}
	if in.Duration <= 0 {
		t.Fatal(in.Duration)
	}
}
