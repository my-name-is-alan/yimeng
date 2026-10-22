package art.yimeng.proto

import androidx.compose.foundation.Canvas
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.RoundRect
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.PathMeasure
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.scale
import androidx.compose.ui.graphics.drawscope.translate
import kotlin.math.PI
import kotlin.math.cos
import kotlin.math.sin

/**
 * Prototype of DoodleLayer "cloud / frame" motif (doodle.md §2.1).
 * Path trim 0→1 over 1.6s; beat pulse scales around cover center.
 * Full SeededRNG / golden parity is Phase 2 — this proves look & motion.
 */
@Composable
fun DoodleLayer(
    modifier: Modifier = Modifier,
    intensity: Float = 0.7f,
    bpm: Float = 96f,
) {
    var drawn by remember { mutableFloatStateOf(0f) }
    var pulse by remember { mutableFloatStateOf(0f) }
    var tSec by remember { mutableFloatStateOf(0f) }

    LaunchedEffect(Unit) {
        val start = withFrameNanos { it }
        var last = start
        while (true) {
            withFrameNanos { now ->
                val dt = ((now - last).coerceAtLeast(0) / 1_000_000_000f)
                last = now
                tSec += dt
                // Draw-on: easeInOut 1.6s
                if (drawn < 1f) {
                    val p = ((now - start) / 1_000_000_000f / 1.6f).coerceIn(0f, 1f)
                    drawn = easeInOut(p)
                }
                // Demo beat pulse (no audio tap yet): doodle.md §6.1 demo formula
                val phase = sin(tSec * (bpm / 60f) * 2f * PI.toFloat())
                pulse = (0.025f * intensity * phase.coerceAtLeast(0f))
            }
        }
    }

    Canvas(modifier) {
        val cover = Rect(
            left = size.width * 0.14f,
            top = size.height * 0.22f,
            right = size.width * 0.86f,
            bottom = size.height * 0.22f + size.width * 0.72f,
        )
        val path = cloudFramePath(cover)
        val measure = PathMeasure().apply { setPath(path, false) }
        val total = measure.length
        val dst = Path()
        measure.getSegment(0f, total * drawn, dst, true)

        val cx = cover.center.x
        val cy = cover.center.y
        val scale = 1f + 0.04f * intensity * (pulse * 20f).coerceIn(0f, 1f)
        // Crayon: two strokes with slight offset (doodle.md §1)
        translate(left = 0.8f, top = -0.6f) {
            scale(scale, scale, pivot = Offset(cx, cy)) {
                drawPath(
                    path = dst,
                    color = Color.White.copy(alpha = 0.35f),
                    style = Stroke(width = 2.4f, cap = StrokeCap.Round),
                )
            }
        }
        scale(scale, scale, pivot = Offset(cx, cy)) {
            drawPath(
                path = dst,
                color = Color.White.copy(alpha = 0.6f + 0.3f * intensity),
                style = Stroke(width = 2.2f, cap = StrokeCap.Round),
            )
        }
        // Glow approximation
        scale(scale, scale, pivot = Offset(cx, cy)) {
            drawPath(
                path = dst,
                color = Color.White.copy(alpha = 0.20f),
                style = Stroke(width = 6f, cap = StrokeCap.Round),
            )
        }
    }
}

private fun easeInOut(t: Float): Float =
    if (t < 0.5f) 2f * t * t else 1f - (-2f * t + 2f).let { it * it } / 2f

/** cloud motif: double frame + two cloud silhouettes + moons + sparkle. */
private fun cloudFramePath(cover: Rect): Path {
    val p = Path()
    val pad = 14f
    val outer = Rect(cover.left - pad, cover.top - pad, cover.right + pad, cover.bottom + pad)
    // Double rounded frame
    p.addRoundRect(RoundRect(outer, CornerRadius(14f, 14f)))
    val inner = Rect(outer.left + 6f, outer.top + 6f, outer.right - 6f, outer.bottom - 6f)
    p.addRoundRect(RoundRect(inner, CornerRadius(12f, 12f)))
    // Cloud left-top
    cloudBlob(p, outer.left + 20f, outer.top - 8f, 110f)
    // Cloud right-bottom
    cloudBlob(p, outer.right - 100f, outer.bottom + 4f, 90f)
    // Moons
    moonArc(p, outer.right - 28f, outer.top + 36f, 16f)
    moonArc(p, outer.left + 36f, outer.bottom - 48f, 10f)
    // Sparkle
    sparkle(p, outer.right - 48f, outer.bottom - 70f, 7f)
    return p
}

private fun cloudBlob(p: Path, x: Float, y: Float, w: Float) {
    val h = w * 0.42f
    p.moveTo(x, y + h * 0.55f)
    p.cubicTo(x, y, x + w * 0.25f, y - h * 0.15f, x + w * 0.38f, y + h * 0.25f)
    p.cubicTo(x + w * 0.42f, y - h * 0.35f, x + w * 0.70f, y - h * 0.2f, x + w * 0.72f, y + h * 0.3f)
    p.cubicTo(x + w * 0.95f, y + h * 0.15f, x + w, y + h * 0.7f, x + w * 0.78f, y + h)
    p.cubicTo(x + w * 0.55f, y + h * 1.15f, x + w * 0.2f, y + h * 1.05f, x, y + h * 0.55f)
    p.close()
}

private fun moonArc(p: Path, cx: Float, cy: Float, r: Float) {
    p.moveTo(cx + r, cy)
    p.cubicTo(cx + r, cy + r * 1.1f, cx - r * 0.2f, cy + r, cx - r * 0.35f, cy)
    p.cubicTo(cx - r * 0.2f, cy - r, cx + r, cy - r * 1.1f, cx + r, cy)
}

private fun sparkle(p: Path, cx: Float, cy: Float, r: Float) {
    for (i in 0 until 4) {
        val a = i * (PI.toFloat() / 2f)
        p.moveTo(cx, cy)
        p.lineTo(cx + cos(a) * r, cy + sin(a) * r)
    }
}
