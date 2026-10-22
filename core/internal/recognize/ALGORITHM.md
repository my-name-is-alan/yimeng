# NetEase 听歌识曲 fingerprint (clean-room notes)

Observed from NetEase's official Chrome extension (`kemcalcncfhmdkgglekijclbomdoohkp`
v1.0.4) `afp.wasm` **outside this repository**, by feeding known PCM and
inspecting exports / memory. No wasm binary is shipped or executed by Core.

## Pipeline (own words)

1. **PCM** — mono float32, 8 kHz. The extension accumulates ~6 s (48 000
   samples) before calling `ExtractQueryFP`. Shorter clips are accepted; the
   duration field in the payload is `samples/8000`.

2. **Spectrogram** — KissFFT-based `SpecCalculator`. Frame length equals FFT
   length 2048 (`freqResolution = 8000/2048 = 3.90625 Hz/bin`). Hop 160 samples
   (frame index `i` starts at sample `i*160`). Hann window. Analysis band
   `freqLB=100` … `freqUB=4000` Hz (bins 26…1024).

3. **Peaks** — `Peak` records of 12 bytes: `uint32 freqBin`, `uint32 frame`,
   `float32 magnitude` (little-endian). Silence yields zero peaks. An impulse
   can emit up to ~999 peaks in one frame (`maxPksPerFrm`). Magnitude on the
   wire is `|FFT| / 14.190…` (empirical scale matching pure-tone vectors).
   Our clean-room picker keeps plateau-friendly local maxima in the analysis
   band, applies a per-bin hold of 1 frame (reproduces the even-frame pattern
   on steady tones), and drops bins below a raw-FFT floor of 1. KissFFT
   numerical side-lobes present in wasm vectors are not reproduced; NetEase
   match is verified live (match-equivalent). Pairing distances `intervalT=8`
   / `intervalF=30` appear in the wasm strings but the wire format stores
   peaks, not combinatorial hashes.

4. **Envelope**
   ```
   uint32 verLen; char ver[verLen];     // "hyai_1.2.0_client_1.0.0"
   uint64 zero;
   float32 durationSec;
   char "FPVER";
   uint32 verLen; char ver[verLen];     // same version again
   char "Peak";
   uint32 peakCount;
   Peak peaks[peakCount];
   ```

5. **Packaging** — zlib default compress the envelope, PKCS#7-pad to 16 bytes,
   AES-128-ECB with ASCII key `4B97221F27F02907` (a 1-byte rotation of the
   hex string `074B97221F27F029` embedded in the wasm). Base64 of the ciphertext
   is `rawdata` for `POST …/eapi/music/audio/match` with
   `algorithmCode=shazam_v2`.

## Versioning

`FPVER` / `hyai_1.2.0_client_1.0.0` are checked after a successful decode of a
self-roundtrip. If NetEase starts rejecting our fingerprints or the embedded
version string we expect is absent from a probe, Core returns **503**
`fingerprint_unsupported` so clients can fall back.

## Verification

- Packaging: byte-identical to wasm output when given the same peak list
  (test vectors under `testdata/`, generated offline).
- Peak extractor: match-equivalent — same song returned by NetEase for a clip
  fingerprinted by wasm vs by Core (live check with yimeng-test).

Live verification (yimeng-test, full-entitlement clip): NetEase returns the same
song for wasm fingerprints and for Core peaks produced with maskDecayT=0.98,
maskDecayF=0.95, maxPksPerFrm=3, raw |FFT| floor 10. Match transport is a plain
form POST to `interface.music.163.com/api/music/audio/match` with `decrypt=1`
(as the official extension does), not eapi.
