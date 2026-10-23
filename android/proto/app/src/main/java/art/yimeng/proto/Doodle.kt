package art.yimeng.proto

import android.graphics.BlurMaskFilter
import androidx.compose.foundation.Canvas
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.graphics.BlendMode
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Paint
import androidx.compose.ui.graphics.Path
import android.graphics.PathMeasure as AndroidPathMeasure
import androidx.compose.ui.graphics.PathEffect
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.asAndroidPath
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.drawIntoCanvas
import androidx.compose.ui.graphics.nativeCanvas
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.unit.dp
import kotlin.math.PI
import kotlin.math.abs
import kotlin.math.cos
import kotlin.math.exp
import kotlin.math.floor
import kotlin.math.max
import kotlin.math.min
import kotlin.math.sin

/** Cover placement in a 402-pt-wide design canvas (scaled to width at runtime). Mirrors iOS CoverSlot. */
data class CoverSlot(val rect: Rect, val radius: Float) {
    val center get() = rect.center
}

fun coverSlot(layout: DreamLayout, w: Float): CoverSlot {
    val k = w / 402f
    return when (layout) {
        DreamLayout.Frame -> CoverSlot(Rect(51 * k, 150 * k, 51 * k + 300 * k, 150 * k + 300 * k), 10f * k)
        DreamLayout.Orbit -> CoverSlot(Rect(40 * k, 180 * k, 40 * k + 230 * k, 180 * k + 230 * k), 999f)
        DreamLayout.Drip -> CoverSlot(Rect(128 * k, 136 * k, 128 * k + 232 * k, 136 * k + 232 * k), 24f * k)
    }
}

/** Mutable orbit phase — wall-clock advance; BPM only steers speed (lerped). Never hard-lock to elapsed. */
private class DoodleOrbitClock {
    var phase: Double = 0.0
    var lastTickNs: Long = 0L
    var smoothedBPM: Double = 0.0
    var starOffsets: DoubleArray = doubleArrayOf(0.0, 0.17, 0.41, 0.63, 0.88)
    var starRates: DoubleArray = doubleArrayOf(1.0, 0.92, 1.08, 0.85, 1.12)
}

private val ChaseColors = listOf(
    Color(0xFFC4F542), // lemon
    Color(0xFF5EEAD4), // cyan
    Color(0xFFFF7AB8), // pink
    Color(0xFF9B6DFF), // violet
)

/**
 * E+G doodle (iOS 0.3.8/19): static base strokes + chase trim + staggered star trail.
 * NO whole-layer scale / white flash. Phase = wall clock; BPM lerps into angular velocity (~0.6s).
 */
@Composable
fun DoodleLayer(
    track: DreamTrack,
    modifier: Modifier = Modifier,
    intensity: Float = 0.7f,
    playing: Boolean = true,
    beat: Boolean = true,
    reduceMotion: Boolean = false,
    /** Optional absolute slot; default = DreamLayout.slot(canvas width). */
    slotOverride: CoverSlot? = null,
    // Legacy ProtoScreen knobs (kept so call sites compile).
    realPlayback: Boolean = false,
    lastBeatAgoSec: Float = 999f,
) {
    var drawn by remember(track.id) { mutableFloatStateOf(0f) }
    var tSec by remember { mutableFloatStateOf(0f) }
    var phase by remember(track.id) { mutableFloatStateOf(0f) }
    var beatAmt by remember { mutableFloatStateOf(0f) }
    var running by remember { mutableStateOf(true) }
    val clock = remember(track.id) {
        DoodleOrbitClock().also { c ->
            c.smoothedBPM = targetBpm(track)
            val rng = SeededRNG(track.seed + 404uL)
            c.starOffsets = DoubleArray(5) { rng.d(0.0, 1.0) }
            c.starRates = DoubleArray(5) { rng.d(0.82, 1.18) }
        }
    }
    val fps = if (reduceMotion) 6 else 60
    val frameNs = 1_000_000_000L / fps
    val calm = reduceMotion

    LaunchedEffect(track.id, playing, beat, calm) {
        drawn = 0f
        clock.lastTickNs = 0L
        val start = withFrameNanos { it }
        var lastEmit = 0L
        while (running) {
            withFrameNanos { now ->
                if (now - lastEmit < frameNs) return@withFrameNanos
                lastEmit = now
                val wall = (now - start) / 1_000_000_000f
                tSec = wall * (if (calm) 0.08f else 1f)
                if (drawn < 1f) {
                    val p = (wall / 1.6f).coerceIn(0f, 1f)
                    drawn = cubicBezierEase(p)
                }
                if (clock.lastTickNs == 0L) {
                    clock.lastTickNs = now
                    clock.smoothedBPM = targetBpm(track)
                }
                val dt = min(0.05, max(0.0, (now - clock.lastTickNs) / 1_000_000_000.0))
                clock.lastTickNs = now
                val target = targetBpm(track)
                val bpmBlend = 1.0 - exp(-dt / 0.6)
                clock.smoothedBPM += (target - clock.smoothedBPM) * bpmBlend
                val lap = lapSeconds(clock.smoothedBPM)
                if (playing && !calm) {
                    clock.phase += dt / lap
                }
                phase = (clock.phase % 1.0).toFloat()
                beatAmt = if (!beat || calm) 0f else {
                    val bpm = if (clock.smoothedBPM > 0) clock.smoothedBPM else target
                    val raw = max(0.0, sin(tSec * bpm / 60.0 * 2.0 * PI)) * 0.55
                    val soft = raw * raw * (3 - 2 * raw)
                    (intensity * min(1.0, soft)).toFloat()
                }
            }
        }
    }
    DisposableEffect(Unit) { onDispose { running = false } }

    val density = LocalDensity.current.density
    Canvas(modifier) {
        val slot = slotOverride ?: coverSlot(track.layout, size.width)
        val full = motifPath(track, slot, size.width, size.height)
        val trimmed = trimAllContours(full, drawn)
        val wobbleAmp = track.jitter * intensity * (if (calm) 0.08f else 0.35f)
        val dst = if (wobbleAmp > 0.01f) wobblePath(trimmed, wobbleAmp, tSec) else trimmed
        val strokePx = track.strokeDp * density
        val strokeOpacity = (0.55f + 0.25f * intensity) * 0.85f

        drawIntoCanvas { canvas ->
            val glow = Paint().asFrameworkPaint().apply {
                isAntiAlias = true
                style = android.graphics.Paint.Style.STROKE
                strokeWidth = strokePx
                strokeCap = android.graphics.Paint.Cap.ROUND
                strokeJoin = android.graphics.Paint.Join.ROUND
                color = android.graphics.Color.argb(90, 255, 255, 255)
                maskFilter = BlurMaskFilter(4.dp.toPx(), BlurMaskFilter.Blur.NORMAL)
            }
            canvas.nativeCanvas.drawPath(dst.asAndroidPath(), glow)
        }
        drawPath(
            path = dst,
            color = Color.White.copy(alpha = strokeOpacity),
            style = Stroke(width = strokePx, cap = StrokeCap.Round, join = StrokeJoin.Round),
        )

        if (!calm && drawn > 0.05f) {
            val orbit = beatOrbit(track, slot)
            val run = phase.toDouble()
            val hot = beatAmt.toDouble()
            val col = chaseColor(run * 1.35)
            val chaseW = strokePx * (1.85f + 0.28f * beatAmt)
            val chaseOpacity = 0.72f + 0.18f * intensity + 0.10f * beatAmt
            val seg = 0.09f

            drawPath(
                path = orbit,
                color = col.copy(alpha = (0.26f + 0.18f * intensity) * 0.45f),
                style = Stroke(
                    width = strokePx * 0.75f,
                    cap = StrokeCap.Round,
                    pathEffect = PathEffect.dashPathEffect(floatArrayOf(6f, 18f), -phase * 90f),
                ),
            )

            val s0 = ((phase % 1f) + 1f) % 1f
            val e = s0 + seg
            fun drawChase(from: Float, to: Float) {
                val chunk = trimPathFraction(orbit, from, to)
                drawIntoCanvas { canvas ->
                    val glow = Paint().asFrameworkPaint().apply {
                        isAntiAlias = true
                        style = android.graphics.Paint.Style.STROKE
                        strokeWidth = chaseW
                        strokeCap = android.graphics.Paint.Cap.ROUND
                        color = android.graphics.Color.argb(
                            ((0.55 + 0.25 * hot) * 255).toInt().coerceIn(0, 255),
                            (col.red * 255).toInt(),
                            (col.green * 255).toInt(),
                            (col.blue * 255).toInt(),
                        )
                        maskFilter = BlurMaskFilter((6f + 3f * beatAmt).dp.toPx(), BlurMaskFilter.Blur.NORMAL)
                    }
                    canvas.nativeCanvas.drawPath(chunk.asAndroidPath(), glow)
                }
                drawPath(
                    path = chunk,
                    color = col.copy(alpha = chaseOpacity),
                    style = Stroke(width = chaseW, cap = StrokeCap.Round, join = StrokeJoin.Round),
                )
            }
            if (e <= 1f) {
                drawChase(s0, e)
            } else {
                drawChase(s0, 1f)
                drawChase(0f, e - 1f)
            }

            val sizes = floatArrayOf(6.5f, 4.5f, 5.5f, 3.8f, 5f)
            for (i in 0 until 5) {
                val off = clock.starOffsets[i]
                val rate = clock.starRates[i]
                var frac = ((run * rate + off) % 1.0)
                if (frac < 0) frac += 1.0
                val pt = PathSampler.point(orbit, frac.toFloat())
                val scol = chaseColor(run * 1.35 + off)
                val breath = (1.0 + 0.10 * hot + 0.06 * sin(run * 2 * PI * rate + off * 6)).toFloat()
                val r = sizes[i] * breath
                drawCircle(
                    color = scol.copy(alpha = 0.85f + 0.12f * beatAmt),
                    radius = r / 2f,
                    center = pt,
                    blendMode = BlendMode.Plus,
                )
                drawIntoCanvas { canvas ->
                    val glow = Paint().asFrameworkPaint().apply {
                        isAntiAlias = true
                        style = android.graphics.Paint.Style.FILL
                        color = android.graphics.Color.argb(
                            (0.55f * 255).toInt(),
                            (scol.red * 255).toInt(),
                            (scol.green * 255).toInt(),
                            (scol.blue * 255).toInt(),
                        )
                        maskFilter = BlurMaskFilter((5f + 2f * beatAmt).dp.toPx(), BlurMaskFilter.Blur.NORMAL)
                    }
                    canvas.nativeCanvas.drawCircle(pt.x, pt.y, r / 2f, glow)
                }
            }
        }
    }
}

private fun targetBpm(track: DreamTrack): Double {
    val fb = track.bpm.toDouble()
    return if (fb >= 50) min(168.0, fb) else 96.0
}

private fun lapSeconds(bpm: Double): Double {
    val raw = 22.0 * 60.0 / max(bpm, 50.0)
    return min(14.0, max(8.0, raw))
}

private fun chaseColor(phase: Double): Color {
    val n = ChaseColors.size
    val p = ((phase % 1.0) + 1.0) % 1.0 * n
    val i0 = floor(p).toInt() % n
    val i1 = (i0 + 1) % n
    val f = (p - floor(p)).toFloat()
    val a = ChaseColors[i0]
    val b = ChaseColors[i1]
    return Color(
        red = a.red + (b.red - a.red) * f,
        green = a.green + (b.green - a.green) * f,
        blue = a.blue + (b.blue - a.blue) * f,
        alpha = a.alpha + (b.alpha - a.alpha) * f,
    )
}

private fun beatOrbit(track: DreamTrack, slot: CoverSlot): Path {
    val rc = slot.rect
    val s = rc.width
    return when (track.motif) {
        Motif.Zig -> DoodlePaths.orbit(
            Offset(rc.center.x + 14f, rc.center.y),
            s * 0.62f, s * 0.5f, track.seed, track.jitter,
        )
        Motif.Cloud, Motif.Ink -> DoodlePaths.frameLoop(rc.inflate(18f), track.seed, track.jitter, pass = 0f)
    }
}

/** Trim a single-contour path to [from, to] fractions along arc length. */
private fun trimPathFraction(src: Path, from: Float, to: Float): Path {
    val out = Path()
    val srcA = android.graphics.Path(src.asAndroidPath())
    val pm = AndroidPathMeasure(srcA, false)
    val len = pm.length
    if (len <= 0f) return out
    val a = (from.coerceIn(0f, 1f)) * len
    val b = (to.coerceIn(0f, 1f)) * len
    if (b <= a) return out
    val chunk = android.graphics.Path()
    pm.getSegment(a, b, chunk, true)
    out.asAndroidPath().addPath(chunk)
    return out
}

/** Arc-length sampling for particles along a doodle orbit. */
object PathSampler {
    fun point(path: Path, fraction: Float): Offset {
        val srcA = android.graphics.Path(path.asAndroidPath())
        val pm = AndroidPathMeasure(srcA, false)
        var total = 0f
        do { total += pm.length } while (pm.nextContour())
        if (total <= 0f) return Offset.Zero
        var f = fraction % 1f
        if (f < 0f) f += 1f
        val target = total * f
        pm.setPath(srcA, false)
        var acc = 0f
        val pos = FloatArray(2)
        while (true) {
            val len = pm.length
            if (target <= acc + len) {
                val d = (target - acc).coerceIn(0f, len)
                pm.getPosTan(d, pos, null)
                return Offset(pos[0], pos[1])
            }
            acc += len
            if (!pm.nextContour()) {
                pm.getPosTan(len, pos, null)
                return Offset(pos[0], pos[1])
            }
        }
    }
}

/** cubic-bezier(0.42, 0, 0.58, 1) — unit curve via Newton on x, then y. */
private fun cubicBezierEase(t: Float): Float {
    val x1 = 0.42f; val y1 = 0f; val x2 = 0.58f; val y2 = 1f
    fun bez(a: Float, b: Float, c: Float, d: Float, u: Float): Float {
        val s = 1f - u
        return s * s * s * a + 3f * s * s * u * b + 3f * s * u * u * c + u * u * u * d
    }
    fun dbez(a: Float, b: Float, c: Float, d: Float, u: Float): Float {
        val s = 1f - u
        return 3f * s * s * (b - a) + 6f * s * u * (c - b) + 3f * u * u * (d - c)
    }
    var u = t.coerceIn(0f, 1f)
    repeat(10) {
        val x = bez(0f, x1, x2, 1f, u)
        val dx = dbez(0f, x1, x2, 1f, u)
        if (abs(dx) < 1e-6f) return@repeat
        u = (u - (x - t) / dx).coerceIn(0f, 1f)
    }
    return bez(0f, y1, y2, 1f, u)
}

/** Android PathMeasure walks one contour at a time — trim across ALL contours together. */
private fun trimAllContours(src: Path, fraction: Float): Path {
    val f = fraction.coerceIn(0f, 1f)
    val out = Path()
    if (f <= 0f) return out
    val srcA = android.graphics.Path(src.asAndroidPath())
    val pm = AndroidPathMeasure(srcA, false)
    val lengths = ArrayList<Float>()
    do { lengths.add(pm.length) } while (pm.nextContour())
    val total = lengths.sum()
    if (total <= 0f) return out
    val target = total * f
    pm.setPath(srcA, false)
    val outA = out.asAndroidPath()
    var acc = 0f
    var i = 0
    do {
        val len = lengths[i]
        val chunk = android.graphics.Path()
        when {
            target >= acc + len -> pm.getSegment(0f, len, chunk, true)
            target > acc -> pm.getSegment(0f, target - acc, chunk, true)
        }
        outA.addPath(chunk)
        acc += len
        i++
    } while (pm.nextContour())
    return out
}

/** Wire up wobble: perpendicular sine offset along each contour (amp = jitter×intensity). */
private fun wobblePath(src: Path, amp: Float, phase: Float): Path {
    val out = Path()
    val pm = AndroidPathMeasure(android.graphics.Path(src.asAndroidPath()), false)
    do {
        val len = pm.length
        if (len >= 1f) {
            val steps = max(2, (len / 3.5f).toInt())
            val pos = FloatArray(2)
            val tan = FloatArray(2)
            for (i in 0..steps) {
                val d = len * i / steps
                pm.getPosTan(d, pos, tan)
                val nx = -tan[1]; val ny = tan[0]
                val wob = sin(d * 0.085f + phase * 2.4f) * amp
                val x = pos[0] + nx * wob
                val y = pos[1] + ny * wob
                if (i == 0) out.moveTo(x, y) else out.lineTo(x, y)
            }
        }
    } while (pm.nextContour())
    return out
}

private fun motifPath(track: DreamTrack, slot: CoverSlot, w: Float, h: Float): Path {
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


    /** Single-pass frame loop used as chase orbit (iOS frameLoop). */
    fun frameLoop(rc: Rect, seed: ULong, jitter: Float, pass: Float = 0f): Path {
        var r = SeededRNG(seed)
        val p = Path()
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
