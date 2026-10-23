package art.yimeng.proto

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.size
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.rotate
import androidx.compose.ui.graphics.drawscope.translate
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp

/**
 * Transition brand marks only — dream-ring + spark.
 * No Material / Segoe glyphs until the shared iOS icon set lands.
 */
@Composable
fun DreamRingSpark(
    modifier: Modifier = Modifier,
    color: Color = Color.White,
    size: Dp = 19.dp,
    selected: Boolean = false,
) {
    val a = if (selected) 1f else 0.75f
    Canvas(modifier.size(size)) {
        val r = size.toPx() / 2f
        val stroke = (1.4f * density).coerceAtLeast(1.2f)
        // dream-ring
        drawCircle(
            color = color.copy(alpha = 0.85f * a),
            radius = r * 0.72f,
            style = Stroke(width = stroke, cap = StrokeCap.Round),
        )
        // inner spark (4-point)
        val c = Offset(r, r)
        val spark = DoodlePaths.sparkle(c, r * 0.28f)
        drawPath(
            spark,
            color = color.copy(alpha = 0.95f * a),
            style = Stroke(width = stroke * 0.7f, cap = StrokeCap.Round, join = StrokeJoin.Round),
        )
    }
}

/** Geometric transport — pause bars / skip chevron. No Material icons. */
@Composable
fun PauseMark(modifier: Modifier = Modifier, color: Color = Color.White, size: Dp = 22.dp) {
    Canvas(modifier.size(size)) {
        val w = size.toPx(); val h = w
        val barW = w * 0.18f; val gap = w * 0.16f; val barH = h * 0.55f
        val top = (h - barH) / 2f
        val left = w / 2f - gap / 2f - barW
        drawRoundRect(color, Offset(left, top), Size(barW, barH),
            cornerRadius = CornerRadius(barW / 3f))
        drawRoundRect(color, Offset(left + barW + gap, top), Size(barW, barH),
            cornerRadius = CornerRadius(barW / 3f))
    }
}

@Composable
fun SkipMark(modifier: Modifier = Modifier, color: Color = Color.White, size: Dp = 20.dp) {
    Canvas(modifier.size(size)) {
        val w = size.toPx(); val h = w
        val p = Path().apply {
            moveTo(w * 0.22f, h * 0.22f)
            lineTo(w * 0.62f, h * 0.50f)
            lineTo(w * 0.22f, h * 0.78f)
            close()
        }
        drawPath(p, color)
        drawRect(color, Offset(w * 0.68f, h * 0.22f), Size(w * 0.12f, h * 0.56f))
    }
}

/** Crayon double-stroke: main 90% + second pass 35% @ offset(0.8,−0.6) rotate 0.8°. */
fun androidx.compose.ui.graphics.drawscope.DrawScope.crayonStroke(
    path: Path,
    color: Color,
    widthPx: Float,
) {
    drawPath(
        path, color.copy(alpha = color.alpha * 0.9f),
        style = Stroke(width = widthPx, cap = StrokeCap.Round, join = StrokeJoin.Round),
    )
    translate(0.8f * density, -0.6f * density) {
        rotate(0.8f, pivot = Offset(size.width / 2f, size.height / 2f)) {
            drawPath(
                path, color.copy(alpha = color.alpha * 0.35f),
                style = Stroke(width = widthPx * 0.7f, cap = StrokeCap.Round, join = StrokeJoin.Round),
            )
        }
    }
}

fun moonPath(c: Offset, r: Float): Path = DoodlePaths.moon(c, r)
fun starSparkPath(c: Offset, r: Float): Path = DoodlePaths.sparkle(c, r)
