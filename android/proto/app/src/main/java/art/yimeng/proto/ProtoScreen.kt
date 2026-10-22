package art.yimeng.proto

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
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
import androidx.compose.material.icons.filled.Home
import androidx.compose.material.icons.filled.LibraryMusic
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.Search
import androidx.compose.material.icons.rounded.Pause
import androidx.compose.material.icons.rounded.SkipNext
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import dev.chrisbanes.haze.HazeState
import dev.chrisbanes.haze.hazeSource

private data class Tab(val label: String, val icon: ImageVector)

@Composable
fun ProtoScreen() {
    val haze = remember { HazeState() }
    var tab by remember { mutableIntStateOf(0) }
    val left = listOf(Tab("首页", Icons.Filled.Home), Tab("曲库", Icons.Filled.LibraryMusic))
    val right = listOf(Tab("搜索", Icons.Filled.Search), Tab("我的", Icons.Filled.Person))

    Box(Modifier.fillMaxSize().background(YmTheme.base)) {
        Box(Modifier.fillMaxSize().hazeSource(haze)) {
            DreamField(Modifier.fillMaxSize())
            DoodleLayer(Modifier.fillMaxSize(), intensity = 0.75f, bpm = 96f)
            Box(
                Modifier
                    .align(Alignment.Center)
                    .padding(bottom = 100.dp)
                    .size(280.dp)
                    .clip(RoundedCornerShape(10.dp))
                    .background(Brush.linearGradient(listOf(YmTheme.blobs[0], YmTheme.blobs[1], YmTheme.blobs[2]))),
                contentAlignment = Alignment.Center,
            ) {
                Text("封面", color = Color.White.copy(0.7f), fontSize = 18.sp, fontWeight = FontWeight.SemiBold)
            }
            Column(
                Modifier.align(Alignment.TopStart).statusBarsPadding().padding(horizontal = 20.dp, vertical = 12.dp),
            ) {
                Text("译梦 · 玻璃 + 涂鸦 PoC", color = YmTheme.fg, fontSize = 20.sp, fontWeight = FontWeight.Bold)
                Text(
                    if (glassBlurActive()) "Haze 模糊开启 · API ${android.os.Build.VERSION.SDK_INT}"
                    else "半透明回退 · API ${android.os.Build.VERSION.SDK_INT} (<31)",
                    color = YmTheme.secondary,
                    fontSize = 13.sp,
                )
                Text("Tab / 迷你播放器 = 玻璃；涂鸦随节拍轻呼吸", color = YmTheme.secondary, fontSize = 12.sp)
            }
        }

        Column(
            Modifier.align(Alignment.BottomCenter).fillMaxWidth().navigationBarsPadding()
                .padding(horizontal = 16.dp, vertical = 10.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            GlassCapsule(hazeState = haze, modifier = Modifier.fillMaxWidth().height(56.dp)) {
                Row(
                    Modifier.fillMaxSize().padding(start = 8.dp, end = 16.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    Box(Modifier.size(40.dp).clip(CircleShape).background(YmTheme.blobs[1]))
                    Spacer(Modifier.width(12.dp))
                    Column(Modifier.weight(1f)) {
                        Text("海阔天空", color = YmTheme.fg, fontSize = 14.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text("Beyond", color = YmTheme.fg.copy(0.7f), fontSize = 12.sp)
                    }
                    Icon(Icons.Rounded.Pause, null, tint = YmTheme.fg, modifier = Modifier.size(22.dp))
                    Spacer(Modifier.width(14.dp))
                    Icon(Icons.Rounded.SkipNext, null, tint = YmTheme.fg, modifier = Modifier.size(20.dp))
                }
            }

            Box(Modifier.fillMaxWidth().height(62.dp)) {
                GlassCapsule(hazeState = haze, modifier = Modifier.fillMaxSize()) {
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
                GlassOrb(
                    hazeState = haze,
                    modifier = Modifier.align(Alignment.TopCenter).offset(y = (-18).dp).size(64.dp),
                ) {
                    Column(Modifier.fillMaxSize(), horizontalAlignment = Alignment.CenterHorizontally, verticalArrangement = Arrangement.Center) {
                        Text("☽", fontSize = 22.sp, color = Color.White)
                        Text("典藏", fontSize = 10.sp, color = Color.White.copy(0.85f))
                    }
                }
            }
        }
    }
}

@Composable
private fun TabItem(t: Tab, selected: Boolean, onClick: () -> Unit, modifier: Modifier = Modifier) {
    Column(
        modifier.height(54.dp).clickable(onClick = onClick),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        Box(
            Modifier
                .clip(RoundedCornerShape(999.dp))
                .background(if (selected) YmTheme.fill.copy(alpha = 0.25f) else Color.Transparent)
                .padding(horizontal = 14.dp, vertical = 4.dp),
        ) {
            Icon(t.icon, t.label, tint = if (selected) YmTheme.accent else YmTheme.fg.copy(0.75f), modifier = Modifier.size(19.dp))
        }
        Text(t.label, color = if (selected) YmTheme.accent else YmTheme.fg.copy(0.75f), fontSize = 10.sp, fontWeight = FontWeight.SemiBold)
    }
}
