package art.yimeng.proto

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.spring
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.draw.scale
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import android.provider.Settings
import androidx.compose.foundation.Canvas
import androidx.compose.material3.Text
import dev.chrisbanes.haze.HazeState
import dev.chrisbanes.haze.hazeSource

private data class Tab(val label: String, val glyph: YMGlyph)

@Composable
fun ProtoScreen() {
    val haze = remember { HazeState() }
    var tab by remember { mutableIntStateOf(0) }
    val track = DreamMock.orange
    val left = listOf(Tab("首页", YMGlyph.Home), Tab("曲库", YMGlyph.Library))
    val right = listOf(Tab("搜索", YMGlyph.Search), Tab("我的", YMGlyph.Me))
    val ctx = LocalContext.current
    val reduce = remember {
        Settings.Global.getFloat(ctx.contentResolver, Settings.Global.ANIMATOR_DURATION_SCALE, 1f) == 0f
    }
    var beatAgo by remember { mutableFloatStateOf(0.15f) }
    LaunchedEffect(Unit) {
        var last = withFrameNanos { it }
        while (true) {
            withFrameNanos { now ->
                val dt = (now - last) / 1e9f
                last = now
                beatAgo += dt
                val period = 60f / track.bpm
                if (beatAgo >= period) beatAgo -= period
            }
        }
    }
    var blurCmp by remember { mutableIntStateOf(20) }

    Box(Modifier.fillMaxSize().background(YmTheme.base)) {
        Box(Modifier.fillMaxSize().hazeSource(haze)) {
            DreamField(track, Modifier.fillMaxSize(), realPlayback = true, lastBeatAgoSec = beatAgo, reduceMotion = reduce)
            Box(
                Modifier
                    .align(Alignment.Center)
                    .padding(bottom = 100.dp)
                    .size(280.dp)
                    .clip(RoundedCornerShape(10.dp))
                    .background(Brush.linearGradient(track.blobs.take(3))),
                contentAlignment = Alignment.Center,
            ) {
                TextSafe("封面", Color.White.copy(0.7f), 18.sp, FontWeight.SemiBold)
            }
        }
        DoodleLayer(
            track = track,
            modifier = Modifier.fillMaxSize(),
            intensity = 0.7f,
            realPlayback = true,
            lastBeatAgoSec = beatAgo,
            reduceMotion = reduce,
        )

        Column(
            Modifier.align(Alignment.TopStart).statusBarsPadding().padding(horizontal = 20.dp, vertical = 12.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(10.dp)) {
                YMBrandMark(size = 28.dp)
                TextSafe("译梦 · 玻璃 + 涂鸦 PoC", YmTheme.fg, 20.sp, FontWeight.Bold)
            }
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                listOf(12, 16, 20, 24).forEach { r ->
                    TextSafe(
                        "${r}dp",
                        if (blurCmp == r) YmTheme.accent else YmTheme.secondary,
                        12.sp,
                        FontWeight.SemiBold,
                        modifier = Modifier
                            .clip(RoundedCornerShape(8.dp))
                            .background(if (blurCmp == r) Color.White.copy(0.12f) else Color.Transparent)
                            .clickable { blurCmp = r }
                            .padding(horizontal = 8.dp, vertical = 4.dp),
                    )
                }
            }
            // Icon set strip (port of YMIcons.swift) for review screenshots
            Row(
                Modifier.padding(top = 8.dp),
                horizontalArrangement = Arrangement.spacedBy(10.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                listOf(
                    YMGlyph.Home, YMGlyph.Library, YMGlyph.Collection, YMGlyph.Search, YMGlyph.Me,
                    YMGlyph.Play, YMGlyph.Pause, YMGlyph.Previous, YMGlyph.Next,
                    YMGlyph.Heart, YMGlyph.Star, YMGlyph.Recognize,
                ).forEach { g ->
                    YMIcon(g, size = 18.dp, weight = 1.6f, color = YmTheme.fg.copy(0.9f))
                }
            }
        }

        Column(
            Modifier.align(Alignment.BottomCenter).fillMaxWidth().navigationBarsPadding()
                .padding(horizontal = 20.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            GlassCapsule(
                hazeState = haze,
                modifier = Modifier.fillMaxWidth().height(56.dp),
                blurRadius = blurCmp.dp,
                miniPlayerExtra = true,
            ) {
                Row(
                    Modifier.fillMaxSize().padding(start = 8.dp, end = 16.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Box(Modifier.size(40.dp).clip(CircleShape).background(track.blobs[1]))
                    Spacer(Modifier.width(12.dp))
                    Column(Modifier.weight(1f)) {
                        TextSafe(track.title, YmTheme.fg, 14.sp, FontWeight.SemiBold, maxLines = 1)
                        TextSafe(track.artist, YmTheme.fg.copy(0.7f), 12.sp, maxLines = 1)
                    }
                    YMIcon(YMGlyph.Pause, size = 22.dp, weight = 1.9f, filled = true, color = YmTheme.fg)
                    Spacer(Modifier.width(14.dp))
                    YMIcon(YMGlyph.Next, size = 20.dp, weight = 1.9f, filled = true, color = YmTheme.fg)
                }
            }

            Box(Modifier.fillMaxWidth().height(62.dp)) {
                GlassCapsule(hazeState = haze, modifier = Modifier.fillMaxSize(), blurRadius = blurCmp.dp) {
                    Row(Modifier.fillMaxSize().padding(horizontal = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                        left.forEachIndexed { i, t ->
                            TabItem(t, selected = tab == i, onClick = { tab = i }, modifier = Modifier.weight(1f))
                        }
                        Spacer(Modifier.width(76.dp))
                        right.forEachIndexed { i, t ->
                            TabItem(t, selected = tab == i + 2, onClick = { tab = i + 2 }, modifier = Modifier.weight(1f))
                        }
                    }
                }
                CollectionOrb(
                    haze = haze,
                    selected = tab == -1,
                    tint = track.blobs[0],
                    modifier = Modifier.align(Alignment.TopCenter).offset(y = (-18).dp).size(64.dp),
                    onClick = { tab = -1 },
                )
            }
        }
    }
}

@Composable
private fun TextSafe(
    text: String,
    color: Color,
    size: androidx.compose.ui.unit.TextUnit,
    weight: FontWeight = FontWeight.Normal,
    maxLines: Int = Int.MAX_VALUE,
    modifier: Modifier = Modifier,
) {
    Text(
        text = text,
        color = color,
        fontSize = size,
        fontWeight = weight,
        maxLines = maxLines,
        overflow = TextOverflow.Ellipsis,
        modifier = modifier,
    )
}

@Composable
private fun TabItem(t: Tab, selected: Boolean, onClick: () -> Unit, modifier: Modifier = Modifier) {
    Box(
        modifier
            .height(54.dp)
            .clip(RoundedCornerShape(999.dp))
            .background(if (selected) Color.White.copy(alpha = 0.128f) else Color.Transparent)
            .clickable(onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            YMIcon(
                glyph = t.glyph,
                size = 19.dp,
                weight = 1.7f,
                active = selected,
                color = if (selected) YmTheme.accent else YmTheme.fg,
            )
            TextSafe(
                t.label,
                if (selected) YmTheme.accent else YmTheme.fg.copy(0.75f),
                10.sp,
                FontWeight.SemiBold,
                maxLines = 1,
            )
        }
    }
}

@Composable
private fun CollectionOrb(
    haze: HazeState,
    selected: Boolean,
    tint: Color,
    modifier: Modifier = Modifier,
    onClick: () -> Unit,
) {
    val scale = remember { Animatable(1f) }
    val starScale = remember { Animatable(0.9f) }
    LaunchedEffect(selected) {
        if (selected) {
            scale.animateTo(1.05f, spring(dampingRatio = 0.6f, stiffness = 195f))
            starScale.animateTo(1.15f, spring(dampingRatio = 0.6f, stiffness = 195f))
        } else {
            scale.animateTo(1f, spring(dampingRatio = 0.6f, stiffness = 195f))
            starScale.animateTo(0.9f, spring(dampingRatio = 0.6f, stiffness = 195f))
        }
    }
    GlassOrb(
        hazeState = haze,
        tint = tint.copy(alpha = 0.42f),
        selected = selected,
        modifier = modifier
            .scale(scale.value)
            .pointerInput(Unit) {
                detectTapGestures(
                    onPress = {
                        scale.snapTo(0.96f)
                        tryAwaitRelease()
                        scale.animateTo(if (selected) 1.05f else 1f, spring(0.6f, 195f))
                    },
                    onTap = { onClick() },
                )
            },
    ) {
        Column(
            Modifier.fillMaxSize(),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.Center,
        ) {
            // iOS Tab.collection → YMGlyph.Collection (stacks)
            YMIcon(
                glyph = YMGlyph.Collection,
                size = 22.dp,
                weight = 1.7f,
                color = Color.White,
                modifier = Modifier.scale(starScale.value),
            )
            TextSafe("典藏", Color.White.copy(0.92f), 10.sp, FontWeight.SemiBold)
            if (selected) {
                Box(Modifier.padding(top = 2.dp).size(5.dp).clip(CircleShape).background(YmTheme.accent))
            }
        }
    }
}
