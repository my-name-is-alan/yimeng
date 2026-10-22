package art.yimeng.proto

import android.graphics.BlurMaskFilter
import android.os.Build
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.draw.drawWithCache
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.geometry.CornerRadius
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.RoundRect
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Paint
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.drawIntoCanvas
import androidx.compose.ui.graphics.nativeCanvas
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import dev.chrisbanes.haze.HazeState
import dev.chrisbanes.haze.HazeStyle
import dev.chrisbanes.haze.hazeEffect

private val Base = Color(0xFF0A0A0C)

/** Designer review: HazeStyle(bg=#0A0A0C, no tints, blur=20) + alpha 0.35; black 14%; soft shadow; glassRim. */
@Composable
fun GlassCapsule(
    hazeState: HazeState,
    modifier: Modifier = Modifier,
    corner: Dp = 999.dp,
    blurRadius: Dp = 20.dp,
    miniPlayerExtra: Boolean = false,
    content: @Composable BoxScope.() -> Unit,
) {
    val shape = RoundedCornerShape(corner)
    val api = Build.VERSION.SDK_INT
    Box(
        modifier = modifier
            .glassSoftShadow(shapeCorner = corner)
            .clip(shape)
            .then(
                when {
                    api >= 31 -> Modifier.hazeEffect(
                        state = hazeState,
                        style = HazeStyle(
                            backgroundColor = Base,
                            tints = emptyList(),
                            blurRadius = blurRadius,
                        ),
                    )
                    else -> Modifier.background(Base.copy(alpha = 0.70f))
                },
            )
            // hazeEffect itself; enforce 0.35 feel with black 14% tint always
            .background(Color.Black.copy(alpha = 0.14f))
            .then(if (miniPlayerExtra) Modifier.background(Base.copy(alpha = 0.55f)) else Modifier)
            .then(if (api in 31..32) Modifier.glassRimStroke() else if (api >= 33) Modifier.glassRimApprox() else Modifier.glassRimFallback())
            .drawWithContent {
                drawContent()
                drawRect(
                    brush = Brush.radialGradient(
                        colors = listOf(Color.White.copy(alpha = 0.14f), Color.Transparent),
                        center = Offset(size.width * 0.30f, 0f),
                        radius = size.width * 0.60f,
                    ),
                )
            },
        content = content,
    )
}

@Composable
fun GlassOrb(
    hazeState: HazeState,
    modifier: Modifier = Modifier,
    tint: Color = Color(0xFF6B4BD8).copy(alpha = 0.42f),
    selected: Boolean = false,
    content: @Composable BoxScope.() -> Unit,
) {
    val api = Build.VERSION.SDK_INT
    val glowA = if (selected) 0.55f else 0.32f
    val glowBlur = if (selected) 14.dp else 10.dp
    Box(
        modifier = modifier
            .drawBehind {
                drawIntoCanvas { canvas ->
                    val p = Paint().asFrameworkPaint().apply {
                        isAntiAlias = true
                        color = android.graphics.Color.argb((255 * glowA).toInt(), tint.red.times(255).toInt(), tint.green.times(255).toInt(), tint.blue.times(255).toInt())
                        maskFilter = BlurMaskFilter(glowBlur.toPx(), BlurMaskFilter.Blur.NORMAL)
                    }
                    canvas.nativeCanvas.drawCircle(size.width / 2f, size.height / 2f + 4f, size.minDimension / 2f, p)
                }
            }
            .glassSoftShadow(shapeCorner = 999.dp, blur = 10.dp, dy = 5.dp)
            .clip(CircleShape)
            .then(
                if (api >= 31) {
                    Modifier.hazeEffect(
                        state = hazeState,
                        style = HazeStyle(backgroundColor = Base, tints = emptyList(), blurRadius = 20.dp),
                    )
                } else Modifier.background(Base.copy(alpha = 0.70f)),
            )
            .background(tint)
            .drawWithContent {
                drawContent()
                drawCircle(
                    brush = Brush.radialGradient(
                        colors = listOf(Color.White.copy(alpha = 0.35f), Color.Transparent),
                        center = Offset(size.width * 0.30f, size.height * 0.15f),
                        radius = 40f,
                    ),
                )
                drawCircle(Color.White.copy(alpha = 0.40f), style = Stroke(width = 0.8f * density))
            },
        content = content,
    )
}

private fun Modifier.glassSoftShadow(
    shapeCorner: Dp,
    blur: Dp = 14.dp,
    dy: Dp = 6.dp,
): Modifier = drawBehind {
    drawIntoCanvas { canvas ->
        val paint = Paint().asFrameworkPaint().apply {
            isAntiAlias = true
            color = android.graphics.Color.argb((255 * 0.30f).toInt(), 0, 0, 0)
            maskFilter = BlurMaskFilter(blur.toPx(), BlurMaskFilter.Blur.NORMAL)
        }
        val r = shapeCorner.toPx().coerceAtMost(size.minDimension / 2f)
        canvas.nativeCanvas.drawRoundRect(
            0f, dy.toPx(), size.width, size.height + dy.toPx(), r, r, paint,
        )
    }
}

/** API 33+ approx of glassRim (full AGSL later); TL/BR bright ~1.6px + inner 10px glow 10%. */
private fun Modifier.glassRimApprox(): Modifier = drawWithCache {
    val stroke = 1.6f
    onDrawWithContent {
        drawContent()
        val path = Path().apply {
            addRoundRect(RoundRect(0f, 0f, size.width, size.height, CornerRadius(size.minDimension / 2f)))
        }
        drawPath(
            path,
            brush = Brush.linearGradient(
                colors = listOf(Color.White.copy(0.40f), Color.White.copy(0.15f)),
                start = Offset.Zero,
                end = Offset(size.width, size.height),
            ),
            style = Stroke(width = stroke),
        )
        drawPath(path, Color.White.copy(0.10f), style = Stroke(width = 10f))
    }
}

/** API 31–32: 1dp stroke white gradient 40%/15%. */
private fun Modifier.glassRimStroke(): Modifier = drawWithCache {
    onDrawWithContent {
        drawContent()
        drawRoundRect(
            brush = Brush.linearGradient(
                listOf(Color.White.copy(0.40f), Color.White.copy(0.15f)),
                Offset.Zero, Offset(size.width, size.height),
            ),
            cornerRadius = CornerRadius(size.minDimension / 2f),
            style = Stroke(width = 1.dp.toPx()),
        )
    }
}

/** <31: base already 70%; highlight + rim. */
private fun Modifier.glassRimFallback(): Modifier = drawWithContent {
    drawContent()
    drawRoundRect(
        brush = Brush.linearGradient(
            listOf(Color.White.copy(0.40f), Color.White.copy(0.15f)),
            Offset.Zero, Offset(size.width, size.height),
        ),
        cornerRadius = CornerRadius(size.minDimension / 2f),
        style = Stroke(width = 1.dp.toPx()),
    )
}

fun glassBlurActive(): Boolean = Build.VERSION.SDK_INT >= 31
