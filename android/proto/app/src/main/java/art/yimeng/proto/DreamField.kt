package art.yimeng.proto

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.BlendMode
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.TileMode
import androidx.compose.ui.graphics.drawscope.DrawScope
import kotlin.math.exp
import kotlin.math.sin

/**
 * Now-playing DreamField (§7.4): multi-color soft mesh (4+ blobs, slow breathing),
 * beat pulse radial (blob[0] × 0.22 × pulse, Screen), top black 10% + bottom black 28% masks.
 * Beat breathing ONLY here + doodles — chrome never moves (§6.1).
 */
@Composable
fun DreamField(
    track: DreamTrack,
    modifier: Modifier = Modifier,
    realPlayback: Boolean = true,
    lastBeatAgoSec: Float = 0.2f,
    reduceMotion: Boolean = false,
    intensity: Float = 0.7f,
) {
    var pulse by remember { mutableFloatStateOf(0f) }
    var tSec by remember { mutableFloatStateOf(0f) }
    LaunchedEffect(realPlayback, reduceMotion, lastBeatAgoSec, track.id) {
        val fps = if (reduceMotion) 4 else 30
        val frameNs = 1_000_000_000L / fps
        var last = 0L
        val start = withFrameNanos { it }
        while (true) {
            withFrameNanos { now ->
                if (now - last < frameNs) return@withFrameNanos
                last = now
                tSec = (now - start) / 1_000_000_000f
                pulse = when {
                    reduceMotion -> 0.08f
                    realPlayback -> exp(-7.0 * lastBeatAgoSec.toDouble()).toFloat()
                    else -> 0f
                }
            }
        }
    }
    Canvas(modifier.fillMaxSize()) {
        val w = size.width
        val h = size.height
        drawRect(track.base)

        // Slow mesh breathing: displace blob centers 0.08·sin(t·0.35+k) like iOS MeshGradient
        val breathe = if (reduceMotion) 0f else 1f
        fun cx(i: Int, base: Float): Float =
            base + breathe * 0.08f * sin(tSec * 0.35f + i * 1.7f) * w
        fun cy(i: Int, base: Float): Float =
            base + breathe * 0.08f * sin(tSec * 0.28f + i * 2.1f) * h

        val b = track.blobs
        // target-cover-v3.html radial stops: pink@20%30%, gold@80%20%, purple@50%80%, green@10%90%
        // blobs[] order expected: [pink, gold, purple, green] for mock match (home orange still works)
        softBlob(b[0], Offset(cx(0, w * 0.20f), cy(0, h * 0.26f)), w * 0.95f, peak = 0.85f)
        softBlob(b[1], Offset(cx(1, w * 0.82f), cy(1, h * 0.16f)), w * 0.90f, peak = 0.78f)
        softBlob(b[2], Offset(cx(2, w * 0.50f), cy(2, h * 0.80f)), w * 1.00f, peak = 0.72f)
        softBlob(b[3], Offset(cx(3, w * 0.10f), cy(3, h * 0.90f)), w * 0.75f, peak = 0.45f)
        // Soft mid wash — pink lift behind cover
        softBlob(
            b[0].copy(alpha = 1f),
            Offset(cx(4, w * 0.48f), cy(4, h * 0.40f)),
            w * 0.55f,
            peak = 0.38f,
        )
        softBlob(
            b[1].copy(alpha = 1f),
            Offset(cx(5, w * 0.62f), cy(5, h * 0.55f)),
            w * 0.48f,
            peak = 0.30f,
        )

        // Beat pulse radial — Screen blend (§6.1)
        if (realPlayback && pulse > 0.001f) {
            val a = 0.22f * pulse * intensity
            val c = Offset(w * 0.5f, h * 0.42f)
            drawCircle(
                brush = Brush.radialGradient(
                    colors = listOf(b[0].copy(alpha = a), Color.Transparent),
                    center = c,
                    radius = 420f,
                    tileMode = TileMode.Clamp,
                ),
                radius = 420f,
                center = c,
                blendMode = BlendMode.Screen,
            )
        }

        // Top black 10% + bottom black 28% (§7.4)
        drawRect(
            brush = Brush.verticalGradient(
                0f to Color.Black.copy(0.10f),
                0.22f to Color.Transparent,
                0.55f to Color.Transparent,
                1f to Color.Black.copy(0.28f),
            ),
        )
    }
}

private fun DrawScope.softBlob(c: Color, center: Offset, radius: Float, peak: Float = 0.55f) {
    drawCircle(
        brush = Brush.radialGradient(
            0f to c.copy(alpha = peak),
            0.45f to c.copy(alpha = peak * 0.45f),
            1f to Color.Transparent,
            center = center,
            radius = radius,
        ),
        radius = radius,
        center = center,
    )
}
