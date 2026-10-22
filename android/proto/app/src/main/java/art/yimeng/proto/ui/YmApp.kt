package art.yimeng.proto.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
// offset via layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import art.yimeng.proto.*
import art.yimeng.proto.core.*
import coil.compose.AsyncImage
import dev.chrisbanes.haze.HazeState
import dev.chrisbanes.haze.hazeSource
import kotlinx.coroutines.launch

private val Lime = Color(0xFFC8F542)
private val Base = Color(0xFF0A0A0C)

enum class MainTab { Home, Library, Collection, Search, Me }

@Composable
fun YmApp() {
    val ctx = LocalContext.current
    val haze = remember { HazeState() }
    var tab by remember { mutableStateOf(MainTab.Home) }
    var showPlayer by remember { mutableStateOf(false) }
    var now by remember { mutableStateOf<CoreTrack?>(null) }
    var home by remember { mutableStateOf<HomePayload?>(null) }
    var fm by remember { mutableStateOf<List<CoreTrack>>(emptyList()) }
    var status by remember { mutableStateOf("连接 Core…") }
    val scope = rememberCoroutineScope()

    LaunchedEffect(Unit) {
        status = "登录…"
        val ok = CoreApi.ensureLogin(ctx)
        if (!ok) {
            status = "Core 失败：${CoreApi.lastError ?: "unknown"}"
            return@LaunchedEffect
        }
        status = "拉取首页…"
        home = CoreApi.fetchHome()
        fm = CoreApi.fetchFm("netease")
        status = if (home == null) "首页空：${CoreApi.lastError}" else "已连接 · ${CoreApi.userLabel}"
        if (now == null) {
            now = home?.sections?.firstOrNull { it.kind == "tracks" && it.tracks.isNotEmpty() }?.tracks?.firstOrNull()
                ?: home?.recentlyPlayed?.firstOrNull()
        }
    }

    fun play(t: CoreTrack) {
        now = t
        showPlayer = true
    }

    Box(Modifier.fillMaxSize().background(Base)) {
        DreamField(
            track = DreamMock.orange,
            modifier = Modifier.fillMaxSize().hazeSource(haze),
            realPlayback = true,
            lastBeatAgoSec = 0.2f,
        )

        if (showPlayer && now != null) {
            PlayerPage(
                track = now!!,
                onBack = { showPlayer = false },
                modifier = Modifier.fillMaxSize(),
            )
        } else {
            Column(Modifier.fillMaxSize()) {
                Box(Modifier.weight(1f).fillMaxWidth()) {
                    when (tab) {
                        MainTab.Home -> HomePage(home, fm, status, onPlay = ::play)
                        MainTab.Library -> LibraryPage(home, onPlay = ::play)
                        MainTab.Collection -> CollectionPage(onPlay = ::play)
                        MainTab.Search -> SearchPage(onPlay = ::play)
                        MainTab.Me -> MePage(status)
                    }
                }
                // Mini player (hidden when full player open)
                now?.let { t ->
                    MiniBar(haze, t, onOpen = { showPlayer = true }, onPause = {})
                }
                BottomTabs(haze, tab) { tab = it }
            }
        }
    }
}

@Composable
private fun HomePage(
    home: HomePayload?,
    fm: List<CoreTrack>,
    status: String,
    onPlay: (CoreTrack) -> Unit,
) {
    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(horizontal = 20.dp, vertical = 16.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically) {
            YMBrandMark(size = 28.dp)
            Spacer(Modifier.width(10.dp))
            Text("首页", color = Color.White, fontSize = 26.sp, fontWeight = FontWeight.Bold)
        }
        Text(status, color = Color.White.copy(0.45f), fontSize = 12.sp, modifier = Modifier.padding(top = 4.dp, bottom = 14.dp))

        val daily = home?.sections?.firstOrNull { it.id.contains("daily") }
        val recPl = home?.sections?.firstOrNull { it.id.contains("playlists") && it.kind == "playlists" && !it.id.contains("mine") && !it.id.contains("charts") }
        val yueqian = daily?.tracks?.firstOrNull()

        // 今日乐签
        SectionTitle("今日乐签")
        YueqianCard(yueqian, onPlay)
        Spacer(Modifier.height(18.dp))

        // 每日推荐 — dated card (iOS DailyMixCard language)
        if (daily != null && daily.tracks.isNotEmpty()) {
            SectionTitle(daily.title)
            DailyMixCard(daily.tracks, onPlay)
            Spacer(Modifier.height(18.dp))
        }

        // 私人 FM
        SectionTitle("网易云音乐 · 私人 FM")
        FmCard(fm.firstOrNull(), onPlay)
        Spacer(Modifier.height(18.dp))

        // 推荐歌单
        if (recPl != null && recPl.playlists.isNotEmpty()) {
            SectionTitle(recPl.title)
            Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                recPl.playlists.take(12).forEach { p ->
                    CoverCard(p.name, p.origin ?: "", p.coverUrl, 118.dp) {}
                }
            }
            Spacer(Modifier.height(18.dp))
        }

        // 最近播放
        if (!home?.recentlyPlayed.isNullOrEmpty()) {
            SectionTitle("最近播放")
            home!!.recentlyPlayed.take(8).forEach { t ->
                TrackRow(t) { onPlay(t) }
            }
        }
        Spacer(Modifier.height(24.dp))
    }
}

@Composable
private fun LibraryPage(home: HomePayload?, onPlay: (CoreTrack) -> Unit) {
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(20.dp)) {
        Text("曲库", color = Color.White, fontSize = 26.sp, fontWeight = FontWeight.Bold)
        Text("平台透传 · 歌单 / 排行", color = Color.White.copy(0.45f), fontSize = 12.sp, modifier = Modifier.padding(bottom = 14.dp))
        home?.sections?.filter { it.kind == "playlists" }?.forEach { sec ->
            SectionTitle(sec.title)
            Row(Modifier.horizontalScroll(rememberScrollState()), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                sec.playlists.take(10).forEach { p ->
                    CoverCard(p.name, "", p.coverUrl, 108.dp) {}
                }
            }
            Spacer(Modifier.height(16.dp))
        }
        if (home == null) Text("加载中…", color = Color.White.copy(0.5f))
    }
}

@Composable
private fun CollectionPage(onPlay: (CoreTrack) -> Unit) {
    var items by remember { mutableStateOf<List<CoreTrack>>(emptyList()) }
    var err by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) {
        items = CoreApi.favorites()
        err = CoreApi.lastError
    }
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(20.dp)) {
        Text("典藏", color = Color.White, fontSize = 26.sp, fontWeight = FontWeight.Bold)
        Text("收藏 / 点赞", color = Color.White.copy(0.45f), fontSize = 12.sp, modifier = Modifier.padding(bottom = 14.dp))
        if (items.isEmpty()) {
            Text(err ?: "暂无收藏曲目", color = Color.White.copy(0.55f))
            Spacer(Modifier.height(12.dp))
            Text("（骨架：接 Core /favorites）", color = Color.White.copy(0.35f), fontSize = 12.sp)
        } else items.forEach { TrackRow(it) { onPlay(it) } }
    }
}

@Composable
private fun SearchPage(onPlay: (CoreTrack) -> Unit) {
    var q by remember { mutableStateOf("梁博") }
    var results by remember { mutableStateOf<List<CoreTrack>>(emptyList()) }
    var busy by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    Column(Modifier.fillMaxSize().padding(20.dp)) {
        Text("搜索", color = Color.White, fontSize = 26.sp, fontWeight = FontWeight.Bold)
        Spacer(Modifier.height(12.dp))
        Row(
            Modifier.fillMaxWidth().clip(RoundedCornerShape(14.dp)).background(Color.White.copy(0.1f)).padding(12.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            YMIcon(YMGlyph.Search, size = 18.dp, color = Color.White.copy(0.7f))
            Spacer(Modifier.width(8.dp))
            Text(
                q.ifEmpty { "搜歌曲、歌手、专辑" },
                color = Color.White.copy(if (q.isEmpty()) 0.4f else 0.9f),
                fontSize = 15.sp,
                modifier = Modifier.weight(1f).clickable {
                    // cycle demo queries for skeleton screenshots
                    q = when (q) {
                        "梁博" -> "陈粒"
                        "陈粒" -> "A-LIN"
                        else -> "梁博"
                    }
                    scope.launch {
                        busy = true
                        results = CoreApi.search(q)
                        busy = false
                    }
                },
            )
            Text("搜", color = Lime, fontWeight = FontWeight.SemiBold, modifier = Modifier.clickable {
                scope.launch {
                    busy = true
                    results = CoreApi.search(q)
                    busy = false
                }
            })
        }
        Spacer(Modifier.height(12.dp))
        if (busy) Text("搜索中…", color = Color.White.copy(0.5f))
        Column(Modifier.verticalScroll(rememberScrollState())) {
            results.forEach { TrackRow(it) { onPlay(it) } }
            if (!busy && results.isEmpty()) Text("点「搜」或点查询词切换", color = Color.White.copy(0.4f), fontSize = 13.sp)
        }
    }
}

@Composable
private fun MePage(status: String) {
    Column(Modifier.fillMaxSize().padding(20.dp)) {
        Text("我的", color = Color.White, fontSize = 26.sp, fontWeight = FontWeight.Bold)
        Spacer(Modifier.height(16.dp))
        Row(verticalAlignment = Alignment.CenterVertically) {
            Box(Modifier.size(56.dp).clip(CircleShape).background(Color(0xFF6B4BD8)), contentAlignment = Alignment.Center) {
                YMIcon(YMGlyph.Me, size = 28.dp, color = Color.White)
            }
            Spacer(Modifier.width(14.dp))
            Column {
                Text(CoreApi.userLabel.ifEmpty { "未登录" }, color = Color.White, fontSize = 18.sp, fontWeight = FontWeight.SemiBold)
                Text(status, color = Color.White.copy(0.5f), fontSize = 12.sp)
            }
        }
        Spacer(Modifier.height(24.dp))
        listOf("设置", "音质", "下载", "关于译梦").forEach {
            Text(it, color = Color.White.copy(0.85f), fontSize = 16.sp, modifier = Modifier.padding(vertical = 12.dp))
        }
    }
}

@Composable
private fun PlayerPage(track: CoreTrack, onBack: () -> Unit, modifier: Modifier = Modifier) {
    Box(modifier.background(Base)) {
        DreamField(DreamMock.orange, Modifier.fillMaxSize(), realPlayback = true, lastBeatAgoSec = 0.15f)
        Column(Modifier.fillMaxSize().padding(20.dp), horizontalAlignment = Alignment.CenterHorizontally) {
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                Row(
                    Modifier.clip(RoundedCornerShape(8.dp)).background(Color.White.copy(0.1f)).clickable(onClick = onBack).padding(8.dp, 6.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    YMIcon(YMGlyph.Back, size = 16.dp, color = Color.White.copy(0.85f))
                    Spacer(Modifier.width(4.dp))
                    Text("返回", color = Color.White.copy(0.85f), fontSize = 13.sp)
                }
            }
            Spacer(Modifier.height(12.dp))
            Box(Modifier.size(280.dp), contentAlignment = Alignment.Center) {
                DoodleLayer(DreamMock.orange, Modifier.fillMaxSize(), intensity = 0.7f)
                CoverImage(track.coverUrl, Modifier.size(220.dp).clip(RoundedCornerShape(18.dp)), fallback = Color(0xFFFF7A1A))
            }
            Spacer(Modifier.height(20.dp))
            Text(track.title, color = Color.White, fontSize = 22.sp, fontWeight = FontWeight.Bold, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(track.artist, color = Color.White.copy(0.7f), fontSize = 14.sp)
            Spacer(Modifier.height(18.dp))
            Box(Modifier.fillMaxWidth().height(4.dp).clip(RoundedCornerShape(2.dp)).background(Color.White.copy(0.2f))) {
                Box(Modifier.fillMaxWidth(0.35f).fillMaxHeight().background(Lime))
            }
            Row(Modifier.fillMaxWidth().padding(top = 6.dp), horizontalArrangement = Arrangement.SpaceBetween) {
                Text("1:24", color = Color.White.copy(0.5f), fontSize = 11.sp)
                Text("4:02", color = Color.White.copy(0.5f), fontSize = 11.sp)
            }
            Spacer(Modifier.height(22.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(36.dp), verticalAlignment = Alignment.CenterVertically) {
                YMIcon(YMGlyph.Previous, size = 26.dp, filled = true, color = Color.White)
                YMIcon(YMGlyph.Pause, size = 40.dp, filled = true, color = Color.White)
                YMIcon(YMGlyph.Next, size = 26.dp, filled = true, color = Color.White)
            }
        }
    }
}

@Composable
private fun MiniBar(haze: HazeState, track: CoreTrack, onOpen: () -> Unit, onPause: () -> Unit) {
    GlassCapsule(
        hazeState = haze,
        modifier = Modifier.padding(horizontal = 16.dp, vertical = 8.dp).fillMaxWidth().height(56.dp).clickable(onClick = onOpen),
        miniPlayerExtra = true,
    ) {
        Row(Modifier.fillMaxSize().padding(horizontal = 10.dp), verticalAlignment = Alignment.CenterVertically) {
            CoverImage(track.coverUrl, Modifier.size(40.dp).clip(RoundedCornerShape(8.dp)), fallback = Color(0xFFFF7A1A))
            Spacer(Modifier.width(10.dp))
            Column(Modifier.weight(1f)) {
                Text(track.title, color = Color.White, fontSize = 14.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
                Text(track.artist, color = Color.White.copy(0.7f), fontSize = 12.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
            }
            YMIcon(YMGlyph.Pause, size = 22.dp, filled = true, color = Color.White, modifier = Modifier.clickable(onClick = onPause))
            Spacer(Modifier.width(14.dp))
            YMIcon(YMGlyph.Next, size = 20.dp, filled = true, color = Color.White)
        }
    }
}

@Composable
private fun BottomTabs(haze: HazeState, tab: MainTab, onSelect: (MainTab) -> Unit) {
    val side = listOf(
        MainTab.Home to (YMGlyph.Home to "首页"),
        MainTab.Library to (YMGlyph.Library to "曲库"),
        MainTab.Search to (YMGlyph.Search to "搜索"),
        MainTab.Me to (YMGlyph.Me to "我的"),
    )
    Box(Modifier.fillMaxWidth().padding(horizontal = 16.dp).padding(bottom = 14.dp).height(78.dp)) {
        GlassCapsule(
            hazeState = haze,
            modifier = Modifier.align(Alignment.BottomCenter).fillMaxWidth().height(62.dp),
        ) {
            Row(Modifier.fillMaxSize(), verticalAlignment = Alignment.CenterVertically) {
                side.take(2).forEach { (t, pair) ->
                    TabSide(t, pair, tab, onSelect, Modifier.weight(1f))
                }
                Spacer(Modifier.weight(1.15f)) // room for raised orb
                side.drop(2).forEach { (t, pair) ->
                    TabSide(t, pair, tab, onSelect, Modifier.weight(1f))
                }
            }
        }
        CollectionOrb(
            active = tab == MainTab.Collection,
            modifier = Modifier.align(Alignment.TopCenter).offset(y = (-4).dp),
            onClick = { onSelect(MainTab.Collection) },
        )
    }
}

@Composable
private fun TabSide(
    t: MainTab,
    pair: Pair<YMGlyph, String>,
    tab: MainTab,
    onSelect: (MainTab) -> Unit,
    modifier: Modifier,
) {
    val selected = t == tab
    Box(modifier.fillMaxHeight().clickable { onSelect(t) }, contentAlignment = Alignment.Center) {
        Column(horizontalAlignment = Alignment.CenterHorizontally) {
            Box(
                Modifier
                    .clip(RoundedCornerShape(20.dp))
                    .background(if (selected) Lime.copy(alpha = 0.25f) else Color.Transparent)
                    .padding(horizontal = 12.dp, vertical = 4.dp),
                contentAlignment = Alignment.Center,
            ) {
                YMIcon(pair.first, size = 20.dp, color = if (selected) Lime else Color.White.copy(0.75f))
            }
            Text(
                pair.second,
                color = if (selected) Lime else Color.White.copy(0.75f),
                fontSize = 10.sp,
                fontWeight = if (selected) FontWeight.SemiBold else FontWeight.Normal,
            )
        }
    }
}

@Composable
private fun CollectionOrb(active: Boolean, modifier: Modifier = Modifier, onClick: () -> Unit) {
    val tint = Color(0xFF6B4BD8)
    Box(
        modifier
            .size(64.dp)
            .clickable(onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        // soft cover-tint glow
        Box(
            Modifier
                .size(if (active) 64.dp else 58.dp)
                .clip(CircleShape)
                .background(tint.copy(alpha = if (active) 0.55f else 0.32f)),
        )
        Box(
            Modifier
                .size(56.dp)
                .clip(CircleShape)
                .background(
                    Brush.radialGradient(
                        listOf(Color.White.copy(0.35f), tint.copy(0.42f), tint.copy(0.55f)),
                    ),
                ),
            contentAlignment = Alignment.Center,
        ) {
            Column(horizontalAlignment = Alignment.CenterHorizontally) {
                // crayon moon + star stand-in with YMIcons
                Box(contentAlignment = Alignment.TopEnd) {
                    YMIcon(YMGlyph.Collection, size = 22.dp, color = Color.White)
                    Text("✦", color = Color.White, fontSize = 10.sp, modifier = Modifier.offset(x = 10.dp, y = (-6).dp))
                }
                Text("典藏", color = Color.White.copy(0.92f), fontSize = 10.sp, fontWeight = FontWeight.SemiBold)
            }
        }
        if (active) {
            Box(
                Modifier
                    .align(Alignment.BottomCenter)
                    .offset(y = 6.dp)
                    .size(5.dp)
                    .clip(CircleShape)
                    .background(Lime),
            )
        }
    }
}

@Composable private fun SectionTitle(t: String) {
    Text(t, color = Color.White, fontSize = 17.sp, fontWeight = FontWeight.SemiBold, modifier = Modifier.padding(bottom = 10.dp))
}

@Composable
private fun YueqianCard(track: CoreTrack?, onPlay: (CoreTrack) -> Unit) {
    val paper = Color(0xFFF3EDE2)
    val ink = Color(0xFF2A241C)
    val accent = Color(0xFFC45C26)
    val day = java.util.Calendar.getInstance().get(java.util.Calendar.DAY_OF_MONTH)
    val month = java.util.Calendar.getInstance().get(java.util.Calendar.MONTH) + 1
    val weekdays = listOf("周日", "周一", "周二", "周三", "周四", "周五", "周六")
    val wd = weekdays[java.util.Calendar.getInstance().get(java.util.Calendar.DAY_OF_WEEK) - 1]
    Box(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(paper)
            .clickable { track?.let(onPlay) }
            .padding(vertical = 14.dp),
    ) {
        Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
            Column(
                Modifier.width(78.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Text("${month}月", color = accent, fontSize = 12.sp, fontWeight = FontWeight.SemiBold)
                Text("%02d".format(day), color = accent, fontSize = 34.sp, fontWeight = FontWeight.Black)
                Text(wd, color = ink.copy(0.55f), fontSize = 10.sp)
            }
            // perforation
            Box(
                Modifier
                    .width(1.dp)
                    .height(64.dp)
                    .background(ink.copy(0.25f)),
            )
            Column(Modifier.weight(1f).padding(start = 12.dp, end = 8.dp)) {
                Text("今日乐签", color = accent, fontSize = 11.sp, fontWeight = FontWeight.Bold)
                Spacer(Modifier.height(4.dp))
                Text(
                    track?.let { "「${it.title}」" } ?: "打开译梦取今日一签",
                    color = ink,
                    fontSize = 15.sp,
                    fontWeight = FontWeight.SemiBold,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
                Text(
                    track?.let { "${it.title} — ${it.artist}" } ?: "登录平台后有票根",
                    color = ink.copy(0.55f),
                    fontSize = 12.sp,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
            Box(Modifier.padding(end = 14.dp)) {
                CoverImage(
                    track?.coverUrl,
                    Modifier.size(62.dp).clip(RoundedCornerShape(8.dp)),
                    fallback = Color(0xFFE0D6C8),
                )
            }
        }
    }
}

@Composable
private fun DailyMixCard(tracks: List<CoreTrack>, onPlay: (CoreTrack) -> Unit) {
    val day = java.util.Calendar.getInstance().get(java.util.Calendar.DAY_OF_MONTH)
    val tint = Color(0xFF6B4BD8)
    Row(
        Modifier
            .fillMaxWidth()
            .clip(RoundedCornerShape(16.dp))
            .background(Color.White.copy(0.08f))
            .clickable { tracks.firstOrNull()?.let(onPlay) }
            .padding(14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            Modifier
                .size(92.dp)
                .clip(RoundedCornerShape(16.dp))
                .background(tint.copy(0.9f)),
            contentAlignment = Alignment.Center,
        ) {
            // blurred cover wash under day number
            CoverImage(tracks.firstOrNull()?.coverUrl, Modifier.fillMaxSize(), fallback = tint)
            Box(Modifier.fillMaxSize().background(Color.Black.copy(0.35f)))
            Text("$day", color = Color.White, fontSize = 40.sp, fontWeight = FontWeight.Black)
        }
        Spacer(Modifier.width(14.dp))
        Column(Modifier.weight(1f)) {
            Text("${tracks.size} 首 · 按账号", color = Color.White.copy(0.65f), fontSize = 13.sp)
            Spacer(Modifier.height(4.dp))
            tracks.take(2).forEach { s ->
                Text(
                    "${s.title} — ${s.artist}",
                    color = Color.White,
                    fontSize = 14.sp,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        YMIcon(YMGlyph.Play, size = 28.dp, filled = true, color = Lime, modifier = Modifier.clickable {
            tracks.firstOrNull()?.let(onPlay)
        })
    }
}

@Composable
private fun FmCard(track: CoreTrack?, onPlay: (CoreTrack) -> Unit) {
    Row(
        Modifier.fillMaxWidth().clip(RoundedCornerShape(16.dp)).background(Color.White.copy(0.08f))
            .clickable { track?.let(onPlay) }.padding(14.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(Modifier.size(64.dp).clip(RoundedCornerShape(12.dp)).background(Color(0xFF6B4BD8)), contentAlignment = Alignment.Center) {
            YMIcon(YMGlyph.Recognize, size = 28.dp, color = Color.White)
        }
        Spacer(Modifier.width(12.dp))
        Column(Modifier.weight(1f)) {
            Text("私人 FM", color = Color.White, fontSize = 16.sp, fontWeight = FontWeight.SemiBold)
            Text(track?.let { "${it.title} · ${it.artist}" } ?: "点按开始电台", color = Color.White.copy(0.6f), fontSize = 12.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        YMIcon(YMGlyph.Play, size = 22.dp, filled = true, color = Lime)
    }
}

@Composable
private fun CoverCard(title: String, subtitle: String, cover: String?, size: Dp, onClick: () -> Unit) {
    Column(Modifier.width(size).clickable(onClick = onClick)) {
        CoverImage(cover, Modifier.size(size).clip(RoundedCornerShape(12.dp)), fallback = Color(0xFF3D2EFF))
        Spacer(Modifier.height(6.dp))
        Text(title, color = Color.White, fontSize = 13.sp, fontWeight = FontWeight.SemiBold, maxLines = 2, overflow = TextOverflow.Ellipsis)
        if (subtitle.isNotEmpty()) Text(subtitle, color = Color.White.copy(0.5f), fontSize = 11.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
    }
}

@Composable
private fun TrackRow(t: CoreTrack, onClick: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clip(RoundedCornerShape(12.dp)).clickable(onClick = onClick).padding(vertical = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        CoverImage(t.coverUrl, Modifier.size(48.dp).clip(RoundedCornerShape(8.dp)), fallback = Color(0xFFFF7A1A))
        Spacer(Modifier.width(12.dp))
        Column(Modifier.weight(1f)) {
            Text(t.title, color = Color.White, fontSize = 15.sp, fontWeight = FontWeight.SemiBold, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(t.artist, color = Color.White.copy(0.55f), fontSize = 12.sp, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
    }
}

@Composable
private fun CoverImage(url: String?, modifier: Modifier, fallback: Color) {
    if (!url.isNullOrBlank()) {
        AsyncImage(model = url, contentDescription = null, modifier = modifier.background(fallback), contentScale = ContentScale.Crop)
    } else {
        Box(modifier.background(fallback), contentAlignment = Alignment.Center) {
            YMBrandMark(size = 28.dp)
        }
    }
}
