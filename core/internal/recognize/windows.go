package recognize

// MatchWindow is one PCM slice to fingerprint. Duration is always derived from
// the slice length (samples / SampleRate); callers must not override it with a
// client hint.
type MatchWindow struct {
	Label string
	PCM   []float32
}

// MatchWindows returns a deterministic list of PCM windows to try against
// NetEase. Same input always yields the same windows (no randomness).
//
// Order: full clip first (best when the client sent a clean ≤15 s take), then
// fixed 6 s slices (the official extension accumulates ~6 s), then a half-hop
// phase offset on the full clip to absorb framing near decision boundaries.
func MatchWindows(pcm []float32) []MatchWindow {
	if len(pcm) == 0 {
		return nil
	}
	out := []MatchWindow{{Label: "full", PCM: pcm}}
	six := SampleRate * 6
	if len(pcm) > six+Hop {
		out = append(out,
			MatchWindow{Label: "first6s", PCM: pcm[:six]},
			MatchWindow{Label: "last6s", PCM: pcm[len(pcm)-six:]},
		)
		mid := (len(pcm) - six) / 2
		out = append(out, MatchWindow{Label: "mid6s", PCM: pcm[mid : mid+six]})
	}
	// Half-hop offset: peaks near rawMagThr / mask edges can flip by one frame.
	off := Hop / 2
	if len(pcm) > FFTLen+off {
		out = append(out, MatchWindow{Label: "full+80", PCM: pcm[off:]})
	}
	return out
}
