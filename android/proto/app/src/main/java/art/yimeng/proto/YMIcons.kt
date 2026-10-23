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
 * Port of iOS YMIcons.swift (.r2-staging / feat/ios-navidrome handoff).
 * Unit-square 0…1 path tables + YMIcon crayon second-pass + YMBrandMark (梦境环).
 * Paths match iOS exactly; Eq/AddList are clean SF-silhouette extensions (no fake wobble).
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

    /** iOS YMIcons.loop — open arc + arrowhead (+ optional "1"). */
    private fun loop(one: Boolean): Path = Path().apply {
        val start = Math.toRadians(-40.0)
        val end = Math.toRadians(220.0)
        val n = 24
        for (i in 0..n) {
            val t = i / n.toFloat()
            val a = start + (end - start) * t
            val x = 0.50f + cos(a).toFloat() * 0.32f
            val y = 0.50f + sin(a).toFloat() * 0.32f
            if (i == 0) moveTo(x, y) else lineTo(x, y)
        }
        moveTo(0.72f, 0.18f); lineTo(0.86f, 0.32f); lineTo(0.68f, 0.36f)
        if (one) {
            moveTo(0.46f, 0.38f); lineTo(0.52f, 0.38f); lineTo(0.52f, 0.68f)
        }
    }

    private fun arrow(): Path = Path().apply {
        moveTo(0.18f, 0.50f); lineTo(0.78f, 0.50f)
        moveTo(0.62f, 0.32f); lineTo(0.82f, 0.50f); lineTo(0.62f, 0.68f)
    }

    /**
     * Lyrics — speech bubble (SF quote.bubble / target-cover-v3).
     * Clean rounded path + text lines; crayon feel from YMIcon ghost pass, not fake wobble.
     */
    private fun quote(): Path = Path().apply {
        // rounded bubble body (unit square of Lucide message-square)
        addRoundRect(RoundRect(0.10f, 0.12f, 0.90f, 0.62f, CornerRadius(0.10f, 0.10f)))
        // tail
        moveTo(0.22f, 0.62f)
        lineTo(0.14f, 0.86f)
        lineTo(0.38f, 0.62f)
        // text lines
        moveTo(0.28f, 0.30f); lineTo(0.72f, 0.30f)
        moveTo(0.28f, 0.44f); lineTo(0.60f, 0.44f)
    }

    /** Queue — iOS YMIcons.list (bullet + line ×3). */
    private fun list(): Path = Path().apply {
        for (y in floatArrayOf(0.28f, 0.50f, 0.72f)) {
            addOval(Rect(0.16f, y - 0.05f, 0.26f, y + 0.05f))
            moveTo(0.36f, y); lineTo(0.84f, y)
        }
    }

    /** More — iOS YMIcons.dots. */
    private fun dots(): Path = Path().apply {
        for (x in floatArrayOf(0.22f, 0.45f, 0.68f)) {
            addOval(Rect(x, 0.42f, x + 0.14f, 0.56f))
        }
    }

    private fun heart(fill: Boolean): Path = Path().apply {
        // iOS YMIcons.heart
        moveTo(0.50f, 0.82f)
        cubicTo(0.20f, 0.68f, 0.08f, 0.52f, 0.14f, 0.38f)
        cubicTo(0.18f, 0.18f, 0.38f, 0.16f, 0.50f, 0.28f)
        cubicTo(0.62f, 0.16f, 0.82f, 0.18f, 0.86f, 0.38f)
        cubicTo(0.92f, 0.52f, 0.80f, 0.68f, 0.50f, 0.82f)
        close()
        @Suppress("UNUSED_EXPRESSION") fill
    }

    private fun star(): Path = DoodlePaths.sparkle(Offset(0.5f, 0.5f), 0.36f)

    private fun download(): Path = Path().apply {
        moveTo(0.50f, 0.14f); lineTo(0.50f, 0.62f)
        moveTo(0.32f, 0.46f); lineTo(0.50f, 0.66f); lineTo(0.68f, 0.46f)
        moveTo(0.22f, 0.78f); lineTo(0.78f, 0.78f)
    }

    private fun share(): Path = Path().apply {
        moveTo(0.50f, 0.70f); lineTo(0.50f, 0.18f)
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
            // iOS YMIcons.chevron
            moveTo(0.5f + 0.18f * f, 0.22f)
            lineTo(0.5f - 0.18f * f, 0.50f)
            lineTo(0.5f + 0.18f * f, 0.78f)
        }
    }

    private fun xmark(): Path = Path().apply {
        moveTo(0.26f, 0.26f); lineTo(0.74f, 0.74f)
        moveTo(0.74f, 0.26f); lineTo(0.26f, 0.74f)
    }

    private fun wave(): Path = Path().apply {
        // iOS YMIcons.wave
        val xs = floatArrayOf(0.18f, 0.32f, 0.46f, 0.60f, 0.74f)
        val hs = floatArrayOf(0.22f, 0.42f, 0.62f, 0.38f, 0.26f)
        for (i in xs.indices) {
            moveTo(xs[i], 0.5f - hs[i] / 2f)
            lineTo(xs[i], 0.5f + hs[i] / 2f)
        }
    }

    /**
     * EQ — Android extension (iOS NowPlaying uses SF slider.horizontal.3).
     * Clean vertical faders matching target-cover-v3; no wobble helper.
     */
    private fun eq(): Path = Path().apply {
        val xs = floatArrayOf(0.26f, 0.50f, 0.74f)
        val knobs = floatArrayOf(0.44f, 0.32f, 0.52f)
        for (i in xs.indices) {
            val x = xs[i]
            moveTo(x, 0.14f); lineTo(x, 0.86f)
            addOval(Rect(x - 0.07f, knobs[i] - 0.07f, x + 0.07f, knobs[i] + 0.07f))
        }
    }

    /**
     * Add-to-playlist — Android extension (iOS SF text.badge.plus).
     * Clean list lines + plus; crayon feel from YMIcon ghost pass only.
     */
    private fun addList(): Path = Path().apply {
        moveTo(0.12f, 0.28f); lineTo(0.54f, 0.28f)
        moveTo(0.12f, 0.50f); lineTo(0.50f, 0.50f)
        moveTo(0.12f, 0.72f); lineTo(0.46f, 0.72f)
        moveTo(0.66f, 0.38f); lineTo(0.90f, 0.38f)
        moveTo(0.78f, 0.26f); lineTo(0.78f, 0.50f)
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
