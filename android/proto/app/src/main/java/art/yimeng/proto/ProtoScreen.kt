package art.yimeng.proto

import androidx.compose.animation.core.Animatable
import androidx.compose.animation.core.Spring
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
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.Home
import androidx.compose.material.icons.rounded.LibraryMusic
import androidx.compose.material.icons.rounded.Person
import androidx.compose.material.icons.rounded.Search
import androidx.compose.material.icons.rounded.Pause
import androidx.compose.material.icons.rounded.SkipNext
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
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
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import android.provider.Settings
import dev.chrisbanes.haze.HazeState
import dev.chrisbanes.haze.hazeSource
import kotlin.math.sin
import kotlin.math.PI

private data class Tab(val label: String, val icon: ImageVector)

@Composable
fun ProtoScreen() {
    val haze = remember { HazeState() }
    var tab by remember { mutableIntStateOf(0) }
    val track = DreamMock.orange
    val left = listOf(Tab("首页", Icons.Rounded.Home), Tab("曲库", Icons.Rounded.LibraryMusic))
    val right = listOf(Tab("搜索", Icons.Rounded.Search), Tab("我的", Icons.Rounded.Person))
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
                // demo beats at bpm
                val period = 60f / track.bpm
                if (beatAgo >= period) beatAgo -= period
            }
        }
    }
    var blurCmp by remember { mutableIntStateOf(20) } // 12/16/20/24 comparison

    Box(Modifier.fillMaxSize().background(YmTheme.base)) {
        // Background + cover in hazeSource; doodle OUTSIDE hazeSource per review
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
                Text("封面", color = Color.White.copy(0.7f), fontSize = 18.sp, fontWeight = FontWeight.SemiBold)
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
            Text("译梦 · 玻璃 + 涂鸦 PoC", color = YmTheme.fg, fontSize = 20.sp, fontWeight = FontWeight.Bold)
            Text(
                "Haze ${blurCmp}dp · motif=${track.motif} · " + if (glassBlurActive()) "API≥31" else "API<31",
                color = YmTheme.secondary, fontSize = 13.sp,
            )
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                listOf(12, 16, 20, 24).forEach { r ->
                    Text(
                        "${r}dp",
                        color = if (blurCmp == r) YmTheme.accent else YmTheme.secondary,
                        fontSize = 12.sp,
                        fontWeight = FontWeight.SemiBold,
                        modifier = Modifier
                            .clip(RoundedCornerShape(8.dp))
                            .background(if (blurCmp == r) Color.White.copy(0.12f) else Color.Transparent)
                            .clickable { blurCmp = r }
                            .padding(horizontal = 8.dp, vertical = 4.dp),
                    )
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
                        Text(track.title, color = YmTheme.fg, fontSize = 14.sp, fontWeight = FontWeight.SemiBold,
                            maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text(track.artist, color = YmTheme.fg.copy(0.7f), fontSize = 12.sp,
                            maxLines = 1, overflow = TextOverflow.Ellipsis)
                    }
                    Icon(Icons.Rounded.Pause, null, tint = YmTheme.fg, modifier = Modifier.size(22.dp))
                    Spacer(Modifier.width(14.dp))
                    Icon(Icons.Rounded.SkipNext, null, tint = YmTheme.fg, modifier = Modifier.size(20.dp))
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
            Icon(t.icon, t.label, tint = if (selected) YmTheme.accent else YmTheme.fg.copy(0.75f), modifier = Modifier.size(19.dp))
            Text(t.label, color = if (selected) YmTheme.accent else YmTheme.fg.copy(0.75f), fontSize = 10.sp, fontWeight = FontWeight.SemiBold,
                maxLines = 1, overflow = TextOverflow.Ellipsis)
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
        Column(Modifier.fillMaxSize(), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
            Box {
                Text("☽", fontSize = 22.sp, color = Color.White.copy(0.90f), modifier = Modifier.rotate(if (selected) -12f else 0f))
                Text("✧", fontSize = 9.sp, color = Color.White.copy(0.90f),
                    modifier = Modifier.align(Alignment.TopEnd).offset(x = 6.dp, y = (-2).dp).scale(starScale.value))
                Text("☽", fontSize = 22.sp, color = Color.White.copy(0.35f),
                    modifier = Modifier.offset(x = 0.8.dp, y = (-0.6).dp).rotate(if (selected) -12f else 0f))
            }
            Text("典藏", fontSize = 10.sp, fontWeight = FontWeight.SemiBold, color = Color.White.copy(0.92f))
            if (selected) {
                Box(Modifier.padding(top = 2.dp).size(5.dp).clip(CircleShape).background(YmTheme.accent))
            }
        }
    }
}
