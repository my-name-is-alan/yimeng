package art.yimeng.proto

import android.os.Build
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.draw.shadow
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import dev.chrisbanes.haze.HazeState
import dev.chrisbanes.haze.HazeStyle
import dev.chrisbanes.haze.HazeTint
import dev.chrisbanes.haze.hazeEffect
import dev.chrisbanes.haze.materials.ExperimentalHazeMaterialsApi
import dev.chrisbanes.haze.materials.HazeMaterials

@OptIn(ExperimentalHazeMaterialsApi::class)
@Composable
fun GlassCapsule(
    hazeState: HazeState,
    modifier: Modifier = Modifier,
    corner: Dp = 999.dp,
    content: @Composable BoxScope.() -> Unit,
) {
    val shape = RoundedCornerShape(corner)
    val useHaze = Build.VERSION.SDK_INT >= 31
    Box(
        modifier = modifier
            .shadow(14.dp, shape, ambientColor = Color.Black.copy(alpha = 0.30f), spotColor = Color.Black.copy(alpha = 0.30f))
            .clip(shape)
            .then(
                if (useHaze) {
                    Modifier.hazeEffect(state = hazeState, style = HazeMaterials.ultraThin())
                } else {
                    Modifier.background(YmTheme.base.copy(alpha = 0.55f))
                },
            )
            .background(Color.Black.copy(alpha = 0.14f))
            .drawWithContent {
                drawContent()
                drawRect(
                    brush = Brush.radialGradient(
                        colors = listOf(Color.White.copy(alpha = 0.14f), Color.Transparent),
                        center = Offset(size.width * 0.30f, 0f),
                        radius = size.width * 0.60f,
                    ),
                )
                val rim = Color.White.copy(alpha = 0.18f)
                drawRect(
                    brush = Brush.verticalGradient(
                        0f to rim,
                        0.08f to Color.Transparent,
                        0.92f to Color.Transparent,
                        1f to rim.copy(alpha = 0.10f),
                    ),
                )
            },
        content = content,
    )
}

@OptIn(ExperimentalHazeMaterialsApi::class)
@Composable
fun GlassOrb(
    hazeState: HazeState,
    modifier: Modifier = Modifier,
    tint: Color = YmTheme.blobs[0].copy(alpha = 0.42f),
    content: @Composable BoxScope.() -> Unit,
) {
    val useHaze = Build.VERSION.SDK_INT >= 31
    Box(
        modifier = modifier
            .shadow(10.dp, CircleShape, ambientColor = Color.Black.copy(alpha = 0.30f), spotColor = Color.Black.copy(alpha = 0.30f))
            .clip(CircleShape)
            .then(
                if (useHaze) {
                    Modifier.hazeEffect(
                        state = hazeState,
                        style = HazeStyle(
                            backgroundColor = Color.Transparent,
                            tints = listOf(HazeTint(tint)),
                            blurRadius = 24.dp,
                        ),
                    )
                } else {
                    Modifier.background(tint.copy(alpha = 0.55f))
                },
            )
            .background(tint.copy(alpha = 0.35f))
            .drawWithContent {
                drawContent()
                drawCircle(
                    brush = Brush.radialGradient(
                        colors = listOf(Color.White.copy(alpha = 0.35f), Color.Transparent),
                        center = Offset(size.width * 0.35f, size.height * 0.30f),
                        radius = size.minDimension * 0.55f,
                    ),
                )
            },
        content = content,
    )
}

fun glassBlurActive(): Boolean = Build.VERSION.SDK_INT >= 31
