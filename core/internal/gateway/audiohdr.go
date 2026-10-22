package gateway

import (
	"net/http"
	"strconv"

	"github.com/my-name-is-alan/yimeng/core/internal/library"
)

// Audio stream headers (D45): what the client is about to decode.
//
//	X-Yimeng-Codec: flac | alac | mp3 | aac | opus | vorbis | …
//	X-Yimeng-SampleRate / X-Yimeng-BitDepth / X-Yimeng-Channels / X-Yimeng-Bitrate-Kbps
//
// Unknown values are omitted (online sources rarely report rate/depth).

var formatCodec = map[string]string{"mp3": "mp3", "m4a": "aac", "aac": "aac", "opus": "opus", "ogg": "vorbis", "flac": "flac", "wav": "pcm", "aiff": "pcm"}

func setAudioHeaders(h http.Header, codec string, rate, depth, ch, kbps int) {
	set := func(k string, v int) {
		if v > 0 {
			h.Set(k, strconv.Itoa(v))
		}
	}
	if codec != "" {
		h.Set("X-Yimeng-Codec", codec)
	}
	set("X-Yimeng-SampleRate", rate)
	set("X-Yimeng-BitDepth", depth)
	set("X-Yimeng-Channels", ch)
	set("X-Yimeng-Bitrate-Kbps", kbps)
	h.Set("Access-Control-Expose-Headers", "X-Yimeng-Codec, X-Yimeng-SampleRate, X-Yimeng-BitDepth, X-Yimeng-Channels, X-Yimeng-Bitrate-Kbps, X-Yimeng-Entitlement, X-Yimeng-Preview-Start-Ms, X-Yimeng-Preview-End-Ms, X-Yimeng-Play-Id, X-Stream-Profile")
}

// localAudioHeaders describes a local file, or what a transcode turns it into.
func localAudioHeaders(h http.Header, s library.StreamInfo, format string, kbps int) {
	if format == "" { // original
		codec := s.Codec
		if codec == "" {
			codec = formatCodec[s.Format]
		}
		setAudioHeaders(h, codec, s.SampleRate, s.BitDepth, s.Channels, s.BitrateKbps)
		return
	}
	rate, depth := s.SampleRate, 0
	switch format {
	case "opus":
		rate = 48000
	case "mp3", "m4a", "aac":
		if rate > 48000 {
			rate = 48000
		}
	case "flac":
		depth = s.BitDepth
		if depth > 24 {
			depth = 24
		}
	}
	setAudioHeaders(h, formatCodec[format], rate, depth, s.Channels, kbps)
}
