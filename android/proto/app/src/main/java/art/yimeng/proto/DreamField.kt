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
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import kotlin.math.PI
import kotlin.math.sin

/** Soft blob field stand-in for DreamField (iOS 17 blur-circles path). */
@Composable
fun DreamField(modifier: Modifier = Modifier) {
    var t by remember { mutableFloatStateOf(0f) }
    LaunchedEffect(Unit) {
        val start = withFrameNanos { it }
        while (true) {
            withFrameNanos { now ->
                t = (now - start) / 1_000_000_000f
            }
        }
    }
    Canvas(modifier.fillMaxSize()) {
        drawRect(YmTheme.idleField)
        YmTheme.blobs.forEachIndexed { i, c ->
            val ox = size.width * (0.25f + 0.2f * i)
            val oy = size.height * (0.30f + 0.08f * sin(t * 0.35f + i))
            drawCircle(
                brush = Brush.radialGradient(
                    colors = listOf(c.copy(alpha = 0.55f), Color.Transparent),
                    center = Offset(ox, oy),
                    radius = size.minDimension * 0.45f,
                ),
                radius = size.minDimension * 0.45f,
                center = Offset(ox, oy),
            )
        }
        // Top/bottom readability scrims
        drawRect(
            brush = Brush.verticalGradient(
                0f to Color.Black.copy(alpha = 0.10f),
                0.5f to Color.Transparent,
                1f to Color.Black.copy(alpha = 0.28f),
            ),
        )
        // Beat glow (demo)
        val pulse = (0.5f + 0.5f * sin(t * (96f / 60f) * 2f * PI.toFloat())).coerceIn(0f, 1f)
        drawCircle(
            color = YmTheme.blobs[0].copy(alpha = 0.22f * pulse),
            radius = 20f + 400f * pulse,
            center = Offset(size.width * 0.5f, size.height * 0.42f),
        )
    }
}
