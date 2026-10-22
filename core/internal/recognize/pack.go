// Package recognize implements NetEase 听歌识曲 fingerprinting (shazam_v2).
//
// Format reverse-engineered clean-room from the official Chrome extension's
// afp.wasm (comparison harness outside the repo). See ALGORITHM.md.
// No wasm is embedded or executed by Core.
package recognize

import (
	"bytes"
	"compress/zlib"
	"crypto/aes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

const (
	// Version is the FPVER string NetEase's hyai client embeds.
	Version = "hyai_1.2.0_client_1.0.0"
	// AESKey is AES-128-ECB key (ASCII); 1-byte rotation of wasm string "074B97221F27F029".
	AESKey = "4B97221F27F02907"

	SampleRate = 8000
	FFTLen     = 2048
	Hop        = 160
	FreqLB     = 100.0
	FreqUB     = 4000.0
)

// Peak is one spectrogram peak in the wire format.
type Peak struct {
	Bin  uint32
	Time uint32
	Mag  float32
}

// Envelope is the zlib-plaintext fingerprint body.
type Envelope struct {
	Duration float32
	Peaks    []Peak
}

func pkcs7Pad(b []byte) []byte {
	n := aes.BlockSize - len(b)%aes.BlockSize
	return append(append([]byte{}, b...), bytes.Repeat([]byte{byte(n)}, n)...)
}

func pkcs7Unpad(b []byte) ([]byte, error) {
	if len(b) == 0 || len(b)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("bad padding length")
	}
	n := int(b[len(b)-1])
	if n == 0 || n > aes.BlockSize || n > len(b) {
		return nil, fmt.Errorf("bad padding")
	}
	for _, x := range b[len(b)-n:] {
		if int(x) != n {
			return nil, fmt.Errorf("bad padding")
		}
	}
	return b[:len(b)-n], nil
}

func aesECBEncrypt(plain []byte) ([]byte, error) {
	blk, err := aes.NewCipher([]byte(AESKey))
	if err != nil {
		return nil, err
	}
	src := pkcs7Pad(plain)
	out := make([]byte, len(src))
	for i := 0; i < len(src); i += aes.BlockSize {
		blk.Encrypt(out[i:], src[i:i+aes.BlockSize])
	}
	return out, nil
}

func aesECBDecrypt(ct []byte) ([]byte, error) {
	blk, err := aes.NewCipher([]byte(AESKey))
	if err != nil {
		return nil, err
	}
	if len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("bad ciphertext length")
	}
	out := make([]byte, len(ct))
	for i := 0; i < len(ct); i += aes.BlockSize {
		blk.Decrypt(out[i:], ct[i:i+aes.BlockSize])
	}
	return pkcs7Unpad(out)
}

func writeStr(buf *bytes.Buffer, s string) {
	_ = binary.Write(buf, binary.LittleEndian, uint32(len(s)))
	buf.WriteString(s)
}

// Pack encodes peaks into NetEase rawdata ciphertext (binary, not base64).
func Pack(e Envelope) ([]byte, error) {
	var body bytes.Buffer
	writeStr(&body, Version)
	body.Write(make([]byte, 8))
	_ = binary.Write(&body, binary.LittleEndian, e.Duration)
	body.WriteString("FPVER")
	writeStr(&body, Version)
	body.WriteString("Peak")
	_ = binary.Write(&body, binary.LittleEndian, uint32(len(e.Peaks)))
	for _, p := range e.Peaks {
		_ = binary.Write(&body, binary.LittleEndian, p.Bin)
		_ = binary.Write(&body, binary.LittleEndian, p.Time)
		_ = binary.Write(&body, binary.LittleEndian, p.Mag)
	}
	var zbuf bytes.Buffer
	zw, err := zlib.NewWriterLevel(&zbuf, zlib.DefaultCompression)
	if err != nil {
		return nil, err
	}
	if _, err := zw.Write(body.Bytes()); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return aesECBEncrypt(zbuf.Bytes())
}

// Unpack reverses Pack (tests + version probing).
func Unpack(ct []byte) (Envelope, error) {
	var zero Envelope
	pt, err := aesECBDecrypt(ct)
	if err != nil {
		return zero, err
	}
	zr, err := zlib.NewReader(bytes.NewReader(pt))
	if err != nil {
		return zero, err
	}
	defer zr.Close()
	raw, err := io.ReadAll(zr)
	if err != nil {
		return zero, err
	}
	return parseEnvelope(raw)
}

func parseEnvelope(raw []byte) (Envelope, error) {
	var e Envelope
	off := 0
	readStr := func() (string, error) {
		if off+4 > len(raw) {
			return "", fmt.Errorf("truncated")
		}
		n := int(binary.LittleEndian.Uint32(raw[off:]))
		off += 4
		if n < 0 || off+n > len(raw) {
			return "", fmt.Errorf("truncated string")
		}
		s := string(raw[off : off+n])
		off += n
		return s, nil
	}
	ver, err := readStr()
	if err != nil {
		return e, err
	}
	if ver != Version {
		return e, fmt.Errorf("unsupported FPVER %q (want %s)", ver, Version)
	}
	if off+12 > len(raw) {
		return e, fmt.Errorf("truncated header")
	}
	off += 8
	e.Duration = math.Float32frombits(binary.LittleEndian.Uint32(raw[off:]))
	off += 4
	if off+5 > len(raw) || string(raw[off:off+5]) != "FPVER" {
		return e, fmt.Errorf("missing FPVER tag")
	}
	off += 5
	if _, err := readStr(); err != nil {
		return e, err
	}
	if off+4 > len(raw) || string(raw[off:off+4]) != "Peak" {
		return e, fmt.Errorf("missing Peak tag")
	}
	off += 4
	if off+4 > len(raw) {
		return e, fmt.Errorf("truncated peak count")
	}
	n := int(binary.LittleEndian.Uint32(raw[off:]))
	off += 4
	if n < 0 || off+n*12 > len(raw) {
		return e, fmt.Errorf("truncated peaks")
	}
	e.Peaks = make([]Peak, n)
	for i := 0; i < n; i++ {
		e.Peaks[i] = Peak{
			Bin:  binary.LittleEndian.Uint32(raw[off:]),
			Time: binary.LittleEndian.Uint32(raw[off+4:]),
			Mag:  math.Float32frombits(binary.LittleEndian.Uint32(raw[off+8:])),
		}
		off += 12
	}
	return e, nil
}

// Supported reports whether this Core build can produce fingerprints NetEase
// still accepts (version string we know how to emit).
func Supported() bool { return Version == "hyai_1.2.0_client_1.0.0" }
