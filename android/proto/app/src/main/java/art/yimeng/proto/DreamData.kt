package art.yimeng.proto

import androidx.compose.ui.graphics.Color

enum class Motif { Cloud, Zig, Ink }
enum class DreamLayout { Frame, Orbit, Drip }

data class DreamTrack(
    val id: String,
    val title: String,
    val artist: String,
    val base: Color,
    val blobs: List<Color>, // 4
    val bpm: Int,
    val energy: Double,
    val genre: String,
) {
    val motif: Motif
        get() = when {
            energy > 0.7 -> Motif.Zig
            genre in setOf("folk", "acoustic", "jazz") -> Motif.Ink
            else -> Motif.Cloud
        }
    val layout: DreamLayout
        get() = when (motif) {
            Motif.Zig -> DreamLayout.Orbit
            Motif.Ink -> DreamLayout.Drip
            Motif.Cloud -> DreamLayout.Frame
        }
    val density: Int get() = (4 + energy * 8).toInt()
    val strokeDp: Float get() = (1.6 + energy * 1.4).toFloat() // default ~2.23 at 0.45
    val jitter: Float get() = (1.5 + energy * 2.5).toFloat()
    val seed: ULong
        get() = id.fold(0UL) { acc, c -> acc + c.code.toULong() }
}

/** SplitMix64 — byte-compatible with iOS SeededRNG. */
class SeededRNG(seed: ULong) {
    private var state: ULong = seed + 0x9E3779B97F4A7C15uL
    fun next(): ULong {
        state += 0x9E3779B97F4A7C15uL
        var z = state
        z = (z xor (z shr 30)) * 0xBF58476D1CE4E5B9uL
        z = (z xor (z shr 27)) * 0x94D049BB133111EBuL
        return z xor (z shr 31)
    }
    fun d(a: Double, b: Double): Double {
        val u = (next() shr 11).toDouble() / (1L shl 53).toDouble()
        return a + (b - a) * u
    }
}

object DreamMock {
    val orange = DreamTrack(
        id = "ym-0099", title = "橘子海", artist = "陈粒",
        base = Color(0xFF7A2A00),
        blobs = listOf(Color(0xFFFF7A1A), Color(0xFFFFD23F), Color(0xFFFF3D2E), Color(0xFFFFB000)),
        bpm = 96, energy = 0.55, genre = "acoustic",
    )
    val zig = DreamTrack(
        id = "ym-1187", title = "紫色黄昏", artist = "不才",
        base = Color(0xFF2A1060),
        blobs = listOf(Color(0xFFFF5FA8), Color(0xFFFFD36B), Color(0xFF6B2CFF), Color(0xFFFF7AC0)),
        bpm = 128, energy = 0.85, genre = "electronic",
    )
    val cloud = DreamTrack(
        id = "ym-0412", title = "云朵来信", artist = "王菲",
        base = Color(0xFF7A2240),
        blobs = listOf(Color(0xFFFF6A88), Color(0xFFFFB199), Color(0xFFFF9A5A), Color(0xFFFFD3C4)),
        bpm = 68, energy = 0.3, genre = "ballad",
    )
}
