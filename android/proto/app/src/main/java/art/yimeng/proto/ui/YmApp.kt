package art.yimeng.proto.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
// offset via layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Text
import androidx.compose.runtime.*
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.graphics.graphicsLayer
import androidx.compose.ui.graphics.CompositingStrategy
import androidx.compose.ui.graphics.BlendMode
import androidx.compose.ui.draw.drawWithContent
import androidx.compose.ui.graphics.Paint
import androidx.compose.ui.graphics.drawscope.drawIntoCanvas
import androidx.compose.ui.graphics.nativeCanvas
import android.graphics.BlurMaskFilter
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.Path
import androidx.compose.ui.graphics.asAndroidPath
import androidx.compose.ui.graphics.StrokeJoin
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.graphics.drawscope.translate
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Rect
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.animation.animateColorAsState
import androidx.compose.animation.core.tween
import androidx.compose.ui.platform.LocalLayoutDirection
import androidx.compose.ui.unit.IntOffset
import androidx.compose.ui.unit.LayoutDirection
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
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
import androidx.compose.ui.unit.em
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.border

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
    var loaded by remember { mutableStateOf(false) }
    var err by remember { mutableStateOf<String?>(null) }
    LaunchedEffect(Unit) {
        items = CoreApi.favorites()
        err = CoreApi.lastError
        loaded = true
    }
    Column(Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(20.dp)) {
        Text("典藏", color = Color.White, fontSize = 26.sp, fontWeight = FontWeight.Bold)
        Text("收藏 / 点赞", color = Color.White.copy(0.45f), fontSize = 12.sp, modifier = Modifier.padding(bottom = 14.dp))
        when {
            !loaded -> Text("加载中…", color = Color.White.copy(0.45f))
            items.isEmpty() && err == null -> DoodleEmptyState(
                title = "还没有收藏",
                subtitle = "点亮爱心，曲目会出现在这里",
            )
            items.isEmpty() -> Text(err ?: "加载失败", color = Color.White.copy(0.55f))
            else -> items.forEach { TrackRow(it) { onPlay(it) } }
        }
    }
}

@Composable
private fun SearchPage(onPlay: (CoreTrack) -> Unit) {
    var q by remember { mutableStateOf("梁博") }
    var results by remember { mutableStateOf<List<CoreTrack>>(emptyList()) }
    var busy by remember { mutableStateOf(false) }
    var searched by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    fun runSearch(query: String) {
        scope.launch {
            busy = true
            results = CoreApi.search(query)
            busy = false
            searched = true
        }
    }
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
                    // Cycle demo queries; empty uses friendly copy, searches internal ____ probe (never shown).
                    when (q) {
                        "梁博" -> { q = "陈粒"; runSearch(q) }
                        "陈粒" -> { q = "找不到的歌"; runSearch("____empty_probe____") }
                        "找不到的歌" -> { q = "梁博"; runSearch(q) }
                        else -> { q = "梁博"; runSearch(q) }
                    }
                },
            )
            Text("搜", color = Lime, fontWeight = FontWeight.SemiBold, modifier = Modifier.clickable { runSearch(q) })
        }
        Spacer(Modifier.height(12.dp))
        if (busy) Text("搜索中…", color = Color.White.copy(0.5f))
        Column(Modifier.verticalScroll(rememberScrollState())) {
            results.forEach { TrackRow(it) { onPlay(it) } }
            if (!busy && searched && results.isEmpty()) {
                DoodleEmptyState(
                    title = "没有找到相关内容",
                    subtitle = "换个关键词试试，或去曲库逛逛",
                )
            } else if (!busy && !searched) {
                DoodleEmptyState(
                    title = "搜点什么吧",
                    subtitle = "歌曲、歌手、专辑 · 点查询词或「搜」",
                )
            }
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
private fun PlayerPage(
    track: CoreTrack,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
) {
    // Pixel targets from sd-np/target-cover-v3 + iOS 0.3.8(19)
    var progress by remember(track.id) { mutableFloatStateOf(0.35f) }
    var liked by remember(track.id) { mutableStateOf(false) }
    var paused by remember { mutableStateOf(false) }
    var lyricsMode by remember { mutableStateOf(false) }
    val durationSec = 403 // 6:43
    val posSec = (progress * durationSec).toInt()
    fun fmt(s: Int) = "%d:%02d".format(s / 60, s % 60)
    val dream = DreamMock.coverV3.copy(title = track.title, artist = track.artist)
    val haze = remember { HazeState() }

    Box(modifier.background(Base)) {
        // Haze source: field (+ cover stage content sits above but source is field)
        DreamField(
            dream,
            Modifier.fillMaxSize().hazeSource(haze),
            realPlayback = true,
            lastBeatAgoSec = 0.12f,
            intensity = 0.75f,
        )

        // iOS NowPlayingView: lyrics full-bleed behind chrome; cover/info only in cover mode.
        // skel6c: Cover(weight=1) + chrome at bottom — never Spacer(weight) above transport
        if (lyricsMode) {
            PlayerLyricsFullscreen(
                progress = progress,
                modifier = Modifier.fillMaxSize(),
            )
        }

        Column(
            Modifier
                .fillMaxSize()
                .statusBarsPadding()
                .navigationBarsPadding()
                .padding(start = 20.dp, end = 20.dp, top = 4.dp, bottom = 2.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            PlayerTopBar(liked = liked, onBack = onBack, onLike = { liked = !liked })

            if (lyricsMode) {
                // Chrome spacer only — lyrics already fill middle behind (no empty cover slots)
                Spacer(Modifier.weight(1f))
            } else {
                Box(
                    Modifier
                        .weight(1f)
                        .fillMaxWidth()
                        .padding(vertical = 6.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    // M8+M2+M9: DreamLayout.slot + independent E+G DoodleLayer (no cover scale)
                    PlayerCoverStage(
                        dream = dream,
                        coverUrl = track.coverUrl,
                        playing = !paused,
                    )
                }
                Text(
                    track.title,
                    color = Color.White,
                    fontSize = 22.sp,
                    fontWeight = FontWeight.Bold,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    textAlign = TextAlign.Center,
                    modifier = Modifier.fillMaxWidth().padding(horizontal = 8.dp),
                )
                Spacer(Modifier.height(4.dp))
                Text(
                    track.artist,
                    color = Color.White.copy(0.60f),
                    fontSize = 16.sp, // S2: iOS 16
                    fontWeight = FontWeight.Normal,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Spacer(Modifier.height(8.dp))
                Row(
                    horizontalArrangement = Arrangement.spacedBy(8.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    QualityPill("FLAC 24/96", accentDot = true)
                    QualityPill("Hi-Res", mutedFill = true)
                }
                Spacer(Modifier.height(4.dp))
                Text(
                    "舞台 · 夜晚 · ${dream.bpm} BPM",
                    color = Color.White.copy(0.50f),
                    fontSize = 12.sp,
                )
                Spacer(Modifier.height(12.dp))
                PlayerProgress(progress, posSec, durationSec, ::fmt) { progress = it }
            }

            // M3: secondary 5 keys in GlassCapsule / Haze
            Spacer(Modifier.height(if (!lyricsMode) 14.dp else 10.dp))
            PlayerActionRow(
                haze = haze,
                lyricsMode = lyricsMode,
                onToggleLyrics = { lyricsMode = !lyricsMode },
            )

            Spacer(Modifier.height(if (!lyricsMode) 14.dp else 10.dp))

            // M4: transport GlassCapsule + skel6c bottom-stick
            PlayerTransportCapsule(
                haze = haze,
                paused = paused,
                onToggle = { paused = !paused },
            )
        }
    }
}


@Composable
private fun PlayerTopBar(liked: Boolean, onBack: () -> Unit, onLike: () -> Unit) {
    Row(
        Modifier.fillMaxWidth(),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        CircleChromeButton(onClick = onBack) {
            // Soft bowed chevron (dream-doodle), not sharp Lucide V
            Canvas(Modifier.size(22.dp)) {
                val stroke = Stroke(width = 2.1.dp.toPx(), cap = StrokeCap.Round, join = StrokeJoin.Round)
                val c = Offset(size.width / 2f, size.height / 2f)
                val path = Path().apply {
                    moveTo(c.x - 6.5.dp.toPx(), c.y - 2.dp.toPx())
                    quadraticTo(c.x, c.y + 5.5.dp.toPx(), c.x + 6.5.dp.toPx(), c.y - 2.dp.toPx())
                }
                // crayon ghost
                translate(0.7f * density, 0.8f * density) {
                    drawPath(path, Color.White.copy(0.35f), style = Stroke(width = 1.8.dp.toPx(), cap = StrokeCap.Round, join = StrokeJoin.Round))
                }
                drawPath(path, Color.White, style = stroke)
            }
        }
        Text(
            "译梦 · 梦境 CLOUD",
            color = Color.White.copy(0.80f),
            fontSize = 13.sp,
            fontWeight = FontWeight.SemiBold,
            letterSpacing = 0.12.em,
            textAlign = TextAlign.Center,
            modifier = Modifier.weight(1f),
            maxLines = 1,
        )
        CircleChromeButton(onClick = onLike) {
            YMIcon(
                if (liked) YMGlyph.HeartFill else YMGlyph.Heart,
                size = 20.dp,
                weight = 1.70f,
                filled = liked,
                color = if (liked) Color(0xFFFF5FA8) else Color.White,
            )
        }
    }
}

@Composable
private fun CircleChromeButton(onClick: () -> Unit, content: @Composable () -> Unit) {
    Box(
        Modifier
            .size(44.dp)
            .clip(CircleShape)
            .background(Color.White.copy(0.14f))
            .clickable(onClick = onClick),
        contentAlignment = Alignment.Center,
    ) { content() }
}

@Composable
private fun QualityPill(label: String, accentDot: Boolean = false, mutedFill: Boolean = false) {
    Row(
        Modifier
            .clip(RoundedCornerShape(999.dp))
            .background(if (mutedFill) Color.White.copy(0.10f) else Color.Black.copy(0.20f))
            .border(1.dp, Color.White.copy(0.20f), RoundedCornerShape(999.dp))
            .padding(horizontal = 10.dp, vertical = 2.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        if (accentDot) {
            Box(Modifier.size(6.dp).clip(CircleShape).background(Lime))
        }
        Text(
            label,
            color = Color.White,
            fontSize = 10.sp,
            fontWeight = FontWeight.SemiBold,
            letterSpacing = 0.08.em,
        )
    }
}

@Composable
private fun PlayerCoverStage(
    dream: DreamTrack,
    coverUrl: String?,
    playing: Boolean,
) {
    BoxWithConstraints(
        Modifier.fillMaxSize(),
        contentAlignment = Alignment.Center,
    ) {
        val wPx = constraints.maxWidth.toFloat().coerceAtLeast(1f)
        val hPx = constraints.maxHeight.toFloat().coerceAtLeast(1f)
        // Fit DreamLayout.frame slot into available stage; keep aspect, center.
        val design = coverSlot(dream.layout, wPx)
        val slotW = design.rect.width
        val slotH = design.rect.height
        val fit = minOf(1f, (hPx * 0.92f) / slotH)
        // Remap slot into centered stage coords
        val cx = wPx / 2f
        val cy = hPx / 2f
        val mapped = CoverSlot(
            rect = Rect(
                left = cx - slotW * fit / 2f,
                top = cy - slotH * fit / 2f,
                right = cx + slotW * fit / 2f,
                bottom = cy + slotH * fit / 2f,
            ),
            radius = design.radius * fit,
        )
        // M9: doodle independent, allowsHitTesting=false equivalent (Canvas no input)
        DoodleLayer(
            track = dream,
            modifier = Modifier.fillMaxSize(),
            intensity = 0.75f,
            playing = playing,
            beat = true,
            slotOverride = mapped,
        )
        val density = LocalDensity.current
        CoverImage(
            coverUrl,
            Modifier
                .offset {
                    IntOffset(mapped.rect.left.toInt(), mapped.rect.top.toInt())
                }
                .size(
                    with(density) { mapped.rect.width.toDp() },
                    with(density) { mapped.rect.height.toDp() },
                )
                .drawBehind {
                    drawIntoCanvas { canvas ->
                        val paint = Paint().asFrameworkPaint().apply {
                            isAntiAlias = true
                            color = android.graphics.Color.argb(89, 0, 0, 0)
                            maskFilter = BlurMaskFilter(35.dp.toPx(), BlurMaskFilter.Blur.NORMAL)
                        }
                        val r = mapped.radius
                        canvas.nativeCanvas.drawRoundRect(
                            0f,
                            20.dp.toPx(),
                            size.width,
                            size.height + 20.dp.toPx(),
                            r,
                            r,
                            paint,
                        )
                    }
                }
                .clip(RoundedCornerShape(with(density) { mapped.radius.toDp() })),
            fallback = Color(0xFFFF7A1A),
        )
    }
}


@Composable
private fun PlayerProgress(
    progress: Float,
    posSec: Int,
    durationSec: Int,
    fmt: (Int) -> String,
    onScrub: (Float) -> Unit,
) {
    val rem = maxOf(0, durationSec - posSec)
    CompositionLocalProvider(LocalLayoutDirection provides LayoutDirection.Ltr) {
        Column(Modifier.fillMaxWidth()) {
            // S1: thin 3dp capsule (iOS), invisible taller hit target
            Box(
                Modifier
                    .fillMaxWidth()
                    .height(20.dp)
                    .pointerInput(Unit) {
                        detectTapGestures { offset ->
                            onScrub((offset.x / size.width).coerceIn(0f, 1f))
                        }
                    },
                contentAlignment = Alignment.CenterStart,
            ) {
                Box(
                    Modifier
                        .fillMaxWidth()
                        .height(3.dp)
                        .clip(RoundedCornerShape(999.dp))
                        .background(Color.White.copy(0.30f)),
                ) {
                    Box(
                        Modifier
                            .fillMaxWidth(progress)
                            .fillMaxHeight()
                            .clip(RoundedCornerShape(999.dp))
                            .background(Color.White),
                    )
                }
            }
            Spacer(Modifier.height(8.dp))
            Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween) {
                Text(
                    fmt(posSec),
                    color = Color.White.copy(0.85f),
                    fontSize = 12.sp,
                    fontFamily = FontFamily.Monospace,
                )
                // M1: remaining with leading "-"
                Text(
                    "-" + fmt(rem),
                    color = Color.White.copy(0.85f),
                    fontSize = 12.sp,
                    fontFamily = FontFamily.Monospace,
                )
            }
        }
    }
}


@Composable
private fun PlayerActionRow(
    haze: HazeState,
    lyricsMode: Boolean,
    onToggleLyrics: () -> Unit,
) {
    GlassCapsule(
        hazeState = haze,
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = 6.dp)
            .height(44.dp),
    ) {
        Row(
            Modifier
                .fillMaxSize()
                .padding(horizontal = 12.dp),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(contentAlignment = Alignment.Center) {
                YMIcon(
                    YMGlyph.Lyrics,
                    size = 20.dp,
                    weight = 1.70f,
                    color = Color.White.copy(0.92f),
                    active = lyricsMode,
                    modifier = Modifier.clickable(onClick = onToggleLyrics),
                )
                if (lyricsMode) {
                    Box(
                        Modifier
                            .padding(top = 26.dp)
                            .size(4.dp)
                            .clip(CircleShape)
                            .background(Lime),
                    )
                }
            }
            YMIcon(YMGlyph.Eq, size = 20.dp, weight = 1.70f, color = Color.White.copy(0.92f))
            YMIcon(YMGlyph.AddList, size = 20.dp, weight = 1.70f, color = Color.White.copy(0.92f))
            YMIcon(YMGlyph.Queue, size = 20.dp, weight = 1.70f, color = Color.White.copy(0.92f))
            YMIcon(YMGlyph.More, size = 20.dp, weight = 1.70f, color = Color.White.copy(0.92f))
        }
    }
}

@Composable
private fun PlayerTransportCapsule(
    haze: HazeState,
    paused: Boolean,
    onToggle: () -> Unit,
) {
    GlassCapsule(
        hazeState = haze,
        modifier = Modifier
            .fillMaxWidth()
            .height(64.dp),
    ) {
        Row(
            Modifier.fillMaxSize().padding(horizontal = 22.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.SpaceBetween,
        ) {
            YMIcon(YMGlyph.Repeat, size = 18.dp, weight = 1.70f, color = Color.White.copy(0.90f))
            // M5 / S3: prev/next ~26dp, filled triangle directions from YMIcons
            YMIcon(YMGlyph.Previous, size = 26.dp, filled = true, color = Color.White)
            Box(
                Modifier
                    .size(50.dp)
                    .clip(CircleShape)
                    .background(Color.White)
                    .clickable(onClick = onToggle),
                contentAlignment = Alignment.Center,
            ) {
                YMIcon(
                    if (paused) YMGlyph.Play else YMGlyph.Pause,
                    size = 22.dp,
                    filled = true,
                    color = Color.Black,
                )
            }
            YMIcon(YMGlyph.Next, size = 26.dp, filled = true, color = Color.White)
            Text(
                "1×",
                color = Color.White.copy(0.70f),
                fontSize = 13.sp,
                fontWeight = FontWeight.Medium,
                fontFamily = FontFamily.Monospace,
                modifier = Modifier.width(28.dp),
                textAlign = TextAlign.Center,
            )
        }
    }
}



@Composable
private fun PlayerLyricsFullscreen(
    progress: Float,
    modifier: Modifier = Modifier,
) {
    // Port of iOS LyricsView / LiveLyrics (yimeng-clean @ 0.3.8/19):
    // uniform dark scrim α0.52 + left-aligned lines + current @ ~36% Y + soft edge MASK.
    // Do NOT paint opaque top/bottom dark rectangles (those looked like empty cover slots).
    val lines = listOf(
        "那些欲言又止",
        "一笑带过",
        "有些话不知道",
        "要怎么说",
        "梦还在心中不曾熄灭",
        "这一路走来不容易",
        "你和我曾并肩前进",
        "风雨里也不曾回头",
        "译梦还在心中",
        "这一刻只为你停留",
    )
    val current = ((progress * lines.size).toInt()).coerceIn(0, lines.lastIndex)
    val scrim = Color(red = 12f / 255f, green = 10f / 255f, blue = 14f / 255f, alpha = 0.45f)
    val density = LocalDensity.current
    val lineStepPx = with(density) { 62.dp.toPx() }

    BoxWithConstraints(modifier) {
        val stageH = constraints.maxHeight.toFloat().coerceAtLeast(1f)
        // Uniform scrim (iOS Color(12,10,14).opacity(0.52)) — full bleed, no banded slots
        Box(Modifier.fillMaxSize().background(scrim))

        Column(
            Modifier
                .fillMaxSize()
                .padding(horizontal = 28.dp)
                .offset {
                    val y = stageH * 0.28f - current * lineStepPx
                    IntOffset(0, y.toInt())
                }
                .graphicsLayer { compositingStrategy = CompositingStrategy.Offscreen }
                .drawWithContent {
                    drawContent()
                    // iOS .mask(LinearGradient clear@0.08…black@0.2…black@0.8…clear@0.9)
                    drawRect(
                        brush = Brush.verticalGradient(
                            // Keep iOS-style edge fade, but start later so middle stays lyrics-first
                            0.00f to Color.Transparent,
                            0.04f to Color.Transparent,
                            0.12f to Color.Black,
                            0.88f to Color.Black,
                            0.96f to Color.Transparent,
                            1.00f to Color.Transparent,
                        ),
                        blendMode = BlendMode.DstIn,
                    )
                },
            horizontalAlignment = Alignment.Start,
            verticalArrangement = Arrangement.Top,
        ) {
            lines.forEachIndexed { i, line ->
                val past = i < current
                val active = i == current
                val dist = kotlin.math.abs(i - current)
                // M6 / LiveLyrics phone: past || active → Theme.lime; future → white ~0.4
                val target = when {
                    active -> Lime
                    past -> Lime.copy(alpha = 0.90f)
                    else -> Color.White.copy(alpha = if (dist == 1) 0.55f else 0.40f)
                }
                val color by animateColorAsState(
                    targetValue = target,
                    animationSpec = tween(durationMillis = 550),
                    label = "lyricAccent",
                )
                Text(
                    line,
                    color = color,
                    fontSize = if (active) 24.sp else 21.sp,
                    fontWeight = FontWeight.Bold,
                    textAlign = TextAlign.Start,
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(vertical = 11.dp),
                )
            }
        }
    }
}


@Composable
private fun DoodleEmptyState(title: String, subtitle: String) {
    Column(
        Modifier.fillMaxWidth().padding(top = 36.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Canvas(Modifier.size(220.dp, 160.dp)) {
            val ink = Color.White.copy(0.55f)
            val stroke = Stroke(width = 2.2f, cap = StrokeCap.Round)
            val cloud = DoodlePaths.cloud(Offset(28f, 70f), 150f)
            val moon = DoodlePaths.moon(Offset(size.width - 48f, 36f), 18f)
            val spark = DoodlePaths.sparkle(Offset(size.width * 0.55f, 28f), 8f)
            val frame = DoodlePaths.frame(
                Rect(40f, 30f, size.width - 40f, size.height - 20f),
                seed = 7u,
                jitter = 2.2f,
            )
            drawPath(frame, ink.copy(0.35f), style = stroke)
            drawPath(cloud, ink, style = stroke)
            drawPath(moon, ink, style = stroke)
            drawPath(spark, ink, style = stroke)
        }
        Spacer(Modifier.height(12.dp))
        Text(title, color = Color.White.copy(0.90f), fontSize = 16.sp, fontWeight = FontWeight.SemiBold)
        Spacer(Modifier.height(6.dp))
        Text(subtitle, color = Color.White.copy(0.4f), fontSize = 13.sp)
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
    val cal = java.util.Calendar.getInstance(java.util.TimeZone.getTimeZone("Asia/Shanghai"))
    val day = cal.get(java.util.Calendar.DAY_OF_MONTH)
    val month = cal.get(java.util.Calendar.MONTH) + 1
    val weekdays = listOf("周日", "周一", "周二", "周三", "周四", "周五", "周六")
    val wd = weekdays[cal.get(java.util.Calendar.DAY_OF_WEEK) - 1]
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
    val day = java.util.Calendar.getInstance(java.util.TimeZone.getTimeZone("Asia/Shanghai"))
        .get(java.util.Calendar.DAY_OF_MONTH)
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
