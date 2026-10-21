package local

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"time"
)

var errUnknownDuration = errors.New("duration: unsupported or unparseable")

// Duration computes playback length with small pure-Go container parsers
// (dhowden/tag does not report duration).
func Duration(r io.ReadSeeker, format string) (time.Duration, error) {
	switch format {
	case "flac":
		return flacDuration(r)
	case "mp3":
		return mp3Duration(r)
	case "m4a":
		return mp4Duration(r)
	case "ogg", "opus":
		return oggDuration(r)
	case "wav":
		return wavDuration(r)
	}
	return 0, errUnknownDuration
}

func secs(num, den float64) time.Duration { return time.Duration(num / den * float64(time.Second)) }

// skipID3v2 returns the offset after a leading ID3v2 tag (0 if none).
func skipID3v2(r io.ReadSeeker) (int64, error) {
	var h [10]byte
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return 0, err
	}
	if string(h[:3]) != "ID3" {
		return 0, nil
	}
	size := int64(h[6]&0x7f)<<21 | int64(h[7]&0x7f)<<14 | int64(h[8]&0x7f)<<7 | int64(h[9]&0x7f)
	off := 10 + size
	if h[5]&0x10 != 0 {
		off += 10
	}
	return off, nil
}

func flacDuration(r io.ReadSeeker) (time.Duration, error) {
	off, err := skipID3v2(r)
	if err != nil {
		return 0, err
	}
	r.Seek(off, io.SeekStart)
	var b [42]byte // "fLaC" + block header(4) + STREAMINFO(34)
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	if string(b[:4]) != "fLaC" || b[4]&0x7f != 0 {
		return 0, errUnknownDuration
	}
	si := b[8:]
	rate := uint32(si[10])<<12 | uint32(si[11])<<4 | uint32(si[12])>>4
	total := uint64(si[13]&0x0f)<<32 | uint64(binary.BigEndian.Uint32(si[14:18]))
	if rate == 0 || total == 0 {
		return 0, errUnknownDuration
	}
	return secs(float64(total), float64(rate)), nil
}

var mp3Bitrates = [2][3][16]int{
	{ // MPEG1: L1, L2, L3
		{0, 32, 64, 96, 128, 160, 192, 224, 256, 288, 320, 352, 384, 416, 448},
		{0, 32, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 384},
		{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320},
	},
	{ // MPEG2/2.5
		{0, 32, 48, 56, 64, 80, 96, 112, 128, 144, 160, 176, 192, 224, 256},
		{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160},
		{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160},
	},
}

type mp3Header struct {
	mpeg1         bool
	layer         int // 1..3
	bitrate, rate int
	mono          bool
	samples       int
}

func parseMP3Header(h []byte) (mp3Header, bool) {
	if h[0] != 0xff || h[1]&0xe0 != 0xe0 {
		return mp3Header{}, false
	}
	ver := (h[1] >> 3) & 3
	layerBits := (h[1] >> 1) & 3
	bi := h[2] >> 4
	si := (h[2] >> 2) & 3
	if ver == 1 || layerBits == 0 || bi == 0 || bi == 15 || si == 3 {
		return mp3Header{}, false
	}
	var hd mp3Header
	hd.mpeg1 = ver == 3
	hd.layer = 4 - int(layerBits)
	vi := 1
	if hd.mpeg1 {
		vi = 0
	}
	hd.bitrate = mp3Bitrates[vi][hd.layer-1][bi] * 1000
	hd.rate = []int{44100, 48000, 32000}[si]
	switch ver {
	case 2:
		hd.rate /= 2
	case 0:
		hd.rate /= 4
	}
	hd.mono = h[3]>>6 == 3
	switch {
	case hd.layer == 1:
		hd.samples = 384
	case hd.layer == 2 || hd.mpeg1:
		hd.samples = 1152
	default:
		hd.samples = 576
	}
	return hd, true
}

func mp3Duration(r io.ReadSeeker) (time.Duration, error) {
	start, err := skipID3v2(r)
	if err != nil {
		return 0, err
	}
	size, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	r.Seek(start, io.SeekStart)
	buf := make([]byte, 64*1024)
	n, _ := io.ReadFull(r, buf)
	buf = buf[:n]
	for i := 0; i+4 <= len(buf); i++ {
		hd, ok := parseMP3Header(buf[i:])
		if !ok {
			continue
		}
		frame := buf[i:]
		// Xing/Info VBR header.
		xoff := 4 + 32
		switch {
		case hd.mpeg1 && hd.mono:
			xoff = 4 + 17
		case !hd.mpeg1 && !hd.mono:
			xoff = 4 + 17
		case !hd.mpeg1 && hd.mono:
			xoff = 4 + 9
		}
		if len(frame) >= xoff+12 {
			id := string(frame[xoff : xoff+4])
			if (id == "Xing" || id == "Info") && binary.BigEndian.Uint32(frame[xoff+4:])&1 != 0 {
				frames := binary.BigEndian.Uint32(frame[xoff+8:])
				samples := int64(frames) * int64(hd.samples)
				// LAME/Lavc extension: encoder delay + padding (gapless info).
				if lo := xoff + 120; len(frame) >= lo+24 {
					if id := string(frame[lo : lo+4]); id == "LAME" || id == "Lavc" || id == "Lavf" {
						g := frame[lo+21:]
						delay := int64(g[0])<<4 | int64(g[1])>>4
						pad := int64(g[1]&0x0f)<<8 | int64(g[2])
						if delay+pad < samples {
							samples -= delay + pad
						}
					}
				}
				return secs(float64(samples), float64(hd.rate)), nil
			}
		}
		if len(frame) >= 36+18 && string(frame[36:40]) == "VBRI" {
			frames := binary.BigEndian.Uint32(frame[36+14:])
			return secs(float64(frames)*float64(hd.samples), float64(hd.rate)), nil
		}
		// CBR estimate.
		audio := size - start - int64(i)
		var tagv1 [3]byte
		if size >= 128 {
			r.Seek(size-128, io.SeekStart)
			io.ReadFull(r, tagv1[:])
			if string(tagv1[:]) == "TAG" {
				audio -= 128
			}
		}
		if hd.bitrate == 0 || audio <= 0 {
			return 0, errUnknownDuration
		}
		return secs(float64(audio)*8, float64(hd.bitrate)), nil
	}
	return 0, errUnknownDuration
}

// mp4Duration walks atoms to moov/mvhd.
func mp4Duration(r io.ReadSeeker) (time.Duration, error) {
	end, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	find := func(from, to int64, name string) (int64, int64, bool) {
		pos := from
		for pos+8 <= to {
			r.Seek(pos, io.SeekStart)
			var h [16]byte
			if _, err := io.ReadFull(r, h[:8]); err != nil {
				return 0, 0, false
			}
			sz := int64(binary.BigEndian.Uint32(h[:4]))
			hdr := int64(8)
			if sz == 1 {
				if _, err := io.ReadFull(r, h[8:16]); err != nil {
					return 0, 0, false
				}
				sz = int64(binary.BigEndian.Uint64(h[8:16]))
				hdr = 16
			} else if sz == 0 {
				sz = to - pos
			}
			if sz < hdr {
				return 0, 0, false
			}
			if string(h[4:8]) == name {
				return pos + hdr, pos + sz, true
			}
			pos += sz
		}
		return 0, 0, false
	}
	ms, me, ok := find(0, end, "moov")
	if !ok {
		return 0, errUnknownDuration
	}
	hs, _, ok := find(ms, me, "mvhd")
	if !ok {
		return 0, errUnknownDuration
	}
	r.Seek(hs, io.SeekStart)
	var b [32]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return 0, err
	}
	var scale uint32
	var dur uint64
	if b[0] == 1 {
		scale = binary.BigEndian.Uint32(b[20:24])
		dur = binary.BigEndian.Uint64(b[24:32])
	} else {
		scale = binary.BigEndian.Uint32(b[12:16])
		dur = uint64(binary.BigEndian.Uint32(b[16:20]))
	}
	if scale == 0 {
		return 0, errUnknownDuration
	}
	return secs(float64(dur), float64(scale)), nil
}

func oggDuration(r io.ReadSeeker) (time.Duration, error) {
	r.Seek(0, io.SeekStart)
	head := make([]byte, 512)
	n, _ := io.ReadFull(r, head)
	head = head[:n]
	if len(head) < 28 || string(head[:4]) != "OggS" {
		return 0, errUnknownDuration
	}
	segs := int(head[26])
	p := 27 + segs
	if len(head) < p+19 {
		return 0, errUnknownDuration
	}
	pkt := head[p:]
	var rate float64
	var preSkip int64
	switch {
	case bytes.HasPrefix(pkt, []byte("\x01vorbis")):
		rate = float64(binary.LittleEndian.Uint32(pkt[12:16]))
	case bytes.HasPrefix(pkt, []byte("OpusHead")):
		rate = 48000
		preSkip = int64(binary.LittleEndian.Uint16(pkt[10:12]))
	default:
		return 0, errUnknownDuration
	}
	size, _ := r.Seek(0, io.SeekEnd)
	tail := int64(64 * 1024)
	if tail > size {
		tail = size
	}
	r.Seek(size-tail, io.SeekStart)
	buf := make([]byte, tail)
	io.ReadFull(r, buf)
	i := bytes.LastIndex(buf, []byte("OggS"))
	if i < 0 || i+14 > len(buf) || rate == 0 {
		return 0, errUnknownDuration
	}
	gran := int64(binary.LittleEndian.Uint64(buf[i+6 : i+14]))
	if gran <= preSkip {
		return 0, errUnknownDuration
	}
	return secs(float64(gran-preSkip), rate), nil
}

func wavDuration(r io.ReadSeeker) (time.Duration, error) {
	r.Seek(0, io.SeekStart)
	var h [12]byte
	if _, err := io.ReadFull(r, h[:]); err != nil || string(h[:4]) != "RIFF" || string(h[8:12]) != "WAVE" {
		return 0, errUnknownDuration
	}
	var byteRate uint32
	for {
		var ch [8]byte
		if _, err := io.ReadFull(r, ch[:]); err != nil {
			return 0, errUnknownDuration
		}
		sz := int64(binary.LittleEndian.Uint32(ch[4:]))
		switch string(ch[:4]) {
		case "fmt ":
			var f [16]byte
			if _, err := io.ReadFull(r, f[:]); err != nil {
				return 0, err
			}
			byteRate = binary.LittleEndian.Uint32(f[8:12])
			r.Seek(sz-16+sz%2, io.SeekCurrent)
		case "data":
			if byteRate == 0 {
				return 0, errUnknownDuration
			}
			return secs(float64(sz), float64(byteRate)), nil
		default:
			r.Seek(sz+sz%2, io.SeekCurrent)
		}
	}
}
