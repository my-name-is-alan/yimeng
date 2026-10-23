// Package transcode provides optional on-demand transcoding through an
// external ffmpeg binary, with an on-disk LRU cache and progressive delivery
// of first-time transcodes. Only whitelisted profiles are accepted: user input
// never reaches the ffmpeg command line except as a validated enum/integer.
package transcode

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
)

const Original = "original"

var ErrBadProfile = errors.New("unsupported stream format/bitrate")

type spec struct {
	encoder     string // ffmpeg encoder name
	muxer       string // ffmpeg output format (streamable containers only)
	ext         string
	contentType string
	bitrates    []int // kbps; empty = lossless
	def         int
}

var specs = map[string]spec{
	"mp3":  {encoder: "libmp3lame", muxer: "mp3", ext: "mp3", contentType: "audio/mpeg", bitrates: []int{128, 192, 256, 320}, def: 320},
	"aac":  {encoder: "aac", muxer: "adts", ext: "aac", contentType: "audio/aac", bitrates: []int{128, 192, 256}, def: 256},
	"opus": {encoder: "libopus", muxer: "ogg", ext: "opus", contentType: "audio/ogg; codecs=opus", bitrates: []int{96, 128, 160, 192}, def: 160},
	"flac": {encoder: "flac", muxer: "flac", ext: "flac", contentType: "audio/flac"},
}

// Profile is a validated output profile. The zero value / Format "original" means no transcoding.
type Profile struct {
	Format      string `json:"format"`
	BitrateKbps int    `json:"bitrate_kbps,omitempty"`
}

func (p Profile) IsOriginal() bool { return p.Format == "" || p.Format == Original }

// Key is a filesystem/ETag-safe identifier, e.g. "mp3-320", "flac".
func (p Profile) Key() string {
	if p.IsOriginal() {
		return Original
	}
	if p.BitrateKbps == 0 {
		return p.Format
	}
	return p.Format + "-" + strconv.Itoa(p.BitrateKbps)
}

func (p Profile) ContentType() string { return specs[p.Format].contentType }

func (p Profile) Lossless() bool { return p.Format == "flac" }

// ParseProfile validates format/bitrate against the whitelist. An empty
// bitrate selects the format's default preset.
func ParseProfile(format, bitrate string) (Profile, error) {
	if format == "" || format == Original {
		if bitrate != "" {
			return Profile{}, fmt.Errorf("%w: bitrate requires a lossy format", ErrBadProfile)
		}
		return Profile{Format: Original}, nil
	}
	sp, ok := specs[format]
	if !ok {
		return Profile{}, fmt.Errorf("%w: format must be original|aac|mp3|opus|flac", ErrBadProfile)
	}
	p := Profile{Format: format}
	if len(sp.bitrates) == 0 {
		if bitrate != "" {
			return Profile{}, fmt.Errorf("%w: %s is lossless, bitrate not allowed", ErrBadProfile, format)
		}
		return p, nil
	}
	if bitrate == "" {
		p.BitrateKbps = sp.def
		return p, nil
	}
	n, err := strconv.Atoi(bitrate)
	if err != nil || !slices.Contains(sp.bitrates, n) {
		return Profile{}, fmt.Errorf("%w: %s bitrate must be one of %v", ErrBadProfile, format, sp.bitrates)
	}
	p.BitrateKbps = n
	return p, nil
}

// FormatInfo describes a selectable format for clients.
type FormatInfo struct {
	Format   string `json:"format"`
	Lossless bool   `json:"lossless"`
	Bitrates []int  `json:"bitrates,omitempty"`
	Default  int    `json:"default_bitrate,omitempty"`
}

// args builds the ffmpeg argument vector. in/out are paths resolved by Core
// (never request input); the "file:" prefix stops protocol/option parsing.
func (p Profile) args(in, out string) []string {
	sp := specs[p.Format]
	a := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y",
		"-i", "file:" + in, "-map", "0:a:0", "-vn", "-sn", "-dn", "-c:a", sp.encoder}
	if p.BitrateKbps > 0 {
		a = append(a, "-b:a", strconv.Itoa(p.BitrateKbps)+"k")
	}
	if p.Format == "mp3" {
		a = append(a, "-id3v2_version", "3")
	}
	return append(a, "-f", sp.muxer, "file:"+out)
}
