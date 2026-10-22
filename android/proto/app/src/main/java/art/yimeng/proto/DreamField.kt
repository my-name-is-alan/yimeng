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

/**
 * Static 4-circle field + beat breathing radial (Screen blend, fixed r=420, α=blob0×0.22×pulse).
 */
@Composable
fun DreamField(
    track: DreamTrack,
    modifier: Modifier = Modifier,
    realPlayback: Boolean = true,
    lastBeatAgoSec: Float = 0.2f,
    reduceMotion: Boolean = false,
) {
    var pulse by remember { mutableFloatStateOf(0f) }
    LaunchedEffect(realPlayback, reduceMotion, lastBeatAgoSec) {
        if (!realPlayback) { pulse = 0f; return@LaunchedEffect }
        val fps = if (reduceMotion) 4 else 30
        val frameNs = 1_000_000_000L / fps
        var last = 0L
        while (true) {
            withFrameNanos { now ->
                if (now - last < frameNs) return@withFrameNanos
                last = now
                pulse = if (reduceMotion) 0.12f else exp(-7.0 * lastBeatAgoSec.toDouble()).toFloat()
            }
        }
    }
    Canvas(modifier.fillMaxSize()) {
        drawRect(track.base)
        val w = size.width; val h = size.height
        softBlob(track.blobs[0], Offset(w * 0.10f, h * 0.08f), w * 1.05f / 2f)
        softBlob(track.blobs[1], Offset(w * 0.90f, h * 0.32f), w * 0.95f / 2f)
        softBlob(track.blobs[2], Offset(w * 0.05f, h * 0.68f), w * 1.00f / 2f)
        softBlob(track.blobs[3], Offset(w * 0.95f, h * 0.92f), w * 0.90f / 2f)
        if (realPlayback && pulse > 0.001f) {
            val a = track.blobs[0].alpha * 0.22f * pulse
            drawCircle(
                brush = Brush.radialGradient(
                    colors = listOf(track.blobs[0].copy(alpha = a), Color.Transparent),
                    center = Offset(w * 0.5f, h * 0.42f),
                    radius = 420f,
                    tileMode = TileMode.Clamp,
                ),
                radius = 420f,
                center = Offset(w * 0.5f, h * 0.42f),
                blendMode = BlendMode.Screen,
            )
        }
        drawRect(
            brush = Brush.verticalGradient(
                0f to Color.Black.copy(0.10f),
                0.5f to Color.Transparent,
                1f to Color.Black.copy(0.28f),
            ),
        )
    }
}

private fun DrawScope.softBlob(c: Color, center: Offset, radius: Float) {
    drawCircle(
        brush = Brush.radialGradient(
            0f to c.copy(alpha = 0.55f),
            0.55f to c.copy(alpha = 0.25f),
            1f to Color.Transparent,
            center = center,
            radius = radius,
        ),
        radius = radius,
        center = center,
    )
}
