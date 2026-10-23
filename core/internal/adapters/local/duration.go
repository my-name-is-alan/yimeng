package local

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"strings"
	"time"
)

var errUnknownDuration = errors.New("duration: unsupported or unparseable")

// Info describes the audio stream (for the signal-path display).
type Info struct {
	Duration    time.Duration
	Codec       string // mp3 | flac | aac | alac | vorbis | opus | pcm | dsd
	SampleRate  int
	BitDepth    int // 0 for lossy codecs
	Channels    int
	BitrateKbps int // average
	Lossless    bool
}

// Probe reads stream properties with small pure-Go container parsers
// (dhowden/tag reports neither duration nor stream format).
func Probe(r io.ReadSeeker, format string) (Info, error) {
	var in Info
	var err error
	switch format {
	case "flac":
		in, err = flacProbe(r)
	case "mp3":
		in, err = mp3Probe(r)
	case "m4a":
		in, err = mp4Probe(r)
	case "ogg", "opus":
		in, err = oggProbe(r)
	case "wav":
		in, err = wavProbe(r)
	case "aiff":
		in, err = aiffProbe(r)
	case "dsf":
		in, err = dsfProbe(r)
	case "dff":
		in, err = dffProbe(r)
	case "ape":
		in, err = apeProbe(r)
	default:
		return in, errUnknownDuration
	}
	if err == nil && in.BitrateKbps == 0 && in.Duration > 0 {
		if size, e := r.Seek(0, io.SeekEnd); e == nil {
			in.BitrateKbps = int(float64(size) * 8 / in.Duration.Seconds() / 1000)
		}
	}
	return in, err
}

// Duration is Probe(...).Duration.
func Duration(r io.ReadSeeker, format string) (time.Duration, error) {
	in, err := Probe(r, format)
	return in.Duration, err
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

func flacProbe(r io.ReadSeeker) (Info, error) {
	off, err := skipID3v2(r)
	if err != nil {
		return Info{}, err
	}
	r.Seek(off, io.SeekStart)
	var b [42]byte // "fLaC" + block header(4) + STREAMINFO(34)
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return Info{}, err
	}
	if string(b[:4]) != "fLaC" || b[4]&0x7f != 0 {
		return Info{}, errUnknownDuration
	}
	si := b[8:]
	rate := uint32(si[10])<<12 | uint32(si[11])<<4 | uint32(si[12])>>4
	ch := int(si[12]>>1&0x07) + 1
	bps := int((si[12]&1)<<4|si[13]>>4) + 1
	total := uint64(si[13]&0x0f)<<32 | uint64(binary.BigEndian.Uint32(si[14:18]))
	if rate == 0 || total == 0 {
		return Info{}, errUnknownDuration
	}
	return Info{Duration: secs(float64(total), float64(rate)), Codec: "flac", SampleRate: int(rate), BitDepth: bps,
		Channels: ch, Lossless: true}, nil
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

func mp3Probe(r io.ReadSeeker) (Info, error) {
	d, hd, cbr, err := mp3Duration(r)
	if err != nil {
		return Info{}, err
	}
	ch := 2
	if hd.mono {
		ch = 1
	}
	in := Info{Duration: d, Codec: "mp3", SampleRate: hd.rate, Channels: ch}
	if cbr {
		in.BitrateKbps = hd.bitrate / 1000
	}
	return in, nil
}

func mp3Duration(r io.ReadSeeker) (time.Duration, mp3Header, bool, error) {
	start, err := skipID3v2(r)
	if err != nil {
		return 0, mp3Header{}, false, err
	}
	size, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, mp3Header{}, false, err
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
				return secs(float64(samples), float64(hd.rate)), hd, false, nil
			}
		}
		if len(frame) >= 36+18 && string(frame[36:40]) == "VBRI" {
			frames := binary.BigEndian.Uint32(frame[36+14:])
			return secs(float64(frames)*float64(hd.samples), float64(hd.rate)), hd, false, nil
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
			return 0, hd, false, errUnknownDuration
		}
		return secs(float64(audio)*8, float64(hd.bitrate)), hd, true, nil
	}
	return 0, mp3Header{}, false, errUnknownDuration
}

// mp4Probe walks atoms to moov/mvhd (duration) and the first audio sample
// entry in moov/trak/mdia/minf/stbl/stsd (codec, rate, channels, sample size).
func mp4Probe(r io.ReadSeeker) (Info, error) {
	end, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return Info{}, err
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
		return Info{}, errUnknownDuration
	}
	hs, _, ok := find(ms, me, "mvhd")
	if !ok {
		return Info{}, errUnknownDuration
	}
	var in Info
	// sample entry: walk trak -> mdia -> minf -> stbl -> stsd
	if ts, te, ok := find(ms, me, "trak"); ok {
		s0, e0 := ts, te
		path := []string{"mdia", "minf", "stbl", "stsd"}
		for _, name := range path {
			if s0, e0, ok = find(s0, e0, name); !ok {
				break
			}
		}
		if ok {
			r.Seek(s0+8, io.SeekStart) // skip version/flags + entry count
			var e [36]byte
			if _, err := io.ReadFull(r, e[:]); err == nil {
				switch string(e[4:8]) {
				case "mp4a":
					in.Codec = "aac"
				case "alac":
					in.Codec, in.Lossless = "alac", true
				default:
					in.Codec = strings.TrimSpace(string(e[4:8]))
				}
				in.Channels = int(binary.BigEndian.Uint16(e[24:26]))
				if in.Lossless {
					in.BitDepth = int(binary.BigEndian.Uint16(e[26:28]))
				}
				in.SampleRate = int(binary.BigEndian.Uint32(e[32:36]) >> 16)
			}
		}
	}
	r.Seek(hs, io.SeekStart)
	var b [32]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return Info{}, err
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
		return Info{}, errUnknownDuration
	}
	in.Duration = secs(float64(dur), float64(scale))
	return in, nil
}

func oggProbe(r io.ReadSeeker) (Info, error) {
	r.Seek(0, io.SeekStart)
	head := make([]byte, 512)
	n, _ := io.ReadFull(r, head)
	head = head[:n]
	if len(head) < 28 || string(head[:4]) != "OggS" {
		return Info{}, errUnknownDuration
	}
	segs := int(head[26])
	p := 27 + segs
	if len(head) < p+19 {
		return Info{}, errUnknownDuration
	}
	pkt := head[p:]
	var rate float64
	var preSkip int64
	var in Info
	switch {
	case bytes.HasPrefix(pkt, []byte("\x01vorbis")):
		rate = float64(binary.LittleEndian.Uint32(pkt[12:16]))
		in = Info{Codec: "vorbis", Channels: int(pkt[11]), SampleRate: int(rate)}
	case bytes.HasPrefix(pkt, []byte("OpusHead")):
		rate = 48000
		preSkip = int64(binary.LittleEndian.Uint16(pkt[10:12]))
		in = Info{Codec: "opus", Channels: int(pkt[9]), SampleRate: 48000} // Opus always decodes at 48 kHz
	default:
		return Info{}, errUnknownDuration
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
		return Info{}, errUnknownDuration
	}
	gran := int64(binary.LittleEndian.Uint64(buf[i+6 : i+14]))
	if gran <= preSkip {
		return Info{}, errUnknownDuration
	}
	in.Duration = secs(float64(gran-preSkip), rate)
	return in, nil
}

func wavProbe(r io.ReadSeeker) (Info, error) {
	r.Seek(0, io.SeekStart)
	var h [12]byte
	if _, err := io.ReadFull(r, h[:]); err != nil || string(h[:4]) != "RIFF" || string(h[8:12]) != "WAVE" {
		return Info{}, errUnknownDuration
	}
	var byteRate uint32
	in := Info{Codec: "pcm", Lossless: true}
	for {
		var ch [8]byte
		if _, err := io.ReadFull(r, ch[:]); err != nil {
			return Info{}, errUnknownDuration
		}
		sz := int64(binary.LittleEndian.Uint32(ch[4:]))
		switch string(ch[:4]) {
		case "fmt ":
			var f [16]byte
			if _, err := io.ReadFull(r, f[:]); err != nil {
				return Info{}, err
			}
			in.Channels = int(binary.LittleEndian.Uint16(f[2:4]))
			in.SampleRate = int(binary.LittleEndian.Uint32(f[4:8]))
			byteRate = binary.LittleEndian.Uint32(f[8:12])
			in.BitDepth = int(binary.LittleEndian.Uint16(f[14:16]))
			in.BitrateKbps = int(byteRate * 8 / 1000)
			r.Seek(sz-16+sz%2, io.SeekCurrent)
		case "data":
			if byteRate == 0 {
				return Info{}, errUnknownDuration
			}
			in.Duration = secs(float64(sz), float64(byteRate))
			return in, nil
		default:
			r.Seek(sz+sz%2, io.SeekCurrent)
		}
	}
}

// aiffProbe parses AIFF / AIFC (FORM AIFF|AIFC).
func aiffProbe(r io.ReadSeeker) (Info, error) {
	r.Seek(0, io.SeekStart)
	var h [12]byte
	if _, err := io.ReadFull(r, h[:]); err != nil || string(h[:4]) != "FORM" {
		return Info{}, errUnknownDuration
	}
	kind := string(h[8:12])
	if kind != "AIFF" && kind != "AIFC" {
		return Info{}, errUnknownDuration
	}
	in := Info{Codec: "pcm", Lossless: true}
	var numSampleFrames uint32
	var sampleSize uint16
	for {
		var ch [8]byte
		if _, err := io.ReadFull(r, ch[:]); err != nil {
			return Info{}, errUnknownDuration
		}
		sz := int64(binary.BigEndian.Uint32(ch[4:]))
		switch string(ch[:4]) {
		case "COMM":
			var c [18]byte
			if _, err := io.ReadFull(r, c[:]); err != nil {
				return Info{}, err
			}
			in.Channels = int(binary.BigEndian.Uint16(c[0:2]))
			numSampleFrames = binary.BigEndian.Uint32(c[2:6])
			sampleSize = binary.BigEndian.Uint16(c[6:8])
			in.BitDepth = int(sampleSize)
			// 80-bit SANE float sample rate at c[8:18]
			in.SampleRate = int(sane80(c[8:18]))
			if in.SampleRate > 0 && numSampleFrames > 0 {
				in.Duration = secs(float64(numSampleFrames), float64(in.SampleRate))
				in.BitrateKbps = in.SampleRate * in.BitDepth * in.Channels / 1000
				return in, nil
			}
			pad := sz - 18
			if pad > 0 {
				r.Seek(pad+sz%2, io.SeekCurrent)
			} else if sz%2 != 0 {
				r.Seek(1, io.SeekCurrent)
			}
		default:
			r.Seek(sz+sz%2, io.SeekCurrent)
		}
	}
}

func sane80(b []byte) float64 {
	if len(b) < 10 {
		return 0
	}
	exp := int(binary.BigEndian.Uint16(b[0:2]))
	mant := binary.BigEndian.Uint64(b[2:10])
	if exp == 0 && mant == 0 {
		return 0
	}
	sign := 1.0
	if exp&0x8000 != 0 {
		sign = -1
		exp &^= 0x8000
	}
	// IEEE 754 extended: value = mant / 2^63 * 2^(exp-16383)
	return sign * float64(mant) / (1 << 63) * pow2(int(exp)-16383)
}

func pow2(e int) float64 { return math.Pow(2, float64(e)) }

// dsfProbe parses Sony DSF (DSD Stream File).
func dsfProbe(r io.ReadSeeker) (Info, error) {
	r.Seek(0, io.SeekStart)
	var h [28]byte
	if _, err := io.ReadFull(r, h[:]); err != nil || string(h[:4]) != "DSD " {
		return Info{}, errUnknownDuration
	}
	// fmt chunk follows after DSD chunk; seek to fmt_
	chunkSize := binary.LittleEndian.Uint64(h[4:12])
	if chunkSize < 28 {
		chunkSize = 28
	}
	r.Seek(int64(chunkSize), io.SeekStart)
	var fmt [52]byte
	if _, err := io.ReadFull(r, fmt[:]); err != nil || string(fmt[:4]) != "fmt " {
		return Info{}, errUnknownDuration
	}
	channels := int(binary.LittleEndian.Uint32(fmt[24:28]))
	sampleRate := int(binary.LittleEndian.Uint32(fmt[28:32])) // DSD sample rate (e.g. 2822400)
	bitsPerSample := int(binary.LittleEndian.Uint32(fmt[32:36]))
	sampleCount := binary.LittleEndian.Uint64(fmt[36:44]) // per channel
	if sampleRate == 0 || sampleCount == 0 {
		return Info{}, errUnknownDuration
	}
	return Info{
		Duration: secs(float64(sampleCount), float64(sampleRate)),
		Codec:    "dsd", SampleRate: sampleRate, BitDepth: bitsPerSample, Channels: channels,
		BitrateKbps: sampleRate * bitsPerSample * channels / 1000, Lossless: true,
	}, nil
}

// dffProbe parses DSDIFF (DFF).
func dffProbe(r io.ReadSeeker) (Info, error) {
	r.Seek(0, io.SeekStart)
	var h [16]byte
	if _, err := io.ReadFull(r, h[:]); err != nil || string(h[:4]) != "FRM8" || string(h[12:16]) != "DSD " {
		return Info{}, errUnknownDuration
	}
	in := Info{Codec: "dsd", Lossless: true, BitDepth: 1}
	var sampleRate uint32
	var channels int
	var dataSize uint64
	for {
		var ch [12]byte
		if _, err := io.ReadFull(r, ch[:]); err != nil {
			break
		}
		id := string(ch[:4])
		sz := binary.BigEndian.Uint64(ch[4:12])
		switch id {
		case "PROP":
			end := sz
			var read uint64
			var propType [4]byte
			if _, err := io.ReadFull(r, propType[:]); err != nil {
				return Info{}, err
			}
			read = 4
			for read+12 <= end {
				var sub [12]byte
				if _, err := io.ReadFull(r, sub[:]); err != nil {
					return Info{}, err
				}
				ssz := binary.BigEndian.Uint64(sub[4:12])
				read += 12
				switch string(sub[:4]) {
				case "FS  ":
					var fs [4]byte
					io.ReadFull(r, fs[:])
					sampleRate = binary.BigEndian.Uint32(fs[:])
					if ssz > 4 {
						r.Seek(int64(ssz-4), io.SeekCurrent)
					}
				case "CHNL":
					var n [2]byte
					io.ReadFull(r, n[:])
					channels = int(binary.BigEndian.Uint16(n[:]))
					if ssz > 2 {
						r.Seek(int64(ssz-2), io.SeekCurrent)
					}
				default:
					r.Seek(int64(ssz), io.SeekCurrent)
				}
				read += ssz
				if ssz%2 != 0 {
					r.Seek(1, io.SeekCurrent)
					read++
				}
			}
			if rem := end - read; rem > 0 {
				r.Seek(int64(rem), io.SeekCurrent)
			}
		case "DSD ":
			dataSize = sz
			r.Seek(int64(sz), io.SeekCurrent)
		default:
			r.Seek(int64(sz), io.SeekCurrent)
		}
		if sz%2 != 0 {
			r.Seek(1, io.SeekCurrent)
		}
	}
	if sampleRate == 0 || channels == 0 || dataSize == 0 {
		return Info{}, errUnknownDuration
	}
	// DSD data is 1 bit/sample packed into bytes; samples = dataSize*8/channels
	samples := dataSize * 8 / uint64(channels)
	in.SampleRate = int(sampleRate)
	in.Channels = channels
	in.Duration = secs(float64(samples), float64(sampleRate))
	in.BitrateKbps = int(sampleRate) * channels / 1000
	return in, nil
}

// apeProbe reads Monkey's Audio descriptor for duration (compression level ignored).
func apeProbe(r io.ReadSeeker) (Info, error) {
	r.Seek(0, io.SeekStart)
	var h [8]byte
	if _, err := io.ReadFull(r, h[:]); err != nil || string(h[:4]) != "MAC " {
		return Info{}, errUnknownDuration
	}
	version := binary.LittleEndian.Uint16(h[4:6])
	// Modern APE (v3.98+): descriptor + header. Skip to common fields.
	// Descriptor size at offset 8 for v >= 3980.
	if version >= 3980 {
		var desc [52]byte
		r.Seek(0, io.SeekStart)
		if _, err := io.ReadFull(r, desc[:]); err != nil {
			return Info{}, err
		}
		descSize := binary.LittleEndian.Uint16(desc[8:10])
		if descSize < 52 {
			descSize = 52
		}
		r.Seek(int64(descSize), io.SeekStart)
		var hdr [24]byte
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			return Info{}, err
		}
		compression := binary.LittleEndian.Uint16(hdr[0:2])
		_ = compression
		sampleRate := int(binary.LittleEndian.Uint32(hdr[20:24]))
		// total frames / final frame blocks further in header — read more
		var more [16]byte
		if _, err := io.ReadFull(r, more[:]); err != nil {
			return Info{}, err
		}
		blocksPerFrame := binary.LittleEndian.Uint32(more[0:4])
		finalFrameBlocks := binary.LittleEndian.Uint32(more[4:8])
		totalFrames := binary.LittleEndian.Uint32(more[8:12])
		bitsPerSample := int(binary.LittleEndian.Uint16(more[12:14]))
		channels := int(binary.LittleEndian.Uint16(more[14:16]))
		if sampleRate == 0 || totalFrames == 0 {
			return Info{}, errUnknownDuration
		}
		var totalSamples uint64
		if totalFrames == 1 {
			totalSamples = uint64(finalFrameBlocks)
		} else {
			totalSamples = uint64(blocksPerFrame)*uint64(totalFrames-1) + uint64(finalFrameBlocks)
		}
		return Info{
			Duration: secs(float64(totalSamples), float64(sampleRate)),
			Codec:    "ape", SampleRate: sampleRate, BitDepth: bitsPerSample, Channels: channels,
			Lossless: true,
		}, nil
	}
	return Info{}, errUnknownDuration
}
