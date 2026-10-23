# 译梦 设计规范（跨平台交接）

> 来源：`ios/Yimeng/*.swift`（分支 `feat/ios-navidrome`，2026-10-22 读取）。所有数值均摘自代码，文件名附在括号里。代码里没有的值会写明「代码中未定义」，不要自行补。
>
> **注意：iPad 和设置页正在重新设计**（另一位同事正在改 `ios/`，涉及 `IPad.swift`、`MeView.swift`、新增的 `YMKit.swift`）。下面的 token 可能还会微调，以合并后的代码为准。

## 1. 颜色 Token（`Theme.swift`）

| Token | 深色 | 浅色 | 说明 |
|---|---|---|---|
| `accent` | `#C8F542`（lime） | `#4F6B00`（limeLight） | 选中态、开关、进度、小圆点 |
| `base` 页面底色 | `#0A0A0C`（0.039,0.039,0.047） | `#FFFFFF` | |
| `fg` 主文字 | `#FFFFFF` | `#121212`（white 0.07） | |
| `secondary` 次文字 | 白 60% | 黑 50% | |
| `fill` 填充 | 白 8% | 黑 6% | 卡片、次按钮、胶囊 |
| `divider` | 白 6% | 黑 8% | 0.5pt 细线 |

**按钮**
- Primary：深色下白底黑字，浅色下 `#121212` 底白字；16pt semibold，最小高 48，胶囊形，按下透明度 0.8。**主按钮不用 lime，也不用玻璃。**
- Secondary：`fill` 底，`fg` 字，按下 0.7。
- Destructive / 危险：文字 `#FF6B6B`，`fill` 底（`YMKit.swift`）。
- CircleIconButton：默认 44pt，图标 17pt semibold；在封面上用黑 28% 底。
- PlayCircle：白底圆，黑色 play.fill，图标为直径的 0.4。

**固定装饰色（代码里出现的常量）**
- 默认氛围色组（没有封面色时）：`#7A1CFF`、`#FF5FA8`、`#33D17A`、`#FFD36B`（`App.swift` `AppState.blobs`），idle 底色 `#1A1028`。
- 收藏心形 / 已收藏：`#FF5FA8`。
- 歌手头像兜底色：`#7A1CFF #FF5FA8 #33D17A #FF9A5A #3B82F6 #14B8A6`（`EmptyStates.swift`），按 id 的 unicode 值求和取模。
- 字母头像（MonogramAvatar）色：`#7A1CFF #E85CFF #3B82F6 #14B8A6 #FF7A1A #FF2D7A`（`CoreV30.swift`），hash 为 `h*31+c`。
- 排行徽章：金 `#F2B705`、银 `#9AA4B2`、铜 `#C7773A`（`CoreV25.swift`）。
- 极光带：`#7AF0C8`、`#9B7BFF`（`Doodle.swift`）。
- 无音源页背景：`#120A24`（`App.swift` `NoSourceView`）。
- 歌词遮罩：RGB(12,10,14) 52%（`NowPlayingView.swift`）。

旧文档 `docs/design-system.md` 写的 `#1A1A1F` 卡面、紫 `#7B5CFF` 等值**在 iOS 代码里没有用到**，以本文件为准。

## 2. 字体

**界面字体**：全部使用系统字体（SF Pro / PingFang），代码里是 `.system(size:weight:)`。没有自带 UI 字体。按出现次数统计的常用规格：

| 用途 | 字号 / 字重 | 出处示例 |
|---|---|---|
| 页面大标题 | 28 bold（13 处）、26 bold | 各列表页 |
| 正在播放标题 | 22 bold | `NowPlayingView.info` |
| 区块标题 | 20 bold、22 bold | |
| 页面居中小标题 / 卡片标题 | 17 semibold | `YMPage`、`EmptyState` |
| 行标题 | 16 regular（设置行）/ 15–16 semibold（歌曲） | `YMRow`、`TrackRow` |
| 按钮 | 16 semibold | `PrimaryButtonStyle` |
| 小胶囊 / Chips | 14 semibold / medium | `YMChips` |
| 正文辅助 | 13 regular（最多，80 处）、12 regular | |
| Tab 文字 | 10 semibold，图标 19 | `App.swift tabItem` |
| 数字 | `.monospacedDigit()`（时间、滑块值、倍速） | |
| 字母头像 | size×0.44 heavy rounded，旋转 −6° | `MonogramAvatar` |

**歌词字体**（`LiveLyrics.swift` `LyricFontStore`，用户可选，存 `ym.lyricFont`）：

| id | 名称 | PostScript 名 | 获取方式 |
|---|---|---|---|
| system | 系统字体 | — | 默认；`.system(size, weight: .bold)` |
| smiley | 得意黑 Smiley Sans | `SmileySans-Oblique` | **内置** `Fonts/SmileySans-Oblique.otf` |
| kuaile | 站酷快乐体 ZCOOL KuaiLe | `ZCOOLKuaiLe-Regular` | **内置** `Fonts/ZCOOLKuaiLe-Regular.ttf` |
| wenkai | 霞鹜文楷 LXGW WenKai | `LXGWWenKai-Regular` | 按需下载（约 25 MB，GitHub Release） |
| shserif | 思源宋体 Source Han Serif | `SourceHanSerifCN-Light` | 按需下载（约 11 MB） |

全部 SIL OFL 1.1，许可文本在 `Fonts/OFL-*.txt`，「开源许可」页要展示。下载后用 `CTFontManagerRegisterFontsForURL(.process)` 注册。其它平台：Android 放 `res/font` + 下载到 `filesDir` 后 `Typeface.createFromFile`；Windows 用 `FontFamily("ms-appdata:///local/xxx.ttf#Name")`。

歌词排版（`NowPlayingView.LyricsView`）：当前行 24 bold，其它行 21 bold；行距 22；翻译 13 regular、60% 透明；距离当前行 1 行时透明度 0.55，≥2 行时 0.4；模糊：距离 2 为 1pt，≥3 为 2.5pt；整体 saturation 0.55；上下渐隐遮罩 stop 0.08/0.2/0.8/0.9。

## 3. 间距

代码里**没有完整的间距刻度表**。只有：
- `Theme.margin = 20`（页面左右边距，所有页面统一）。
- 常见的 stack spacing：2、8、12、14、16、18、22、26（逐处写死）。
- 设置行最小高 50（`YMRow`、`YMToggleRow`），卡片内边距水平 16、垂直 4，区块之间 top 20。
- 设置页内容最大宽 680（`YMPage`）。
- 正在播放用 402pt 宽的设计画布，运行时按 `width/402` 缩放（`Doodle.swift` `DreamLayout.slot`）。

建议各平台先把 4/8/12/16/20/24 作为刻度用，碰到代码里的特殊值照抄。

## 4. 圆角

按出现次数：14（11 次）、20（10）、12（7）、18（6）、16（5）、10（5）、8、9、4、6、7、22、26、28。固定规则：

| 元素 | 圆角 |
|---|---|
| 设置卡（YMCard） | 20，`.continuous`（连续曲率 / squircle） |
| Sheet 弹层 | 28（`presentationCornerRadius(28)`） |
| 专辑方块 / 骨架 | 12 |
| 缩放转场源 | 12 |
| 氛围预览瓦片 | 14 |
| 正在播放封面 | frame 布局 10、drip 布局 24、orbit 布局 999（正圆） |
| Tab 栏、迷你播放器、按钮、Chips | 胶囊 |
| 迷你播放器封面、歌手头像 | 圆 |

**所有圆角矩形都用 continuous 曲率**。Android 用 `RoundedCornerShape` 会略显"硬"，可用 `androidx.graphics.shapes` 或 Compose `SquircleShape` 近似；Windows 用 Composition `CompositionRoundedRectangleGeometry` 近似即可。

## 5. 阴影

| 元素 | 参数（SwiftUI `.shadow`） |
|---|---|
| 玻璃胶囊 | 黑 30%（浅色 12%），radius 14，y 6 |
| 典藏球（旧系统） | 黑 30%，radius 10，y 5 |
| 正在播放封面 | 黑 35%，radius 35，y 30 |
| 涂鸦线条辉光 | 白 50%，radius 4 |
| 其它 | 黑 35% / 40% / 45% 若干处，按出处照抄 |

SwiftUI 的 radius 约等于 CSS blur 的一半，换算时：Android `Modifier.shadow` 无法设颜色透明度与 y 偏移的组合，建议用 `drawBehind` + `BlurMaskFilter`；WinUI 用 `DropShadow`（BlurRadius ≈ radius×2）。

## 6. 动效

**通用曲线**
- `Animation.ui = .smooth(duration: 0.45)`（`App.swift`）——打开/关闭正在播放、全局状态切换都用它。
- Tab 切换：`.snappy`（系统默认时长）；Tab 选中胶囊用 `matchedGeometryEffect` 滑动。
- 正在播放出现：opacity + 向上 60pt；消失：opacity + 向下 80pt。
- 迷你播放器出现：从底部 move + opacity。

**入场 `MotionIn`（`Motion.swift`）**：每个视图实例只跑一次（push、切 tab、弹 sheet），滚动不触发。
- from bottom/top：偏移 ±16pt，spring(response 0.5, damping 0.8)
- from scale：0.92 → 1，spring(response 0.55, damping 0.68)
- 错峰：`stagger(i) = 0.15 + min(i,8) × 0.05` 秒
- 系统「减少动态效果」开启：只做 easeOut 0.25 的淡入，无位移

**滚动显现 `ScrollReveal`**：离开可视区时 opacity 0.55、scale 0.96（iOS 17 `scrollTransition`）。

**歌词 `BlurIn`**：blur 10 → 0 + 淡入，smooth 0.6。当前行切换：smooth 0.6。

**其它常用**：smooth 0.6（6 次）、smooth 0.45、smooth 0.35、spring(0.5, 0.82)（氛围切换）、spring(0.55, 0.85)（正在播放换氛围）、spring(0.45, 0.6)（典藏球选中）、snappy 0.22（开关）、easeInOut 1.6（涂鸦描线）、easeOut 0.55（DoodleTabs 描线）、easeInOut 2.4 循环（空状态漂浮 ±3pt）、easeInOut 0.8 循环（加载中月亮摇摆 ±12°）、easeInOut 0.9 循环（骨架屏 1↔0.55）。

SwiftUI spring(response, dampingFraction) → Compose `spring(dampingRatio = d, stiffness = (2π/response)²)`（质量 1）；WinUI 用 `SpringScalarNaturalMotionAnimation`（DampingRatio = d，Period = response/(2π)）。

### 6.1 节拍呼吸（铁律：只有涂鸦和背景呼吸）

- 代码注释原文：*Background breathing on the beat (doodles + field only; controls/text never move)*（`DreamField.swift`）；*Only time-driven pieces live in TimelineViews; chrome … never picks up beat/backdrop modulation*（`NowPlayingView.swift`）。
- **会动的**：`DreamField` 上叠一层径向渐变（blob[0] × 0.22 × pulse，半径 20→420，screen 混合）；`DoodleLayer` 整体以封面中心为锚点缩放 `1 + 0.04 × intensity × pulse`。
- **不动的**：顶栏、标题、时间、控制胶囊、操作行、菜单——它们放在 TimelineView 外面，不会因时钟重绘。控制胶囊用的是实心黑 32% 底 + 白 8% 0.5pt 描边，**故意不用玻璃**，避免把跳动的涂鸦折射进来。
- pulse 计算（`BeatTracker.swift` `BeatStore.pulse`）：
  - 有服务端 BPM：周期 60/BPM，相位对齐到最近一次检测到的 onset，`pulse = exp(−phase × 7)`。
  - 无服务端 BPM：最近一次 onset 后 `exp(−dt × 7)`（约 150 ms 衰减），3 秒内没有 beat 视为不活跃 → 0。
  - 没有真实播放（演示）时涂鸦用 `0.025 × intensity × max(0, sin(t·bpm/60·2π))`。
  - `outputLead = 0.12 s`：AVPlayer 的 tap 比扬声器早，脉冲向后挪 120 ms。其它平台要按自己的输出延迟重新测。
- 刷新率：所有节拍驱动视图用 30 fps（`minimumInterval: 1/30`）。
- 用户开关：梦境 sheet 里「随节拍律动」（`beatSync`）、强度滑块 `intensity`（默认 0.7，存 `ym.intensity`）；另有「随节拍轻震」触感（`ym.hapticBeat`，pulse 越过 0.85 时触发，强度 0.35 + energy×0.6）。

## 7. 封面色氛围

三套实现，用途不同：

1. **主色提取 `Palette.extract`**（`Palette.swift`）：缩到 24×24 → k-means（k=5，按亮度分位数确定初值，迭代 12 次）→ 评分 `count × (0.35 + 饱和度)` 取最高。用于决定页头文字亮/暗：主色与底色 70/30 混合后算 WCAG 亮度，深色模式 ≤0.5 用浅字，浅色模式 <0.36 用浅字。注释说与 `backdrop-algorithm.md` 一致（该文件不在本仓库中）。
2. **扩散背景 `RemoteDiffusion.diffuse` / `RemoteBackdrop`**（`CoverImage.swift`）：长边缩到 96px → 饱和度 1.8 → 高斯模糊 10 → 再取 `CIAreaAverage` 均值色。显示时：均值色铺底 + 模糊图放大 2.6×、中心在高度 40% 处；上面盖渐变到 `base`：stop 0 → 0%、0.25 → 13%、0.62 → 67%、1 → 100%。结果缓存，**不逐帧模糊**。设置类页面（`YMBackdrop`）用 420pt 高，深色 55%、浅色 35% 透明度。
3. **CoverBloom**（`Banner.swift`，专辑/歌手/歌单详情页）：封面放大 2.8×（下拉时 + stretch/400），blur 80，saturation 1.35，深色 0.95 / 浅色 0.8 透明度；遮罩 stop 0 → 10%（浅色 5%）、0.4 → 25%、0.78 → 80%、1 → 100%。下拉拉伸，上滑视差 0.45。
4. **正在播放 `DreamField`**：iOS 18+ 用 3×3 `MeshGradient`，30 fps 缓慢呼吸（中间点位移 `0.08·sin(t·0.35+k)`）；iOS 17 用 4 个圆 blur 70。顶部黑 10%、底部黑 28% 的线性遮罩保证文字可读。颜色来源：Core `GET /api/v1/tracks/{id}/dream` 返回 `colors`（≥2 个 hex）→ 前 3 个 + 默认第 4 色；或 Navidrome 歌曲上的 `colors`；都没有用默认色组。
5. **Blurhash 占位**：Core 返回 `blurhash` 时，封面加载前先显示（`CoverImage.swift`、`Blurhash.swift`）。

## 8. 玻璃面板

**只用在两处：底部 Tab 栏 和 迷你播放器**（`YMKit.swift` 注释：*Liquid glass is NOT used here (tab bar + mini player only)*）。Tab 栏中间的「典藏」球是 Tab 栏的一部分，也是玻璃。设置页、卡片、正在播放控制条都**不是**玻璃。（另：正在播放的「梦境」sheet 和 `AudioRoute` sheet 用系统 `.ultraThinMaterial` 作为 sheet 背景，Banner 上有一个 ultraThin 小胶囊——这些是系统材质，不算定制玻璃。）

### 8.1 `GlassCapsule`（`Glass.swift`）
- **iOS 26+**：系统 Liquid Glass `glassEffect(.clear, in: Capsule())`，上叠一层 tint：深色黑 10%，浅色白 20%。
- **iOS 17–25 回退（也是 Android/Windows 的参考实现）**，自下而上：
  1. `.ultraThinMaterial` 胶囊，整体透明度 0.35
  2. tint：深色黑 14%，浅色白 38%
  3. 高光：径向渐变 白 14%（浅色 30%）→ 透明，圆心 (0.3, 0)，半径 0 → 宽 × 0.6
  4. 镜面边缘 Metal 着色器 `glassRim`，强度深色 0.95 / 浅色 0.8
  5. 阴影 黑 30%（浅色 12%），radius 14，y 6
- **没有粗描边**（注释：*No thick border*），边缘只靠 `glassRim`。
- `.ultraThinMaterial` 的具体模糊半径**代码中未定义**（系统材质）；实测约 20–30pt，各平台请对照截图调。

### 8.2 `glassRim` 着色器（`Glass.metal`）
- 胶囊 SDF，`edge = smoothstep(1.6, 0, |d+0.8|)`（约 1.6px 亮边），`inner = smoothstep(10, 0, −d) × 0.10`（内侧 10px 微光）。
- 光照方向 (−1,−1) 归一化，`light = |dot(n, L)|` → 左上和右下最亮。
- `alpha = (edge × (0.18 + 0.82·light²) + inner·light) × intensity`，输出白色预乘。

### 8.3 `glassLens` 折射（`Glass.metal`，`glassLens` modifier）
- **现状：已实现但目前没有任何视图调用 `.glassLens()` / `.glassFrame()`**（全仓 grep 只有定义）。iOS 26 上折射由系统 Liquid Glass 提供；iOS 17–25 回退目前没有折射。其它平台第一版可以不做折射，等 iOS 启用后再跟进。
- 设计意图：作用于**玻璃下面的内容层**，最多 3 个矩形（通过 `GlassFrameKey` 收集 Tab 栏、迷你播放器的位置）。
- bezel 默认 14，strength 默认 16，`maxSampleOffset = strength × 1.2`。
- 在边缘 bezel 宽度内沿 SDF 法线偏移采样，`m = smoothstep` 曲线；RGB 三通道分别 ×1.00 / 0.92 / 0.84 偏移 → 轻微色散。
- 注释写明：*Same math ports 1:1 to Android AGSL RuntimeShader*。

### 8.4 迷你播放器
高 56，左内边距 8、右 16，`GlassCapsule` 上再叠 `base` 色：深色 55%、浅色 60%（保证文字对比）。封面 40×40 圆形，标题 14 semibold（中间截断），歌手 12 regular 70%。按钮：播放/暂停 20pt、下一首 18pt、队列 17pt。

### 8.5 Tab 栏
高 62，水平内边距 4，`GlassCapsule` 底；每项高 54；选中项：`accent` 色 + 背后一个 `fill×1.6` 的胶囊（matchedGeometry 滑动）；未选中 `fg` 75%。中间留 76pt 给「典藏」球。
**典藏球 `CentreOrb`**：64×64，向上偏移 18；iOS 26 `glassEffect(.regular.tint(封面色 45%).interactive())` + 白 35% 0.8pt 描边；旧系统 ultraThin + 封面色 42% + 径向高光白 35% + 白 40% 描边 + 阴影 黑 30% r10 y5。内部是蜡笔月亮（22pt，线宽 2.2，选中时旋转 −12°）+ 星（9pt，线宽 1.6，选中放大 1.15）+「典藏」10pt；选中时下方 5pt lime 圆点。spring(0.45, 0.6)；颜色变化 smooth 0.6。

## 9. 文字截断

规则：**标题类单行文字一律中间截断**（`.lineLimit(1).truncationMode(.middle)`），保留开头和结尾（歌名后缀常是 "(Live)"、"- Remastered" 等关键信息）。代码中使用的位置：
- 迷你播放器标题、正在播放标题、iPad 播放列
- 歌曲行标题（`TrackRow`、`Audio.swift` 队列）、更多菜单标题
- 设置页标题与行的 title/detail（`YMPage`、`YMRow`）
- 乐签歌名、识曲结果、排行条目、平台歌单名、「歌名 — 歌手」组合行

歌手名等副标题只 `lineLimit(1)`（默认尾部截断）。
平台对照：Android `TextOverflow.MiddleEllipsis`（Compose 1.7+ 才有，旧版本需自写测量）；WinUI 的 `TextTrimming` **没有中间截断**，要自己测量后在中间插入 "…"；macOS SwiftUI 直接复用。

## 10. 图标资源

- **App 图标：液态玻璃「梦境环」**，`Assets.xcassets/AppIcon.appiconset`，1024×1024 三套：`icon-dream-light.png`（默认）、`icon-dream-dark.png`（luminosity dark）、`icon-dream-tinted.png`（tinted）。其它平台从这三张导出：Android 自适应图标（前景 + 背景，Android 13 单色图标用 tinted 版），macOS 需要单独做 `.icns`/macOS 图标规格（圆角方形由系统**不会**自动加，要设计稿自带），Windows 用 light/dark 两套 `Square44x44Logo` 等。
- 应用内品牌图形 **MoonwaveIcon**（`OnboardingFlow.swift`）：1024 设计坐标的 Canvas 绘制——月牙（椭圆 (255,170,430,430) 减去 (370,210,330,330)，线宽 6）、9 根波形竖线（线宽 5.5）、四角星（中心 (720,254) 半径 34），按进度分段描出（0–0.55 月牙、0.4–0.85 波形、0.78–1 星）。
- 界面图标全部是 SF Symbols。其它平台需要映射表（建议 Android 用 Material Symbols Rounded，Windows 用 Segoe Fluent Icons），映射表**代码中没有**，需要新建。
- `Assets.xcassets` 里的 `cv*`、`ym_*` 图片是演示/截图用封面，正式版不需要。
