package recognize

import (
	"math"
)

const (
	magScale   = 14.190356447182975 // |FFT| / magScale ≈ wasm Peak.Mag for unit tones
	maxPksFrm  = 3                  // top peaks kept per frame after masking
	maskDecayT = 0.98
	maskDecayF = 0.95
	rawMagThr  = 10.0 // drop weak / numerical lobes; music peaks are typically ≫ 10
)

// ExtractPeaks runs the clean-room STFT peak picker on mono f32 PCM at 8 kHz.
// Parameters match those recovered from afp.wasm (see ALGORITHM.md). Peak lists
// are match-equivalent (same song via NetEase match) rather than byte-identical
// to KissFFT numerical artefacts.
func ExtractPeaks(pcm []float32) Envelope {
	n := len(pcm)
	dur := float32(n) / float32(SampleRate)
	if n < FFTLen {
		return Envelope{Duration: dur}
	}
	binL := int(math.Floor(FreqLB * float64(FFTLen) / float64(SampleRate)))
	binU := int(math.Floor(FreqUB * float64(FFTLen) / float64(SampleRate)))
	if binU > FFTLen/2 {
		binU = FFTLen / 2
	}
	win := make([]float64, FFTLen)
	for i := range win {
		win[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(FFTLen))
	}
	nBin := FFTLen/2 + 1
	mask := make([]float64, nBin)
	var peaks []Peak
	re := make([]float64, FFTLen)
	im := make([]float64, FFTLen)
	mag := make([]float64, nBin)

	type cand struct {
		mag float64
		bin int
	}
	for frame, off := 0, 0; off+FFTLen <= n; frame, off = frame+1, off+Hop {
		for i := 0; i < FFTLen; i++ {
			re[i] = float64(pcm[off+i]) * win[i]
			im[i] = 0
		}
		fft(re, im)
		for k := 0; k < nBin; k++ {
			mag[k] = math.Hypot(re[k], im[k])
		}
		var cs []cand
		hi := binU
		if hi >= nBin-1 {
			hi = nBin - 2
		}
		for f := binL; f <= hi; f++ {
			m := mag[f]
			if m < rawMagThr {
				continue
			}
			if m >= mag[f-1] && m >= mag[f+1] && m > mask[f] {
				cs = append(cs, cand{m, f})
			}
		}
		for i := 0; i < len(cs); i++ {
			for j := i + 1; j < len(cs); j++ {
				if cs[j].mag > cs[i].mag {
					cs[i], cs[j] = cs[j], cs[i]
				}
			}
		}
		if len(cs) > maxPksFrm {
			cs = cs[:maxPksFrm]
		}
		for _, c := range cs {
			if c.mag <= mask[c.bin] {
				continue
			}
			peaks = append(peaks, Peak{
				Bin:  uint32(c.bin),
				Time: uint32(frame),
				Mag:  float32(c.mag / magScale),
			})
			mask[c.bin] = c.mag
		}
		newMask := make([]float64, nBin)
		for f := 0; f < nBin; f++ {
			v := mask[f] * maskDecayT
			if f > 0 {
				if x := mask[f-1] * maskDecayF; x > v {
					v = x
				}
			}
			if f+1 < nBin {
				if x := mask[f+1] * maskDecayF; x > v {
					v = x
				}
			}
			newMask[f] = v
		}
		mask = newMask
	}
	return Envelope{Duration: dur, Peaks: peaks}
}

// fft is an in-place radix-2 Cooley–Tukey (re/im length = power of two).
func fft(re, im []float64) {
	n := len(re)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}
	for length := 2; length <= n; length <<= 1 {
		ang := -2 * math.Pi / float64(length)
		wlenRe, wlenIm := math.Cos(ang), math.Sin(ang)
		for i := 0; i < n; i += length {
			wRe, wIm := 1.0, 0.0
			half := length / 2
			for j := 0; j < half; j++ {
				uRe, uIm := re[i+j], im[i+j]
				vRe := re[i+j+half]*wRe - im[i+j+half]*wIm
				vIm := re[i+j+half]*wIm + im[i+j+half]*wRe
				re[i+j], im[i+j] = uRe+vRe, uIm+vIm
				re[i+j+half], im[i+j+half] = uRe-vRe, uIm-vIm
				nwRe := wRe*wlenRe - wIm*wlenIm
				wIm = wRe*wlenIm + wIm*wlenRe
				wRe = nwRe
			}
		}
	}
}
