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
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import dev.chrisbanes.haze.HazeState
import dev.chrisbanes.haze.HazeStyle
import dev.chrisbanes.haze.hazeEffect

private val Base = Color(0xFF0A0A0C)

/**
 * Real hazeEffect α=0.35 (do NOT substitute black 14% for that alpha).
 * Stack: haze(α=0.35, blur=20, no material tint) + black 14% + optional Base 55% (mini) + rim + shadow black30% r14 y6.
 */
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
                            tints = emptyList(), // no material tint
                            blurRadius = blurRadius,
                        ),
                    ) {
                        // Real haze alpha — not faked by black overlay alone.
                        alpha = 0.35f
                    }
                    else -> Modifier.background(Base.copy(alpha = 0.70f))
                },
            )
            // Keep black 14% tint (separate from haze α)
            .background(Color.Black.copy(alpha = 0.14f))
            // Mini player: Base #0A0A0C 55%
            .then(if (miniPlayerExtra) Modifier.background(Base.copy(alpha = 0.55f)) else Modifier)
            .then(
                when {
                    api >= 33 -> Modifier.glassRimApprox()
                    api >= 31 -> Modifier.glassRimStroke()
                    else -> Modifier.glassRimFallback()
                },
            )
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
                        color = android.graphics.Color.argb(
                            (255 * glowA).toInt(),
                            (tint.red * 255).toInt(),
                            (tint.green * 255).toInt(),
                            (tint.blue * 255).toInt(),
                        )
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
                    ) { alpha = 0.35f }
                } else Modifier.background(Base.copy(alpha = 0.70f)),
            )
            .background(Color.Black.copy(alpha = 0.14f))
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

/** Shadow black 30%, r14, y6. */
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
