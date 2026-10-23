import Accelerate
import Foundation
import QuartzCore

/// On-device beat estimate from the decoded PCM the MTAudioProcessingTap already sees (no extra decode, no downloads).
/// Per 1024-frame hop: mean-square energy → log envelope → half-wave-rectified flux (onset strength).
/// Beats: flux above an adaptive threshold (mean + 1.4σ over ~1 s) with a 280 ms refractory gap.
/// BPM: every ~2 s, autocorrelation of the last ~12 s of flux over 60–180 BPM lags (≈ 15k MACs — negligible).
/// Render thread: fixed-size buffers, no allocation, no locks except a tiny unfair lock to publish 3 numbers.
final class BeatAnalyzer {
    private let ringN = 512
    private var ring: [Float]
    private var head = 0, filled = 0
    private var acc: Float = 0, accN = 0
    private var prevE: Float = 0
    private var hop = 1024
    private var hopRate: Double = 43
    private var sinceBeat = 1_000
    private var sinceBPM = 0
    private var scratch: [Float]

    init() { ring = Array(repeating: 0, count: 512); scratch = Array(repeating: 0, count: 512) }

    func prepare(sampleRate: Double) {
        hop = sampleRate > 60_000 ? 2048 : 1024
        hopRate = sampleRate / Double(hop)
        head = 0; filled = 0; acc = 0; accN = 0; prevE = 0; sinceBeat = 1_000
        BeatStore.reset()
    }

    /// `p` = first channel samples (stride `st`).
    func feed(_ p: UnsafePointer<Float>, stride st: Int, frames n: Int) {
        var i = 0
        while i < n {
            let m = min(hop - accN, n - i)
            var ms: Float = 0
            vDSP_measqv(p + i * st, st, &ms, vDSP_Length(m))
            acc += ms * Float(m); accN += m; i += m
            if accN >= hop { step(acc / Float(accN)); acc = 0; accN = 0 }
        }
    }

    private func step(_ ms: Float) {
        let e = log1pf(ms * 1000)
        let flux = max(0, e - prevE)
        prevE = prevE * 0.6 + e * 0.4
        ring[head] = flux; head = (head + 1) % ringN; filled = min(filled + 1, ringN)
        sinceBeat += 1; sinceBPM += 1
        // adaptive threshold over the last ~1 s
        let w = min(filled, Int(hopRate))
        if w > 8 {
            var mean: Float = 0, sd: Float = 0
            let start = (head - w + ringN) % ringN
            if start + w <= ringN {
                ring.withUnsafeBufferPointer { b in vDSP_normalize(b.baseAddress! + start, 1, nil, 1, &mean, &sd, vDSP_Length(w)) }
            } else { mean = flux; sd = 0 }
            let gap = Int(0.28 * hopRate)
            if flux > mean + 1.4 * sd, flux > 0.02, sinceBeat > gap {
                sinceBeat = 0
                BeatStore.beat(energy: Double(min(1, e / 6)))
            }
        }
        if sinceBPM > Int(2 * hopRate), filled >= Int(6 * hopRate) { sinceBPM = 0; estimateBPM() }
    }

    private func estimateBPM() {
        // unroll ring into scratch (oldest → newest)
        let n = filled
        for k in 0..<n { scratch[k] = ring[(head - n + k + ringN) % ringN] }
        let lo = Int(hopRate * 60 / 180), hi = min(Int(hopRate * 60 / 60), n / 2)
        guard hi > lo else { return }
        var best: Float = 0, bestLag = 0
        scratch.withUnsafeBufferPointer { b in
            for lag in lo...hi {
                var d: Float = 0
                vDSP_dotpr(b.baseAddress!, 1, b.baseAddress! + lag, 1, &d, vDSP_Length(n - lag))
                // mild preference for 90–140 BPM (octave errors)
                let bpm = hopRate * 60 / Double(lag)
                let wgt: Float = (bpm >= 85 && bpm <= 145) ? 1.15 : 1
                if d * wgt > best { best = d * wgt; bestLag = lag }
            }
        }
        if bestLag > 0 { BeatStore.tempo(hopRate * 60 / Double(bestLag)) }
    }
}

/// Published beat state (any thread). UI reads it inside existing 30 fps TimelineViews only.
enum BeatStore {
    private static var lock = os_unfair_lock()
    private static var lastBeat: CFTimeInterval = 0
    private static var bpm: Double = 0
    private static var energy: Double = 0
    /// AVPlayer pulls the tap ahead of the speaker; shift pulses by roughly that lead.
    static var outputLead: CFTimeInterval = 0.12

    static func reset() { os_unfair_lock_lock(&lock); lastBeat = 0; bpm = 0; energy = 0; os_unfair_lock_unlock(&lock) }
    static func beat(energy e: Double) {
        let t = CACurrentMediaTime() + outputLead
        os_unfair_lock_lock(&lock); lastBeat = t; energy = energy * 0.7 + e * 0.3; os_unfair_lock_unlock(&lock)
    }
    static func tempo(_ b: Double) { os_unfair_lock_lock(&lock); bpm = bpm == 0 ? b : bpm * 0.7 + b * 0.3; os_unfair_lock_unlock(&lock) }

    struct Reading { let pulse: Double; let bpm: Double; let energy: Double; let live: Bool }
    /// pulse: 1 right on a beat, decaying (~150 ms) to 0. live = beats seen in the last 3 s.
    static func read(now: CFTimeInterval = CACurrentMediaTime()) -> Reading {
        os_unfair_lock_lock(&lock); let lb = lastBeat, b = bpm, e = energy; os_unfair_lock_unlock(&lock)
        let dt = now - lb
        let live = lb > 0 && dt < 3
        return Reading(pulse: live && dt >= 0 ? exp(-dt * 7) : 0, bpm: b, energy: e, live: live)
    }

    /// Doodle / background pulse 0…1. Server BPM (platform-provided) wins for tempo, phase-locked to the last
    /// detected onset when we have one; otherwise the on-device beat pulses; otherwise nothing.
    static func pulse(serverBPM: Double?, now: CFTimeInterval = CACurrentMediaTime()) -> Double {
        let r = read(now: now)
        if let s = serverBPM, s > 0 {
            os_unfair_lock_lock(&lock); let lb = lastBeat; os_unfair_lock_unlock(&lock)
            let period = 60 / s
            let phase = lb > 0 ? (now - lb).truncatingRemainder(dividingBy: period) : now.truncatingRemainder(dividingBy: period)
            return exp(-max(0, phase) * 7)
        }
        return r.pulse
    }
}
