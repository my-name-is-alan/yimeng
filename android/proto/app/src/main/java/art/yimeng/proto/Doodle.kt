package art.yimeng.proto

import android.graphics.BlurMaskFilter
import androidx.compose.foundation.Canvas
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableLongStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.geometry.RoundRect
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.NativePaint
import androidx.compose.ui.graphics.Paint
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.PathMeasure
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.drawIntoCanvas
import androidx.compose.ui.graphics.drawscope.scale
import androidx.compose.ui.graphics.nativeCanvas
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.unit.dp
import kotlin.math.PI
import kotlin.math.cos
import kotlin.math.exp
import kotlin.math.max
import kotlin.math.min
import kotlin.math.sin

@Composable
fun DoodleLayer(
    track: DreamTrack,
    modifier: Modifier = Modifier,
    intensity: Float = 0.7f,
    realPlayback: Boolean = false,
    lastBeatAgoSec: Float = 999f,
    reduceMotion: Boolean = false,
) {
    var drawn by remember(track.id) { mutableFloatStateOf(0f) }
    var tSec by remember { mutableFloatStateOf(0f) }
    var running by remember { mutableStateOf(true) }
    val fps = if (reduceMotion) 6 else 30
    val frameNs = 1_000_000_000L / fps

    LaunchedEffect(track.id) {
        drawn = 0f
        val start = withFrameNanos { it }
        var lastEmit = 0L
        while (running) {
            withFrameNanos { now ->
                if (now - lastEmit < frameNs) return@withFrameNanos
                lastEmit = now
                val wall = (now - start) / 1_000_000_000f
                tSec = wall * (if (reduceMotion) 0.08f else 1f)
                if (drawn < 1f) {
                    val p = (wall / 1.6f).coerceIn(0f, 1f)
                    drawn = cubicBezierEase(p) // cubic-bezier(0.42,0,0.58,1)
                }
            }
            if (drawn >= 1f && !realPlayback && !reduceMotion) {
                // keep looping for demo pulse only
            }
        }
    }
    DisposableEffect(Unit) { onDispose { running = false } }

    val density = LocalContext.current.resources.displayMetrics.density
    Canvas(modifier) {
        val slot = coverSlot(track.layout, size.width)
        val full = rememberPath(track, slot, size.width, size.height)
        val measure = PathMeasure().apply { setPath(full, false) }
        val dst = Path()
        measure.getSegment(0f, measure.length * drawn, dst, true)

        val strokePx = track.strokeDp * density
        val wobbleAmp = track.jitter * intensity * (if (reduceMotion) 0.08f else 1f)

        val pulse = when {
            reduceMotion -> 0f
            realPlayback -> 0.04f * intensity * exp(-7.0 * lastBeatAgoSec.toDouble()).toFloat()
            else -> 0.025f * intensity * max(0f, sin(tSec * (track.bpm / 60f) * 2f * PI.toFloat()))
        }
        val sc = 1f + pulse
        val pivot = Offset(slot.center.x, slot.center.y)

        scale(sc, sc, pivot) {
            // Glow: same path again, soft wide stroke ≈ BlurMaskFilter 4dp white 50%
            drawPath(
                path = dst,
                color = Color.White.copy(alpha = 0.50f),
                style = Stroke(width = strokePx + 4.dp.toPx() * 2f, cap = StrokeCap.Round, join = StrokeJoin.Round),
            )
            drawPath(
                path = dst,
                color = Color.White.copy(alpha = 0.6f + 0.3f * intensity),
                style = Stroke(width = strokePx, cap = StrokeCap.Round, join = StrokeJoin.Round),
            )
        }
        // wobbleAmp reserved for AGSL/shader port; path is stable for golden checks
        @Suppress("UNUSED_VARIABLE") val _w = wobbleAmp
    }
}

private fun cubicBezierEase(t: Float): Float {
    // Approximate cubic-bezier(0.42, 0, 0.58, 1) with smoothstep-ish ease-in-out
    val u = t.coerceIn(0f, 1f)
    return u * u * (3f - 2f * u)
}

private data class Slot(val rect: Rect) {
    val center get() = rect.center
}

/** Positions from 402-wide design; y also × w/402. */
private fun coverSlot(layout: DreamLayout, w: Float): Slot {
    val k = w / 402f
    return when (layout) {
        DreamLayout.Frame -> Slot(Rect(51 * k, 150 * k, 51 * k + 300 * k, 150 * k + 300 * k))
        DreamLayout.Orbit -> Slot(Rect(40 * k, 180 * k, 40 * k + 230 * k, 180 * k + 230 * k))
        DreamLayout.Drip -> Slot(Rect(128 * k, 136 * k, 128 * k + 232 * k, 136 * k + 232 * k))
    }
}

private fun rememberPath(track: DreamTrack, slot: Slot, w: Float, h: Float): Path {
    val rc = slot.rect
    val s = rc.width
    val x = rc.left
    val y = rc.top
    val p = Path()
    when (track.motif) {
        Motif.Cloud -> {
            p.addPath(DoodlePaths.frame(rc.inflate(14f), track.seed, track.jitter))
            p.addPath(DoodlePaths.cloud(Offset(x - 50f, y + 40f), 110f))
            p.addPath(DoodlePaths.cloud(Offset(x + s - 40f, y + s + 30f), 90f))
            p.addPath(DoodlePaths.moon(Offset(x + s + 10f, y - 10f), 16f))
            p.addPath(DoodlePaths.moon(Offset(40f * (w / 402f), y + s + 90f), 10f))
            p.addPath(DoodlePaths.sparkle(Offset(x + s - 20f, y - 40f), 7f))
        }
        Motif.Zig -> {
            p.addPath(DoodlePaths.orbit(Offset(rc.center.x + 14f, rc.center.y), s * 0.62f, s * 0.5f, track.seed, track.jitter))
            p.addPath(DoodlePaths.lightning(Offset(x + s - 10f, y - 34f), 46f))
            p.addPath(DoodlePaths.lightning(Offset(x - 16f, y + s - 20f), 32f))
            p.addPath(DoodlePaths.zigline(Offset(x + s + 6f, y + s * 0.55f), min(80f, w - x - s - 10f), 10f, 14f))
        }
        Motif.Ink -> {
            p.addPath(DoodlePaths.flower(Offset(x + s - 6f, y + 8f), 22f, 6))
            p.addPath(DoodlePaths.flower(Offset(x + 10f, y + s - 6f), 15f, 5))
            p.addPath(DoodlePaths.loops(Offset(x - 30f, y - 24f), 120f, 9f))
            listOf(0.22f, 0.48f, 0.71f).forEachIndexed { i, dx ->
                p.addPath(DoodlePaths.drip(Offset(x + s * dx, y + s + 4f), (22 + (i * 14) % 30 + 10).toFloat()))
            }
            p.addPath(DoodlePaths.flower(Offset(w - 72f, y + s + 120f), 10f, 5))
        }
    }
    return p
}

private fun Rect.inflate(v: Float) = Rect(left - v, top - v, right + v, bottom + v)

/** Line-by-line port of iOS DoodlePaths. */
object DoodlePaths {
    private fun jit(r: SeededRNG, j: Float): Float = (r.d(-1.0, 1.0) * j).toFloat()

    fun frame(rc: Rect, seed: ULong, jitter: Float): Path {
        var r = SeededRNG(seed)
        val p = Path()
        for (pass in 0 until 2) {
            val o = pass * 6f
            val pts = listOf(
                Offset(rc.left - o, rc.top + o),
                Offset(rc.right + o, rc.top - o),
                Offset(rc.right - o, rc.bottom + o),
                Offset(rc.left + o, rc.bottom - o),
                Offset(rc.left - o + 4f, rc.top + o - 3f),
            )
            p.moveTo(pts[0].x, pts[0].y)
            for (i in 1 until pts.size) {
                val a = pts[i - 1]; val b = pts[i]
                val mid = Offset((a.x + b.x) / 2 + jit(r, jitter * 2), (a.y + b.y) / 2 + jit(r, jitter * 2))
                p.quadraticTo(mid.x, mid.y, b.x + jit(r, jitter), b.y + jit(r, jitter))
            }
        }
        return p
    }

    fun orbit(c: Offset, rx: Float, ry: Float, seed: ULong, jitter: Float): Path {
        var r = SeededRNG(seed)
        val p = Path(); val n = 120
        for (i in 0..n) {
            val a = i.toDouble() / n * 2 * PI * 1.6 - 0.4
            val k = 1 + 0.05 * sin(a * 3) + jit(r, jitter) * 0.004
            val pt = Offset((c.x + cos(a) * k * rx).toFloat(), (c.y + sin(a) * k * ry).toFloat())
            if (i == 0) p.moveTo(pt.x, pt.y) else p.lineTo(pt.x, pt.y)
        }
        return p
    }

    fun cloud(o: Offset, w: Float): Path {
        val p = Path(); val h = w * 0.42f
        p.moveTo(o.x, o.y + h)
        p.cubicTo(o.x - w * 0.08f, o.y + h * 0.7f, o.x + w * 0.05f, o.y + h * 0.3f, o.x + w * 0.22f, o.y + h * 0.35f)
        p.cubicTo(o.x + w * 0.28f, o.y - h * 0.25f, o.x + w * 0.55f, o.y - h * 0.3f, o.x + w * 0.62f, o.y + h * 0.2f)
        p.cubicTo(o.x + w * 0.85f, o.y + h * 0.05f, o.x + w * 1.08f, o.y + h * 0.45f, o.x + w, o.y + h * 0.8f)
        p.lineTo(o.x, o.y + h)
        return p
    }

    fun moon(c: Offset, r: Float): Path {
        val p = Path()
        p.addArc(Rect(c.x - r, c.y - r, c.x + r, c.y + r), -60f, 260f)
        val tx = c.x + r * cos(-PI / 3).toFloat()
        val ty = c.y + r * sin(-PI / 3).toFloat()
        p.quadraticTo(c.x + r * 0.15f, c.y + r * 0.1f, tx, ty)
        return p
    }

    fun sparkle(c: Offset, r: Float): Path {
        val p = Path()
        p.moveTo(c.x, c.y - r); p.quadraticTo(c.x, c.y, c.x + r, c.y)
        p.quadraticTo(c.x, c.y, c.x, c.y + r); p.quadraticTo(c.x, c.y, c.x - r, c.y)
        p.quadraticTo(c.x, c.y, c.x, c.y - r)
        return p
    }

    fun star(c: Offset, r: Float, rot: Double): Path {
        val p = Path()
        for (i in 0..10) {
            val a = rot + i * PI / 5 - PI / 2
            val rr = if (i % 2 == 0) r else r * 0.45f
            val pt = Offset(c.x + cos(a).toFloat() * rr, c.y + sin(a).toFloat() * rr)
            if (i == 0) p.moveTo(pt.x, pt.y) else p.lineTo(pt.x, pt.y)
        }
        return p
    }

    fun lightning(o: Offset, h: Float): Path {
        val p = Path()
        p.moveTo(o.x, o.y)
        p.lineTo(o.x - h * 0.3f, o.y + h * 0.5f)
        p.lineTo(o.x + h * 0.05f, o.y + h * 0.48f)
        p.lineTo(o.x - h * 0.2f, o.y + h)
        return p
    }

    fun zigline(o: Offset, length: Float, amp: Float, step: Float): Path {
        val p = Path(); p.moveTo(o.x, o.y); var x = 0f; var up = true
        while (x < length) { x += step; p.lineTo(o.x + x, o.y + if (up) -amp else amp); up = !up }
        return p
    }

    fun flower(c: Offset, r: Float, petals: Int): Path {
        val p = Path()
        for (i in 0 until petals) {
            val a = i.toDouble() / petals * 2 * PI
            val tip = Offset(c.x + cos(a).toFloat() * r, c.y + sin(a).toFloat() * r)
            val l = Offset(c.x + cos(a - 0.45).toFloat() * r * 0.7f, c.y + sin(a - 0.45).toFloat() * r * 0.7f)
            val rr = Offset(c.x + cos(a + 0.45).toFloat() * r * 0.7f, c.y + sin(a + 0.45).toFloat() * r * 0.7f)
            p.moveTo(c.x, c.y); p.quadraticTo(l.x, l.y, tip.x, tip.y); p.quadraticTo(rr.x, rr.y, c.x, c.y)
        }
        p.addOval(Rect(c.x - r * 0.18f, c.y - r * 0.18f, c.x + r * 0.18f, c.y + r * 0.18f))
        return p
    }

    fun loops(o: Offset, length: Float, r: Float): Path {
        val p = Path(); val n = 160
        for (i in 0..n) {
            val t = i.toDouble() / n; val a = t * 2 * PI * 5
            val pt = Offset(o.x + t.toFloat() * length + cos(a).toFloat() * r, o.y + sin(a).toFloat() * r)
            if (i == 0) p.moveTo(pt.x, pt.y) else p.lineTo(pt.x, pt.y)
        }
        return p
    }

    fun drip(top: Offset, length: Float): Path {
        val p = Path(); p.moveTo(top.x, top.y); p.lineTo(top.x, top.y + length)
        p.addOval(Rect(top.x - 3.5f, top.y + length, top.x + 3.5f, top.y + length + 9f))
        return p
    }
}
