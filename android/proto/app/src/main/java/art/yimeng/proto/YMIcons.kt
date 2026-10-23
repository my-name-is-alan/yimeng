package art.yimeng.proto

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.geometry.RoundRect
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Fill
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.translate
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import kotlin.math.PI
import kotlin.math.cos
import kotlin.math.max
import kotlin.math.sin

/**
 * Port of iOS YMIcons.swift @ 06f2059 (feat/ios-navidrome).
 * Unit-square 0…1 path tables + YMIcon crayon second-pass + YMBrandMark (梦境环).
 * Do not invent glyphs not in the source file.
 */

enum class YMGlyph {
    // tabs / sidebar
    Home, Library, Collection, Search, Me,
    // player
    Play, Pause, Previous, Next, Shuffle, Repeat, RepeatOne, Order, Lyrics, Queue, More, Eq, AddList,
    // actions
    Heart, HeartFill, Star, Download, Share, Settings, Back, Close, Recognize, Chevron,
}

object YMIconPaths {
    fun path(g: YMGlyph): Path = when (g) {
        YMGlyph.Home -> house()
        YMGlyph.Library -> notes()
        YMGlyph.Collection -> stacks()
        YMGlyph.Search -> magnifier()
        YMGlyph.Me -> person()
        YMGlyph.Play -> play()
        YMGlyph.Pause -> pause()
        YMGlyph.Previous -> skip(back = true)
        YMGlyph.Next -> skip(back = false)
        YMGlyph.Shuffle -> shuffle()
        YMGlyph.Repeat -> loop(one = false)
        YMGlyph.RepeatOne -> loop(one = true)
        YMGlyph.Order -> arrow()
        YMGlyph.Lyrics -> quote()
        YMGlyph.Queue -> list()
        YMGlyph.More -> dots()
        YMGlyph.Eq -> eq()
        YMGlyph.AddList -> addList()
        YMGlyph.Heart -> heart(fill = false)
        YMGlyph.HeartFill -> heart(fill = true)
        YMGlyph.Star -> star()
        YMGlyph.Download -> download()
        YMGlyph.Share -> share()
        YMGlyph.Settings -> gear()
        YMGlyph.Back -> chevron(left = true)
        YMGlyph.Close -> xmark()
        YMGlyph.Recognize -> wave()
        YMGlyph.Chevron -> chevron(left = false)
    }

    private fun house(): Path = Path().apply {
        moveTo(0.50f, 0.14f)
        lineTo(0.88f, 0.46f); lineTo(0.78f, 0.46f); lineTo(0.78f, 0.86f)
        lineTo(0.55f, 0.86f); lineTo(0.55f, 0.62f); lineTo(0.45f, 0.62f)
        lineTo(0.45f, 0.86f); lineTo(0.22f, 0.86f); lineTo(0.22f, 0.46f)
        lineTo(0.12f, 0.46f); close()
    }

    private fun notes(): Path = Path().apply {
        addOval(Rect(0.18f, 0.62f, 0.40f, 0.80f))
        addOval(Rect(0.55f, 0.55f, 0.77f, 0.73f))
        moveTo(0.38f, 0.70f); lineTo(0.38f, 0.22f)
        lineTo(0.75f, 0.16f); lineTo(0.75f, 0.63f)
    }

    private fun stacks(): Path = Path().apply {
        for (i in 0 until 3) {
            val o = i * 0.08f
            addRoundRect(
                RoundRect(
                    left = 0.18f + o, top = 0.28f - o * 0.5f,
                    right = 0.18f + o + 0.58f, bottom = 0.28f - o * 0.5f + 0.42f,
                    cornerRadius = CornerRadius(0.08f, 0.08f),
                ),
            )
        }
    }

    private fun magnifier(): Path = Path().apply {
        addOval(Rect(0.18f, 0.14f, 0.70f, 0.66f))
        moveTo(0.62f, 0.58f); lineTo(0.86f, 0.84f)
    }

    private fun person(): Path = Path().apply {
        addOval(Rect(0.32f, 0.12f, 0.68f, 0.48f))
        moveTo(0.16f, 0.88f)
        quadraticTo(0.50f, 0.48f, 0.84f, 0.88f)
    }

    private fun play(): Path = Path().apply {
        moveTo(0.32f, 0.18f); lineTo(0.82f, 0.50f); lineTo(0.32f, 0.82f); close()
    }

    private fun pause(): Path = Path().apply {
        addRoundRect(RoundRect(0.28f, 0.18f, 0.44f, 0.82f, CornerRadius(0.04f)))
        addRoundRect(RoundRect(0.56f, 0.18f, 0.72f, 0.82f, CornerRadius(0.04f)))
    }

    // Previous = |◀ (back=true); Next = ▶| (back=false). Old mirror pointed the wrong way.
    private fun skip(back: Boolean): Path {
        return Path().apply {
            if (back) {
                // |◀
                moveTo(0.18f, 0.20f); lineTo(0.18f, 0.80f)
                moveTo(0.82f, 0.18f); lineTo(0.38f, 0.50f); lineTo(0.82f, 0.82f); close()
            } else {
                // ▶|
                moveTo(0.18f, 0.18f); lineTo(0.62f, 0.50f); lineTo(0.18f, 0.82f); close()
                moveTo(0.82f, 0.20f); lineTo(0.82f, 0.80f)
            }
        }
    }

    private fun shuffle(): Path = Path().apply {
        moveTo(0.12f, 0.30f); lineTo(0.32f, 0.30f)
        quadraticTo(0.50f, 0.30f, 0.68f, 0.70f); lineTo(0.88f, 0.70f)
        moveTo(0.78f, 0.58f); lineTo(0.90f, 0.70f); lineTo(0.78f, 0.82f)
        moveTo(0.12f, 0.70f); lineTo(0.32f, 0.70f)
        quadraticTo(0.50f, 0.70f, 0.68f, 0.30f); lineTo(0.88f, 0.30f)
        moveTo(0.78f, 0.18f); lineTo(0.90f, 0.30f); lineTo(0.78f, 0.42f)
    }

    /** Dual-arrow repeat — soft hand arcs, not Lucide chevrons. */
    private fun loop(one: Boolean): Path = Path().apply {
        // top sweep →
        moveTo(0.68f, 0.20f)
        quadraticTo(0.90f, 0.22f, 0.86f, 0.38f)
        quadraticTo(0.55f, 0.36f, 0.22f, 0.38f)
        quadraticTo(0.10f, 0.40f, 0.12f, 0.52f)
        // arrow head (slightly askew)
        moveTo(0.74f, 0.14f)
        lineTo(0.90f, 0.34f)
        lineTo(0.68f, 0.36f)
        // bottom sweep ←
        moveTo(0.32f, 0.80f)
        quadraticTo(0.10f, 0.78f, 0.14f, 0.62f)
        quadraticTo(0.45f, 0.64f, 0.78f, 0.62f)
        quadraticTo(0.90f, 0.60f, 0.88f, 0.48f)
        moveTo(0.26f, 0.86f)
        lineTo(0.10f, 0.66f)
        lineTo(0.32f, 0.64f)
        if (one) {
            moveTo(0.46f, 0.42f); lineTo(0.52f, 0.42f); lineTo(0.52f, 0.58f)
        }
    }

    private fun arrow(): Path = Path().apply {
        doodleLine(0.18f, 0.50f, 0.78f, 0.50f, 0.015f)
        moveTo(0.62f, 0.32f); lineTo(0.82f, 0.50f); lineTo(0.62f, 0.68f)
    }

    /**
     * Lyrics — speech bubble with text lines (target-cover-v3 silhouette)
     * drawn as imperfect crayon strokes, not Lucide rounded-rect.
     */
    private fun quote(): Path = Path().apply {
        // bubble outline — wobbly rounded rect + tail
        moveTo(0.20f, 0.24f)
        quadraticTo(0.50f, 0.18f, 0.80f, 0.24f)
        quadraticTo(0.92f, 0.28f, 0.90f, 0.42f)
        quadraticTo(0.92f, 0.56f, 0.78f, 0.62f)
        lineTo(0.44f, 0.62f)
        // tail (hand-drawn triangle)
        quadraticTo(0.34f, 0.72f, 0.26f, 0.84f)
        quadraticTo(0.30f, 0.70f, 0.32f, 0.62f)
        lineTo(0.20f, 0.62f)
        quadraticTo(0.08f, 0.58f, 0.10f, 0.42f)
        quadraticTo(0.08f, 0.28f, 0.20f, 0.24f)
        close()
        // text lines — slight bow
        doodleLine(0.26f, 0.36f, 0.70f, 0.35f, 0.012f)
        doodleLine(0.26f, 0.48f, 0.62f, 0.49f, -0.010f)
    }

    /** Queue — bulleted list with bowed crayon lines. */
    private fun list(): Path = Path().apply {
        val ys = floatArrayOf(0.28f, 0.50f, 0.72f)
        val ends = floatArrayOf(0.86f, 0.80f, 0.84f)
        val bumps = floatArrayOf(0.014f, -0.012f, 0.010f)
        for (i in ys.indices) {
            val y = ys[i]
            // imperfect dots (slightly oval / offset)
            val ox = if (i == 1) 0.01f else 0f
            addOval(Rect(0.15f + ox, y - 0.045f, 0.27f + ox, y + 0.055f))
            doodleLine(0.36f, y, ends[i], y + (if (i == 2) 0.01f else -0.005f * i), bumps[i])
        }
    }

    /** More — three uneven crayon dots. */
    private fun dots(): Path = Path().apply {
        addOval(Rect(0.18f, 0.40f, 0.34f, 0.58f))
        addOval(Rect(0.42f, 0.38f, 0.58f, 0.56f))
        addOval(Rect(0.66f, 0.42f, 0.82f, 0.58f))
    }

    private fun heart(fill: Boolean): Path = Path().apply {
        // Asymmetric lobes — hand-drawn feel
        moveTo(0.50f, 0.84f)
        cubicTo(0.18f, 0.70f, 0.06f, 0.50f, 0.16f, 0.36f)
        cubicTo(0.22f, 0.18f, 0.40f, 0.14f, 0.50f, 0.30f)
        cubicTo(0.58f, 0.12f, 0.84f, 0.16f, 0.86f, 0.38f)
        cubicTo(0.94f, 0.54f, 0.78f, 0.70f, 0.50f, 0.84f)
        close()
        @Suppress("UNUSED_EXPRESSION") fill
    }

    private fun star(): Path = DoodlePaths.sparkle(Offset(0.5f, 0.5f), 0.36f)

    private fun download(): Path = Path().apply {
        doodleLine(0.50f, 0.14f, 0.50f, 0.62f, 0.012f)
        moveTo(0.32f, 0.46f); lineTo(0.50f, 0.66f); lineTo(0.68f, 0.46f)
        doodleLine(0.22f, 0.78f, 0.78f, 0.78f, -0.01f)
    }

    private fun share(): Path = Path().apply {
        doodleLine(0.50f, 0.70f, 0.50f, 0.18f, 0.01f)
        moveTo(0.32f, 0.34f); lineTo(0.50f, 0.14f); lineTo(0.68f, 0.34f)
        moveTo(0.22f, 0.48f); lineTo(0.22f, 0.84f); lineTo(0.78f, 0.84f); lineTo(0.78f, 0.48f)
    }

    private fun gear(): Path = Path().apply {
        addOval(Rect(0.36f, 0.36f, 0.64f, 0.64f))
        for (i in 0 until 8) {
            val a = i / 8.0 * 2 * PI
            val i0 = Offset(0.5f + cos(a).toFloat() * 0.22f, 0.5f + sin(a).toFloat() * 0.22f)
            val o0 = Offset(0.5f + cos(a).toFloat() * 0.40f, 0.5f + sin(a).toFloat() * 0.40f)
            moveTo(i0.x, i0.y); lineTo(o0.x, o0.y)
        }
    }

    private fun chevron(left: Boolean): Path {
        val f = if (left) 1f else -1f
        return Path().apply {
            // Soft bow instead of sharp V
            moveTo(0.5f + 0.20f * f, 0.22f)
            quadraticTo(0.5f - 0.06f * f, 0.50f, 0.5f + 0.20f * f, 0.78f)
        }
    }

    private fun xmark(): Path = Path().apply {
        doodleLine(0.26f, 0.26f, 0.74f, 0.74f, 0.012f)
        doodleLine(0.74f, 0.26f, 0.26f, 0.74f, -0.010f)
    }

    private fun wave(): Path = Path().apply {
        val xs = floatArrayOf(0.18f, 0.32f, 0.46f, 0.60f, 0.74f)
        val hs = floatArrayOf(0.22f, 0.42f, 0.62f, 0.38f, 0.26f)
        val bumps = floatArrayOf(0.012f, -0.010f, 0.014f, -0.008f, 0.010f)
        for (i in xs.indices) {
            doodleLine(xs[i], 0.5f - hs[i] / 2, xs[i] + 0.01f * (i % 2), 0.5f + hs[i] / 2, bumps[i])
        }
    }

    /** Three vertical EQ faders — crooked crayon rails + imperfect knobs. */
    private fun eq(): Path = Path().apply {
        val xs = floatArrayOf(0.26f, 0.50f, 0.74f)
        val knobs = floatArrayOf(0.44f, 0.32f, 0.52f)
        val tops = floatArrayOf(0.14f, 0.18f, 0.16f)
        val bots = floatArrayOf(0.86f, 0.82f, 0.84f)
        val bumps = floatArrayOf(0.016f, -0.014f, 0.012f)
        for (i in xs.indices) {
            val x = xs[i]
            doodleLine(x, tops[i], x + 0.012f * (if (i == 1) -1 else 1), bots[i], bumps[i])
            // knobs slightly oval / uneven
            val kr = if (i == 1) 0.075f else 0.065f
            addOval(Rect(x - kr, knobs[i] - kr * 0.9f, x + kr * 1.05f, knobs[i] + kr * 1.1f))
        }
    }

    /** Add-to-playlist — bowed list lines + askew plus. */
    private fun addList(): Path = Path().apply {
        doodleLine(0.12f, 0.28f, 0.54f, 0.30f, 0.014f)
        doodleLine(0.12f, 0.50f, 0.50f, 0.48f, -0.012f)
        doodleLine(0.12f, 0.70f, 0.46f, 0.72f, 0.010f)
        // plus — slightly off-axis
        doodleLine(0.66f, 0.38f, 0.90f, 0.36f, 0.008f)
        doodleLine(0.78f, 0.24f, 0.80f, 0.50f, -0.010f)
    }

    /** Wobbly stroke helper — mid control bump perpendicular to the segment. */
    private fun Path.doodleLine(x0: Float, y0: Float, x1: Float, y1: Float, bump: Float) {
        val mx = (x0 + x1) / 2f
        val my = (y0 + y1) / 2f
        val dx = x1 - x0
        val dy = y1 - y0
        val len = max(0.001f, kotlin.math.sqrt((dx * dx + dy * dy).toDouble()).toFloat())
        // bump is already unit-space perpendicular offset (~0.01–0.02); do not amplify
        val nx = -dy / len * bump
        val ny = dx / len * bump
        moveTo(x0, y0)
        quadraticTo(mx + nx, my + ny, x1, y1)
    }

    /** Scale unit-square path into pixel size. */
    fun scaled(g: YMGlyph, sizePx: Float): Path {
        val out = Path()
        out.addPath(path(g))
        val m = androidx.compose.ui.graphics.Matrix().apply { scale(sizePx, sizePx) }
        out.transform(m)
        return out
    }
}

@Composable
fun YMIcon(
    glyph: YMGlyph,
    modifier: Modifier = Modifier,
    size: Dp = 22.dp,
    weight: Float = 1.65f,
    active: Boolean = false,
    filled: Boolean = false,
    color: Color? = null,
) {
    val ink = color ?: if (active) YmTheme.accent else YmTheme.fg
    val doFill = filled || glyph == YMGlyph.Play || glyph == YMGlyph.Pause || glyph == YMGlyph.HeartFill
    Canvas(modifier.size(size)) {
        val s = size.toPx()
        val path = YMIconPaths.scaled(glyph, s)
        // 1.5–2pt hairline at 22dp — designer skel6b spec (not Material bold)
        val lw = max(1.35f, weight * s / 22f).coerceAtMost(2.15f * density)
        // Single crayon ghost — iOS YMIcon (α0.35, offset 0.6/0.7)
        translate(0.6f * density, 0.7f * density) {
            drawPath(
                path, ink.copy(alpha = ink.alpha * 0.32f),
                style = Stroke(width = lw * 0.85f, cap = StrokeCap.Round, join = StrokeJoin.Round),
            )
        }
        if (doFill) drawPath(path, ink, style = Fill)
        drawPath(
            path, ink,
            style = Stroke(width = lw, cap = StrokeCap.Round, join = StrokeJoin.Round),
        )
    }
}

/** 梦境环 — WobblyCircle + sparkle (iOS YMBrandMark). */
@Composable
fun YMBrandMark(
    modifier: Modifier = Modifier,
    size: Dp = 36.dp,
    ringWidth: Float = 1.8f,
    sparkle: Boolean = true,
    accentSparkle: Boolean = true,
) {
    Canvas(modifier.size(size)) {
        val s = size.toPx()
        val ring = wobblyCircle(Offset(s / 2f, s / 2f), s * 0.46f, seed = 3uL)
        val lw = ringWidth * (s / 36f)
        // Crayon: main 90% + second 35% @ (0.8,−0.6) rot 0.8°
        drawPath(
            ring, Color.White.copy(alpha = 0.9f),
            style = Stroke(width = lw, cap = StrokeCap.Round, join = StrokeJoin.Round),
        )
        translate(0.8f * density, -0.6f * density) {
            drawPath(
                ring, Color.White.copy(alpha = 0.35f),
                style = Stroke(width = lw * 0.7f, cap = StrokeCap.Round, join = StrokeJoin.Round),
            )
        }
        if (sparkle) {
            val sp = DoodlePaths.sparkle(Offset(s * 0.78f, s * 0.22f), s * 0.16f)
            val c = if (accentSparkle) YmTheme.accent else Color.White
            drawPath(
                sp, c,
                style = Stroke(width = max(1.2f, lw * 0.85f), cap = StrokeCap.Round, join = StrokeJoin.Round),
            )
        }
    }
}

/** iOS WobblyCircle (IPad.swift @ 06f2059) — two jittered passes. */
private fun wobblyCircle(c: Offset, R: Float, seed: ULong): Path {
    var rng = SeededRNG(seed)
    val p = Path()
    for (pass in 0 until 2) {
        val n = 14
        for (i in 0..n) {
            val a = i.toDouble() / n * 2 * PI + if (pass == 0) 0.0 else 0.3
            val rr = R * (1 + rng.d(-0.05, 0.05)).toFloat()
            val pt = Offset(c.x + cos(a).toFloat() * rr, c.y + sin(a).toFloat() * rr)
            if (i == 0) p.moveTo(pt.x, pt.y) else p.lineTo(pt.x, pt.y)
        }
    }
    return p
}
