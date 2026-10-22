# 译梦 涂鸦系统（跨平台交接）

> 来源：`Doodle.swift`、`DreamData.swift`、`EmptyStates.swift`、`CoreV25.swift`、`CoreV30.swift`、`CoreUI.swift`、`YMKit.swift`、`OnboardingFlow.swift`、`Glass.metal`、`BeatTracker.swift`、`Effects.swift`。数值全部摘自代码。

## 0. 总述

「涂鸦」是译梦的签名视觉：白色/墨色的手绘线条（蜡笔感），出现在正在播放的封面周围、Tab、空状态、头像、开关和图表上。要点：

- **全部程序化绘制，没有任何涂鸦图片资源**（`Assets.xcassets` 里没有涂鸦图；唯一的图片资源是 App 图标和演示封面）。所以移植 = 移植路径算法，不是导出 PNG。
- **随机性全部可复现**：用 `SeededRNG`（SplitMix64）按歌曲/元素做种子，同一首歌每次画出来一样。各平台必须用**同一个 PRNG 和同一个取数顺序**，才能做到像素级一致。
- **描线动画**：用 `trim(from: 0, to: drawn)` 把路径"画出来"。
- **蜡笔质感**两种：`Crayon`（同一路径描两遍，第二遍偏移 + 旋转）和 `doodleWobble`（Metal 扭曲着色器，随时间轻微抖动）。
- **只有涂鸦和背景随节拍呼吸**，控件和文字永不动（见 design-spec §6.1）。

## 1. 基础构件

### 1.1 SeededRNG（`DreamData.swift`）——必须逐位一致
```
init(seed):  state = seed &+ 0x9E3779B97F4A7C15
next():      state &+= 0x9E3779B97F4A7C15
             z = state
             z = (z ^ (z >> 30)) &* 0xBF58476D1CE4E5B9
             z = (z ^ (z >> 27)) &* 0x94D049BB133111EB
             return z ^ (z >> 31)
d(a, b):     Double.random(in: a...b, using: &rng)
```
⚠️ `d(a,b)` 用的是 Swift 标准库 `Double.random(in:using:)`，它内部怎么把 UInt64 映射到浮点数是 Swift 实现细节（目前是取高 52 位 × ulp，闭区间还有额外处理）。**Android/Windows 要做到完全一致，需要在 Swift 里写一个单元测试导出前 N 个 `d(0,1)` 值作为 golden 数据，其它平台对照实现。** 代码里目前没有这份 golden 数据。
种子：`DreamTrack.seed = id 每个 unicode 标量值之和`（UInt64）。

### 1.2 Crayon（`EmptyStates.swift`）
同一形状描两遍：
- 第 1 遍：颜色 90%，线宽 w，圆头圆角
- 第 2 遍：颜色 35%，线宽 0.7w，偏移 (0.8, −0.6)，旋转 0.8°
默认 w = 2.4，白色。

### 1.3 doodleWobble（`Glass.metal`，SwiftUI `distortionEffect`）
```
dx = sin(y*0.045 + t*1.7)*0.6 + sin(y*0.13 + t*0.9)*0.4
dy = cos(x*0.05  + t*1.3)*0.6 + sin(x*0.11 - t*1.1)*0.4
pos' = pos + (dx, dy) * amp * 0.5
```
amp = `track.jitter × intensity`，maxSampleOffset 6×6，t = 时间对 1000 取模。

### 1.4 形状库
- `DoodlePaths`（`Doodle.swift`）：frame（双圈潦草方框）、orbit（1.6 圈椭圆轨道）、cloud、moon、sparkle（四角星）、star（五角星，内径 0.45）、lightning、zigline、flower、loops（5 圈螺旋，160 点）、drip（滴落线 + 7×9 椭圆）。
- `EmptyStates.swift`：MoonShape、StarShape（四角）、RecordShape（三个同心圆 1 / 0.62 / 0.16）、WaveShape（6 段）、HeartShape。
- `CoreV25.swift`：ScribbleCircle（46 点，每点半径 ±1.2 抖动，40 点一圈 → 多画 6 点形成"没闭合"的重叠）、ScribbleUnderline（6 段二次曲线 + 回笔）、StarScribble、NoteScribble、CrownScribble。
- `CoreUI.swift` WobblyFrame（两遍抖动方框，幅度 min(3, 宽/40)，第二遍 ×1.6）；`CoreV30.swift` WobblyBar；`NowPlayingView.swift` SquiggleLine（每 10pt 一个波）。

## 2. 各组件

### 2.1 正在播放的涂鸦层 `DoodleLayer` ——"梦境"
**参数由歌曲推导（`DreamTrack`）**
| 字段 | 规则 |
|---|---|
| motif | energy > 0.7 → `zig`；genre ∈ {folk, acoustic, jazz} → `ink`；否则 `cloud` |
| layout | zig → orbit，ink → drip，cloud → frame |
| density（星星数） | `Int(4 + energy × 8)` |
| stroke 线宽 | `1.6 + energy × 1.4` |
| jitter | `1.5 + energy × 2.5` |

⚠️ 真实播放时（`applyNDSong`）energy 固定 0.45、genre 固定 "navidrome"，所以**目前真实歌曲总是 cloud / frame**；`/dream` 接口只会更新 colors、bpm、tags，不更新 energy/genre。zig/ink 目前只在 DEBUG 演示数据中出现。如果 Core 以后返回 energy/genre，三种构图都会出现——移植时三种都要做。

**封面位置**（402pt 设计画布，×width/402）
| layout | 封面矩形 | 圆角 | 标题 y |
|---|---|---|---|
| frame | (51,150,300,300) | 10 | 530，居中 |
| orbit | (40,180,230,230) | 圆 | 560，左对齐 |
| drip | (128,136,232,232) | 24 | 540，左对齐 |

**每种 motif 的线条**（全部合成一条 Path，一个 trim 画完）：
- cloud：封面外扩 14 的双圈 frame；两朵云（左上 w110、右下 w90）；两个月亮（r16、r10）；一个 sparkle r7。
- zig：椭圆 orbit（rx=0.62s, ry=0.5s，中心右移 14）；两道闪电（h46、h32）；右侧 zigline（长 ≤80，振幅 10，步长 14）；另外 density 颗五角星（只在 zig 下出现，位置/大小 4–10/透明度 0.5–0.95/旋转 用 `seed+99` 的 RNG 依次取）。
- ink：两朵花（r22 六瓣、r15 五瓣）+ 一朵小花 r10；左上 loops（长 120，r9）；三条滴落线（x 位于 0.22/0.48/0.71，长度 `22+(i×14)%30+10`）。

**样式**：白色，透明度 `0.6 + 0.3 × intensity`，线宽 stroke，圆头；白 50% radius 4 辉光；星星透明度跟随 drawn。
**动画**：换歌（track.id 变化）时 drawn 0 → 1，easeInOut 1.6 s；30 fps TimelineView 驱动 wobble 和节拍缩放（锚点为封面中心，`1 + 0.04 × intensity × pulse`）。
**用户可调**：梦境 sheet 选 `涂鸦 / 星尘 / 极光 / 纯净`，强度 0–1，随节拍开关。

### 2.2 正在播放的光环 `StardustHalo`（「星尘」氛围）
Canvas 绘制 200 个点，RNG 种子 = track.seed：
- 角度 `a = d(0,2π) + time×0.05`（整体缓慢旋转）
- 半径 `R = 封面宽 × 0.68`，`rr = R × (1 + 0.08·sin(7a)) + d(−10,10) + d(0,1)³ × 60`
- 点半径 `d(0.6, 2.4)`；颜色从 4 个 blob 色 + 两个白色里取；透明度 `d(0.35,1) × (0.5 + 0.5×intensity)`
- 混合模式 `plusLighter`（加色）
- RNG 取数顺序：a → rr 的抖动 → rr 的三次方项 → s → 颜色索引 → 透明度。**顺序必须一致。**

「极光」`AuroraBands`：两条胶囊渐变带（宽 1.4×，高 180 / 120，y 170 / 280，旋转 `−14+3sin(0.3t)` / `−8+3cos(0.25t)`），blur 30。「纯净」：什么都不画。

### 2.3 Doodle Tabs（`CoreV25.swift` `DoodleTabs`）
用于平台切换（排行的「本地/网易云/QQ/酷狗」）和听歌报告的「本月/今年」。
- 文字 18pt，选中 bold / 未选 medium；间距 26。
- 选中项：ScribbleCircle（seed = 文字字符数）在文字外 5pt 描圈，fg 55%，线宽 1.3；下方 ScribbleUnderline（seed = 字符数 + 7），**lime 85%，线宽 5**（荧光笔效果），高 10。
- 切换时 drawn 0 → 1，easeOut 0.55。
- 每项前有 `PlatformGlyph`（平台小图标，15×15，手绘）。

### 2.4 空状态插画（`EmptyStates.swift` `EmptyState`）
8 种：songs / albums / artists / playlists / starred / search / collection / network。插画区 150×120，全部 Crayon 组合：
- starred、collection：粉色心形 `#FF5FA8` 64×56 + 月亮 44
- network：月亮 60 + 波浪 90×12（旋转 −6°）
- search：圆 52 + 斜线 20 + 小月亮 22
- 其它：唱片 76（随 float 在 −4° ↔ 8° 摇摆）+ 月亮 30
- 每种都有两颗星：lime 14pt（透明度 0.4 ↔ 1 闪烁）+ ink 60% 9pt
- 整体上下漂浮 ±3pt，easeInOut 2.4 循环往复
- 文案：标题 17 semibold，说明 13 secondary 最宽 260；可选次按钮宽 160。文案在代码里，照抄。

其它同族：`DoodleLoader`（月亮 + lime 星，±12° 摇摆 0.8 s）、`FirstRunDoodle`（首次同步「正在把曲库搬进梦里…」）、`SkeletonTile`（方块里淡淡的唱片）、`DoodleBars`（听歌报告柱状图：WobblyBar，最高一根 lime + 星，spring(0.7,0.8) 长出来，无循环）、`YMDoodleRing`（缓存用量环：虚线轨道 + lime 双弧 + sparkle）、`YMToggleStyle`（开关：关=虚线胶囊，开=lime 填充 + 黑 35% 波浪线；圆钮 22pt 带偏移描边）、`YMCard`（设置卡的 1pt 虚线边框 dash [6,4]，fg 8%）、`RankBadge`（ScribbleCircle 填 18% + 描边）、歌词当前行下的 SquiggleLine。

### 2.5 涂鸦字母头像 `MonogramAvatar`（`CoreV30.swift`）
- 首字符（去空格，空则「梦」），大写。
- 颜色：6 色调色板（见 design-spec），索引 = seed ?? |Σ(h×31 + c)|（Int 溢出包装运算 `&*`、`&+`，其它平台要用 64 位环绕乘法）。
- 圆形线性渐变 c → c×0.7（左上 → 右下）；ScribbleCircle（默认 seed 1）白 55%，线宽 max(1.2, size/34)，内缩 size×0.07；字 size×0.44 heavy rounded，白色，旋转 −6°。
- 用在：我的页头像、个人资料页、平台账号、听歌报告的歌手。
`ArtistAvatar`（`EmptyStates.swift`）类似：首字母 + 白 50% 圆形 Crayon（线宽 1.2），图片加载完覆盖上去。

### 2.6 典藏球（Tab 栏中间）
蜡笔月亮 + 星，见 design-spec §8.5。

## 3. 和节拍/能量分析的连接

链路：**AVPlayerItem → audioMix → MTAudioProcessingTap → `TapState.process` → `BeatAnalyzer.feed` → `BeatStore` → 30fps 视图读取 `BeatStore.pulse`**。

- tap 一直挂着：音效关时是「只分析」tap（`fxOn = false`，样本不改）；音效开时先 EQ/限幅再分析。比特流（E-AC-3/杜比）不挂 tap → 没有节拍，`pulse` 落到服务端 BPM 或 0。
- `BeatAnalyzer`（`BeatTracker.swift`）：
  - hop = 1024 帧（采样率 > 60 kHz 时 2048），只用第一声道
  - 每个 hop：`vDSP_measqv` 均方能量 → `e = log1p(ms×1000)` → `flux = max(0, e − prev)`，`prev = prev×0.6 + e×0.4`
  - 环形缓冲 512 个 flux
  - 判拍：最近 ~1 s 的 mean/sd（`vDSP_normalize`），`flux > mean + 1.4·sd` 且 `flux > 0.02` 且距上次 > 280 ms → beat，`energy = min(1, e/6)`，平滑 0.7/0.3
  - BPM：每 ~2 s，用最近 ~12 s 做自相关，范围 60–180 BPM，85–145 BPM 加权 1.15（防倍频错误）；结果平滑 0.7/0.3
  - 渲染线程不分配内存，只用一把 `os_unfair_lock` 发布三个数
- 服务端 BPM（`/tracks/{id}/dream` 的 `bpm`）优先，相位对齐本地检测到的最后一个 onset。

## 4. 移植配方

通用原则：
1. 先移植 `SeededRNG` + `d(a,b)` 并用 golden 数据校验；再移植 `DoodlePaths` 等纯函数（输入矩形/种子 → 路径），这部分可以写成共享规格 + 每平台单测比对路径点。
2. 所有节拍驱动的东西放在一个 30 fps 的独立绘制层，控件层不参与重绘。
3. 描线动画都用「路径长度裁剪」实现。

### 4.1 Android（Jetpack Compose）
| iOS | Compose |
|---|---|
| `Shape.path(in:)` / `Path` | `androidx.compose.ui.graphics.Path`（`quadraticTo`、`cubicTo`、`arcTo`、`addOval`） |
| `.trim(from:0,to:p)` | `PathMeasure.getSegment(0, len×p, dst)`；多段合成路径用 `PathMeasure` 逐 contour 累加（iOS 的 trim 是按整条路径总长裁剪，要一致） |
| `Crayon` 两遍描边 | `drawPath` 两次，第二次 `withTransform { translate(0.8,-0.6); rotate(0.8°) }` |
| `doodleWobble` distortionEffect | API 33+：AGSL `RuntimeShader` + `RenderEffect.createRuntimeShaderEffect` 对涂鸦层做位移采样；API 31–32：`RenderEffect` 不支持自定义着色器 → 改为 CPU 对路径点施加同一位移函数（路径点数少，开销可接受）；更低版本同 CPU 方案 |
| `Canvas` 星尘 | `Canvas { drawCircle(...) }`，混合 `BlendMode.Plus`；200 点/帧没问题 |
| MeshGradient | 没有原生等价物。API 33+ 用 AGSL 写 3×3 双三次插值；更低版本用 4 圆 + `Modifier.blur(70.dp)`（API 31+）或预渲染位图 |
| `TimelineView(1/30)` | `withFrameNanos` 循环里按 33 ms 节流更新一个 `State<Float>`，只让涂鸦/背景 `drawWithCache` 读它 |
| 节拍分析 | Media3 `AudioProcessor`（`BaseAudioProcessor`，在 `queueInput` 里读 PCM，原样输出）——把 `BeatAnalyzer` 逐行翻译成 Kotlin，用 `FloatArray` 预分配，`AtomicLong`/`@Volatile` 发布 |
| 输出延迟 | `AudioTrack.getTimestamp()` 估算，替换 iOS 的 0.12 s |

### 4.2 macOS（SwiftUI，同一套代码）
- `DoodlePaths`、`Crayon`、各 Shape、`DoodleLayer`、`StardustHalo`、`Glass.metal` 的 `doodleWobble` **可以直接编译到 macOS 14+**（`distortionEffect`/`layerEffect`/`colorEffect` 在 macOS 14 可用；`MeshGradient` 需 macOS 15，否则走已有的 blur 回退）。
- 唯一要改的是 `UIImage` 相关（`Palette.swift`、`CoverImage.swift`）→ 用 `CGImage` 或 `NSImage` 包一层 typealias。
- 节拍分析：`MTAudioProcessingTap` 和 vDSP 在 macOS 上一样可用，代码不用改。

### 4.3 Windows（WinUI 3 + Win2D / Composition）
| iOS | WinUI 3 |
|---|---|
| `Path` | Win2D `CanvasPathBuilder`（`AddQuadraticBezier`、`AddCubicBezier`、`AddArc`）→ `CanvasGeometry` |
| `.trim` | Win2D `CanvasGeometry.ComputePathLength()` + `CanvasStrokeStyle` 的 dash 技巧（dash = [len×p, len]），或 Composition `CompositionSpriteShape.Geometry.TrimEnd`（Composition 原生支持 TrimStart/TrimEnd，**推荐**，可直接用 `ScalarKeyFrameAnimation` 做 1.6 s 描线） |
| `Crayon` | 两个 `CompositionSpriteShape`，第二个设 Offset 和 RotationAngleInDegrees |
| `doodleWobble` | Win2D `CanvasAnimatedControl` 里每帧对路径点施加同一位移函数（最稳）；或 Win2D `PixelShaderEffect`（HLSL，从 Metal 逐行翻译）作用于离屏 `CanvasRenderTarget` |
| 星尘 | `CanvasAnimatedControl` 里 `FillCircle` 200 次，`CanvasBlend.Add` |
| MeshGradient | 没有原生等价物 → 4 个圆画到小尺寸 `CanvasRenderTarget` 再 `GaussianBlurEffect`，或 HLSL 着色器 |
| 30 fps | `CanvasAnimatedControl.TargetElapsedTime = 33ms`，或 `CompositionTarget.Rendering` 节流 |
| 节拍分析 | NAudio：`ISampleProvider` 包装层在 `Read()` 里喂 PCM 给移植的 `BeatAnalyzer`（C#，`System.Numerics.Vector` 代替 vDSP）；用 `MediaPlayer` 的话需要 `AudioGraph` + `AudioFrameOutputNode` 拿 PCM |

## 5. 验收

- 对同一首歌（同一 id）在四个平台截正在播放页，叠图比对：路径位置误差 < 1pt，星星/星尘点位一致。
- 打开系统「减少动态效果」时，入场动画只淡入；涂鸦是否停止呼吸——**代码中目前没有对涂鸦做 reduce motion 处理**，建议各平台与 iOS 一起补上（产品决定）。
- 播放时用帧率工具确认：节拍只引起涂鸦层和背景层重绘，控件层 0 次重绘。
