# 译梦 页面与功能清单（跨平台交接）

> 从 `ios/Yimeng/*.swift` 推导（分支 `feat/ios-navidrome`，2026-10-22）。【iOS 独有】= 依赖 iOS 系统能力，其它平台需要替代方案或不做。iPad 和设置页正在重做，相关条目以合并后的代码为准。

## 1. 外壳与导航

- 5 个 Tab（`App.swift` `Tab`）：**首页 · 曲库 · 典藏（中间凸起的玻璃球）· 搜索 · 我的**。重复点当前 Tab = 回到根。
- 每个 Tab 一个 `NavigationStack`，路由（`Route`）：album / ndAlbum / ndArtist / ndPlaylist / artist / genre / genres / charts / yueqian / trackList。
- 封面 → 专辑页的**缩放转场**（iOS 18 `navigationTransition(.zoom)`，iOS 17 普通 push）；迷你播放器封面 → 正在播放封面 matchedGeometry。
- 全局 sheet（`GlobalSheets`）：播放队列、音效、加到歌单、提示条。
- 启动：`RootGate` → 首次引导 `OnboardingFlow`（MoonwaveIcon 描线、连接音源）→ 没有音源时 `NoSourceView`（单屏、一个按钮）→ 正常外壳。
- 服务端推送：Core 优先 WebSocket `/ws`，回退 SSE `GET /api/v1/events`，带退避重连；收到事件（如 `favorites.changed`）只做轻量刷新（`CoreEvents.swift`）。

## 2. 按 Tab 的页面

### 首页（`HomeView` / `HomeLive` / `Banner` / `CoreUI`）
- 问候语（早上好 / 下午好 / 晚上好 / 夜深了）
- 顶部轮播 Banner「为你推荐」：叠层滑页 + Ken Burns，背景色随滑动交叉渐变，圆点 5pt、当前 14pt
- 区块（无数据自动隐藏）：最近播放、最近添加、常听、热门歌手、随机专辑、歌单、按流派、排行入口、今日乐签票根
- **平台透传区块**（Core 且登录了平台）：「X · 每日推荐」、「X · 私人 FM」（FMCard + 喜欢/不喜欢反馈）、「X · 推荐歌单」、「X 的歌单」（我创建的 / 我收藏的）、可一键导入（clone）到 Core 歌单
- 首次同步：`FirstRunDoodle`「正在把曲库搬进梦里…」
- 新账号欢迎卡（`CoreV28` `NewAccountCard`）

### 曲库（`LibraryView`）
- 分段：歌曲 · 专辑 · 歌手 · 歌单 · 收藏
- 专辑排序：最新 / A–Z / 最近播放 / 常听 / 随机（`albumSort`）；分页 + 「从第 N 张起 · 载入前面」
- 歌手：拼音感知 A–Z 电梯索引（`ElevatorIndex.swift`，CFStringTransform 转拼音，忽略 The 等冠词，拖动时浮动气泡）
- 歌单：新建歌单（名称、公开）
- 来源筛选胶囊 `SourceFilterChips`（Core：本地 / 网易云 / QQ / 酷狗）
- 子页面：专辑详情（`AlbumView`/`NDAlbumView`，CoverBloom 背景）、歌手详情（`ArtistDetailView`：头图、折叠简介、热门歌曲、专辑网格、相似歌手）、歌单详情（`PlaylistDetailView`：2×2 拼图封面、改名、排序、删除歌曲，乐观更新）、流派列表 / 流派页（`GenreView`）、排行（`ChartsView`，DoodleTabs 切平台）、通用歌曲列表 `TrackListPage`

### 典藏（`CollectionView`）
- 收藏的专辑（与歌曲数统计），3D 卡片堆叠拖动浏览；空时 `EmptyState.collection` + 「去曲库逛逛」

### 搜索（`LibrarySearchView`，在 `AlbumView.swift`）
- 输入框「搜索曲库…」，热门词胶囊，Core 下有联想 `search/suggest` 和「在线」开关（`online=1` 同时搜平台）
- 结果：艺人、专辑、歌曲
- **识曲** 按钮（仅 Core 为音源时显示）

### 我的（`MeView` → `SettingsView`，正在重做）
- 个人资料卡 → **个人资料页 `ProfileView`**（`CoreV31.swift`）：头像（JPEG/PNG/WebP，上传/移除）、昵称（≤32 字）、用户名、身份（管理员/成员）、已登录的平台账号；无头像时 MonogramAvatar
- 音乐来源 / 音源设置（`MusicSources.swift` `SourcesView`）：译梦服务端、飞牛音乐、Navidrome、Emby、本地
- 偏好：串流音质（原始 · 无损 / 转码）、音效设置（过渡 · 回放增益 · 均衡器）、歌词字体、歌词翻译、涂鸦强度、随节拍轻震、触感反馈、外观（跟随系统 / 浅色 / 深色）、灵动岛 / 实时活动
- 离线缓存（用量环、清除）
- Core 专属：听歌报告（本月 · 今年，DoodleBars）、Last.fm（填 key → 授权 → 开关 → 断开）、安全提醒（新设备配对 / 新 IP 登录 / 多次失败 / 锁定，全部已读）、跨平台换源（开关 + 拖动排序平台优先级）、设备配对、退出登录
- 系统：重看启动引导、开源许可（含字体 OFL）、关于

### 正在播放（`NowPlayingView` + `NowPlayingMore`）
- 全屏梦境背景 + 涂鸦层 + 封面 + 标题/歌手/音质徽章 `QualityBadge`/换源来源/BeatTagLine/进度
- 操作行：歌词 · 音效 · 加到歌单 · 播放列表 · ⋯
- 控制条：播放模式（列表循环 / 单曲 / 随机 / 顺序）、上一首、播放/暂停、下一首、倍速显示
- 歌词模式：Core 逐字歌词 `LiveLyricsView`（`/lyrics/best`），否则普通 `LyricsView`；字体选择器
- 「梦境」sheet：涂鸦 / 星尘 / 极光 / 纯净 + 强度 + 随节拍
- 「⋯」更多 sheet：专辑、歌手、加到歌单、分享、倍速、**定时关闭**、音质（直通 / 转码 MP3 / 音效处理）、设备投放（AirPlay）、接力（Core 会话）、调节音量、梦境氛围、换个平台播放（alternatives）
- 信号路径 sheet `SignalPathSheet`（来源 → 解码 → 处理 → 输出 Hz/声道）

## 3. 按音源分

| 功能 | Core（译梦服务端） | Navidrome / Subsonic | Emby | 飞牛音乐 |
|---|---|---|---|---|
| 客户端 | `CoreClient` + `CoreBackend`（把 Core 模型映射为 ND 模型复用 UI） | `NavidromeClient`（`/rest/*.view`） | `EmbyClient` | `FnosClient`（`/music/api/v1`，music-token cookie 鉴权） |
| 登录 | 账号密码 / 配对码 | 账号密码 | AuthenticateByName | 账号 |
| 曲库/专辑/歌手/歌单 | ✓ | ✓ | ✓ | ✓ |
| 收藏 | ✓ | star/unstar | FavoriteItems | 依实现 |
| 歌词 | `/lyrics/best`（逐字） | getLyrics | `/Audio/{id}/Lyrics` | — |
| 首页推荐区块、平台透传、每日、FM、排行 | ✓ | — | — | — |
| 梦境颜色/BPM/标签 `/dream` | ✓ | 仅歌曲自带 colors | — | — |
| 识曲、听歌报告、Last.fm、安全提醒、跨平台换源、接力、睡眠同步、进度同步、头像昵称 | ✓（仅 Core） | — | — | — |
| 播放上报 | `/scrobble` | scrobble | Sessions/Playing* | — |

**产品方向**：Core 是主音源；其它三种是兼容。新平台第一版建议：Core 全功能 + Navidrome；Emby、飞牛排后。

## 4. Core API（客户端实际调用，`/api/v1`）

| 分组 | 端点 | 用途 / 出处 |
|---|---|---|
| 认证 | `GET /server/info`（无鉴权）、`POST /auth/login`、`POST /pair`、`POST /auth/refresh`、`POST /auth/logout` | `CoreClient` |
| 账户 | `GET/PATCH /me`、`PUT/DELETE /me/avatar`、`GET /devices/me` | 资料页 |
| 曲库 | `GET /albums`、`/albums/{id}`、`/artists`、`/artists/{id}/albums`、`/artists/{id}/tracks`、`/tracks` | `CoreBackend` |
| 收藏 | `GET /favorites?type=`、`PUT/DELETE /favorites/{type}/{id}` | |
| 歌单 | `GET/POST /playlists`、`PATCH/DELETE /playlists/{id}`、`GET/POST/PUT/DELETE /playlists/{id}/tracks` | PUT = 全量重排 |
| 首页 | `GET /home` | |
| 搜索 | `GET /search?q&online`、`GET /search/suggest` | |
| 歌词 | `GET /lyrics/best` | |
| 梦境 | `GET /tracks/{id}/dream` | colors / bpm / tags |
| 换源 | `GET /tracks/{id}/alternatives`、`GET/PUT /me/source-switch` | |
| 播放偏好 | `GET/PUT /me/playback` | 无缝、过渡等跨设备同步 |
| 进度 | `GET /progress?ids=`、`PUT /progress/{id}` | 断点续播、Handoff |
| 上报 | `POST /scrobble` | |
| 会话/接力 | `GET /sessions`、`POST /sessions/handoff` | |
| 睡眠 | `POST /players/{id}/sleep` | Core 下发 `player.sleep.fade` / `player.sleep` |
| 平台透传 | `GET /online/{origin}/daily`、`/fm`、`POST /fm/feedback`、`/recommend/playlists`、`/user/playlists?type=`、`POST /online/{origin}/playlists/{id}/clone` | origin ∈ netease / qq / kugou |
| 排行 | `GET /charts?origin=` | |
| 报告 | `GET /me/report` | |
| Last.fm | `GET/PUT/DELETE /me/lastfm`、`PUT /me/lastfm/keys`、`POST /me/lastfm/auth`、`/auth/complete` | |
| 安全 | `GET /security/alerts`、`POST /security/alerts/read` | |
| 识曲 | `POST /recognize`（multipart，part 名 `audio`） | |
| 媒体 | `/api/v1/covers/{id}?size&format=webp`、流 `/api/stream/{id}`（或服务端签名 URL） | |
| 推送 | `/ws`、`GET /events`（SSE） | |

⚠️ **契约缺口**：`core/api/openapi.yaml` 目前描述的是 `/api/...`（无 v1）的一套接口（74 条路径），**上面大部分 `/api/v1` 端点不在 openapi 里**（如 `/home`、`/tracks/{id}/dream`、`/online/*`、`/recognize`、`/me/*`）。开发 Android/Windows 前必须先把 openapi 补齐到客户端实际用的这套，见 platform-plan.md。

## 5. 交互细节

### 手势（`GesturesV32.swift`）
- 全局边缘右滑返回（自定义导航栏也保留）。
- 迷你播放器：上滑 > 40pt → 展开；左右 > 60pt → 下一首/上一首（带触感）。
- 正在播放：任意位置下拉跟手，> 140pt 或预测 > 380pt 收起。
- 封面：左右 > 80pt 或预测 > 260pt 切歌，带甩出动画。
- 进度条拖动：每 10 秒一次触感刻度（`ScrubBar`）。
- 硬件键盘快捷键（`PlayerShortcuts`，iPhone + iPad）。

### 长按菜单（带预览，`GesturesV32.swift`、`Common.swift`）
- 专辑：播放 · 下一首播放 · 加入播放队列 · 前往歌手 · 收藏/取消收藏
- 歌手：播放热门歌曲 · 下一首播放 · 加入播放队列 · 前往歌手 · 关注/取消关注
- 歌单：播放 · 下一首播放 · 加入播放队列 · 打开歌单
- 歌曲：下一首播放 · 加入播放队列 · 添加到歌单… · 前往专辑 · 前往歌手

### 播放引擎（`PlayerStore.swift`、`Audio.swift`）
- **无缝**（默认开，`ym.gapless`）：`AVQueuePlayer` 提前排入下一首。仅在过渡 = 0、非单曲循环、非「本首后停止」时生效。
- **过渡/淡入淡出**（`ym.crossfade`，0–12 秒，0 = 关）：用音量渐变实现，淡入最长 4 秒。
- **回放增益**（关 / 单曲 / 专辑）：只调 `AVPlayer.volume` ≤ 1，不做 DSP，不破坏直通。
- 倍速（保持音高）、播放模式、播放历史、重启后恢复进度。
- 直通：音效关 = 不处理（FLAC / 杜比原样）。

### EQ 预设（`Effects.swift`）
10 段图示均衡（31/62/125/250/500/1k/2k/4k/8k/16k，±12 dB）+ 低频搁架 80 Hz（0–12 dB）+ 人声 2.8 kHz（0–8 dB）+ 前级（−12…+6，可自动留余量）+ 峰值限制器（−0.3 dBFS，约 80 ms 释放）。
预设：平直、超重低音、清澈人声、现场、摇滚、古典、流行、电子、爵士、耳机增强、夜间（各段数值见 `FXSettings.presets`，照抄）。手动改任意一段 → 「自定义」。

### 睡眠定时（`PlayerStore.setSleep`、`NowPlayingMore`）
N 分钟后暂停 / 播完当前歌曲；定时开启后可切换「渐弱」；Core 下同时在服务端 arm（`POST /players/{id}/sleep`），其它会话可见。Shortcuts 有「睡眠定时」。

### 离线缓存（`Offline.swift`）
- 缓存的是原始流字节（与播放同一 URL），所以离线播放仍是比特精确直通。
- 位置 Application Support（不被系统清理），默认上限 2048 MB（`ym.offline.limitMB`），默认仅 Wi-Fi（`ym.offline.wifiOnly`）。
- 播放时后台另起下载缓存；智能预缓存：今日每日推荐 + 最常播放。
- 淘汰：从未重播的先删，旧的先删，今日推荐算多一次播放。

### 今日乐签（`Yueqian.swift`）
每天一首歌做成票根卡片：已登录平台的每日推荐第一首（透传）；否则从自己曲库按日期种子确定性选。挑一句 6–22 字、在歌曲中段、不是署名行的歌词。本月打开日志。**界面上从不写「推荐」二字。** 首页票根入口 + 独立页面 + 小组件。

### 平台透传推荐
Core 登录网易云/QQ/酷狗后，每日推荐、私人 FM、推荐歌单、用户歌单原样展示并标注来源平台（「网易云 · 每日推荐」），不做自己的推荐算法；可 clone 到 Core。

### 识曲（`Recognize.swift`）
只有 Core 为音源时显示入口；只在用户点按后打开（不自动）；录 10 秒 → `POST /api/v1/recognize` multipart → 结果行；Core 匹配到曲库内的歌就可直接播放。

## 6. 系统集成

| 功能 | iOS 实现 | 其它平台 |
|---|---|---|
| 锁屏/控制中心 | `MPNowPlayingInfoCenter` + `MPRemoteCommandCenter`（`NowPlayingInfo.swift`） | Android MediaSession；Windows SMTC；Mac 同 iOS |
| 灵动岛 / 实时活动 | ActivityKit `YimengLiveActivity`【iOS 独有】 | Android 媒体通知；无 |
| 小组件 | 正在播放（small/medium/锁屏 3 种）、今日乐签（small/medium/锁屏 2 种）、私人 FM（small/锁屏圆形）；App Group 共享数据；点按 `yimeng://player`、`yimeng://yueqian`、`yimeng://fm` | Android Glance 小组件；Mac 同一套 WidgetKit；Windows 11 小组件板（可选，优先级低） |
| 快捷指令 | App Intents：播放每日推荐、播放私人 FM、播放歌单、睡眠定时（带中文 Siri 短语）【iOS/Mac】 | Android App Shortcuts（静态快捷方式）；Windows 跳转列表 |
| Handoff | `NSUserActivity` 类型 `art.yimeng.app.playing`，带歌曲、位置、音源 id；音源不同时提示；Core 下同时 PUT 进度【Apple 独有】 | 跨平台统一用 Core「接力」（`/sessions/handoff`）+ 进度同步 |
| AirPlay / 路由 | `AVRoutePickerView`【Apple 独有】 | Android 输出切换器 / Cast；Windows 音频端点选择 |
| 触感 | `Haptics`（全局开关）+ 随节拍轻震 | Android `HapticFeedback`/Vibrator；Windows 无（忽略） |

## 7. iPad 布局（`IPad.swift`，正在重做）
- 水平尺寸 regular 时：`NavigationSplitView` 三栏 = 侧边栏（5 个 Tab，选中 lime 25% 圆角 10 背景，宽 180–260）+ 内容列表 + **常驻播放列**（封面、进度、控制，下方歌词或待播）。
- iPad 上不弹全屏正在播放；播放列里打开「全屏」：横屏 = 黑胶转盘 `VinylNowPlaying`（33⅓ rpm，暂停后连续、唱臂 `Tonearm`），竖屏 = 普通正在播放。
- **Mac 和 Windows 宽屏布局建议直接以 iPad 布局为蓝本**（侧边栏 + 列表 + 播放列），Android 平板同理。
