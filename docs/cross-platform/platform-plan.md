# 译梦 跨平台技术方案与分工

> 前提：Mac、Android、Windows 三端的**外观和行为要和 iOS 一模一样**；译梦服务端（Core）是主音源；性能第一；玻璃面板和涂鸦要做到完美。
> 依据：读的是 `ios/` 代码（74 个 Swift 文件，约 1.5 万行，部署目标 iOS 17，`project.yml` 用 XcodeGen）。仓库里的 `mac/`、`android/`、`win/` 目前都是空目录。

## 0. 先要解决的共同问题

1. **API 契约不一致**：iOS 调用的是 `/api/v1/...`（约 60 个端点，清单见 features-and-screens.md §4），而 `core/api/openapi.yaml` 描述的是 `/api/...`（无 v1，74 条路径），其中**不包含** `/home`、`/tracks/{id}/dream`、`/online/*`、`/recognize`、`/me/*`、`/progress`、`/sessions` 等客户端核心接口。Android/Windows 不能靠读 Swift 代码去猜接口，**第一件事是把 openapi 补成 v1 的真实契约**，再从它生成 Kotlin / C# 客户端。
2. **跨平台一致性的"真值"**：涂鸦的随机数（SplitMix64 + Swift 的 `Double.random(in:using:)`）、调色板提取、节拍检测、路径几何都要逐位一致。建议在 iOS 测试工程里导出 golden 数据（RNG 前 N 个值、每种涂鸦的路径点、给定图片的主色、给定 PCM 的 beat 时间），放到 `docs/cross-platform/golden/`，三端单测都对着它跑。
3. **共享资源**：字体（2 个内置 OFL 字体 + 2 个按需下载）、App 图标三套、SF Symbols → Material/Fluent 图标映射表（目前没有，需要新建）。

## 1. Mac：不做单独的 App，在现有 SwiftUI 代码上加原生 macOS target

**推荐**：在 `ios/project.yml` 里新增 `platform: macOS` 的 App target（macOS 14 起），和 iOS 共用模型、Core 客户端、Stores、涂鸦、Theme 和大部分视图，只给 Mac 单独写"外框"（窗口、侧边栏、菜单栏命令、键盘快捷键、迷你播放器窗口、菜单栏 Now Playing）。

理由：
- 行为一致是"白送"的：同一份 `PlayerStore`、`BeatAnalyzer`、`DoodleLayer`、`Glass.metal`，不存在移植误差。
- 技术上可行：`MTAudioProcessingTap`、vDSP、AVQueuePlayer、Metal 着色器（`layerEffect`/`colorEffect`/`distortionEffect` 需 macOS 14）、`MeshGradient`（macOS 15，否则已有 blur 回退）、App Intents、WidgetKit、`NSUserActivity` Handoff 在 macOS 上都有。macOS 26 也有 `glassEffect`。
- 工作量集中在：
  - 15 个文件直接用到 UIKit（`UIImage`、`UIApplication`、`UINavigationController` 等）：`CoreV31 NowPlayingInfo WidgetsIntents Palette PlayerStore EmptyStates CoverImage Yueqian GesturesV32 Blurhash CoreV25 MeView ElevatorIndex DebugSnap IPad`。用一个 `PlatformImage` typealias + `#if os(macOS)` 解决，大多是图片解码和触感。
  - `AVAudioSession`（iOS 专有）在 macOS 上去掉；输出设备选择改 Core Audio。
  - 灵动岛 / 实时活动、`AVRoutePickerView` 的 iOS 版、边缘右滑返回、触感 → Mac 上不做或换替代。
  - 宽屏布局以 iPad 的三栏布局（`IPad.swift`）为蓝本。
- 代码组织建议：等 iPad/设置页重做合并后，把非 UI 层抽成本地 Swift Package（如 `YimengKit`：Models、CoreClient、Stores、Audio、Doodle、Theme），iOS 和 macOS 两个 App target 都依赖它。**现在另一位同事正在改 `ios/`，所以这一步要等合并后再做，本次不动代码。**
- Hi-Res：macOS 上可以用 Core Audio 把设备采样率切到和音源一致（iOS 做不到），这是 Mac 版相对 iOS 的加分项，可第二阶段做。

**对比 Mac Catalyst**（简短）：
| | 原生 macOS target（推荐） | Catalyst |
|---|---|---|
| 代码复用 | 高（SwiftUI 共享，少量 `#if`） | 更高（几乎零改动即可跑） |
| 观感 | 真正的 Mac 控件、窗口、菜单、指针悬停 | 带 iPad 痕迹，菜单/窗口/字号需要大量修补 |
| 性能 | AppKit 底座，正常 | 多一层 UIKit-on-AppKit |
| 音频 | 可用 Core Audio 切采样率、选设备 | 受 iOS API 形态限制 |
| 结论 | 一次投入稍大，长期好维护 | 起步快，但"像 Mac 应用"这件事永远在补 |

## 2. Android：Kotlin + Jetpack Compose

- **UI**：Compose（Material 3 只当底座，视觉完全按 design-spec 自绘；不用 Material 的默认组件外观）。
- **玻璃**（只在 Tab 栏和迷你播放器）：
  - API 31+：`RenderEffect` 真实背景模糊，推荐用 **Haze**（chrisbanes/haze）做「内容层作为 hazeSource、玻璃作为 hazeEffect」，叠加 design-spec §8.1 的 tint 和径向高光；镜面边缘 `glassRim` 在 API 33+ 用 AGSL `RuntimeShader` 1:1 翻译（注释里本来就按 AGSL 写的）。
  - API 33+ 折射 `glassLens`（iOS 目前未启用，可晚做）同样用 AGSL。
  - API 31–32：有模糊，没有自定义着色器 → 边缘用渐变描边近似。
  - API < 31：没有实时背景模糊 → 半透明 `base` 色 + 高光 + 描边的降级样式（Haze 本身也会走降级）。建议 minSdk 26，31 以下接受降级。
- **涂鸦**：Compose `Canvas` + `PathMeasure` 描线，AGSL 做 wobble 和 MeshGradient（详见 doodle.md §4.1）。
- **播放**：Media3 / ExoPlayer。
  - 无缝：ExoPlayer 播放列表天然无缝（FLAC/WAV 无问题；MP3/AAC 依赖编码器延迟元数据）。
  - 过渡（crossfade）：**ExoPlayer 没有内建**，需要两个播放器实例交替 + 音量渐变，或自写 `AudioProcessor` 做混合。要排期，不是配置项。
  - 分析 + EQ：自定义 `AudioProcessor` 链：前级 → 12 个 biquad（RBJ 系数直接照抄 `FXParams.coefficients`）→ 限幅器 → 节拍分析（只读）。音效关时只挂分析处理器、样本原样输出，等价于 iOS 的「直通 + 只分析 tap」。
  - 回放增益：调 `player.volume`，不做 DSP。
  - 后台：`MediaSessionService`（Media3 session），锁屏/通知/蓝牙/Android Auto 控制都走它。
  - Hi-Res：Android 14+ 支持 `AudioMixerAttributes` bit-perfect（USB DAC）；更低版本由系统混音，诚实标注为"非直通"。
- **离线缓存**：Media3 `SimpleCache` + 自定义淘汰策略（照抄 iOS 的"未重播先删、旧的先删"）。
- **网络**：从 openapi 生成 Kotlin 客户端（Retrofit/Ktor 任选），Kotlinx Serialization。
- **小组件**：Glance（正在播放、今日乐签、私人 FM）；快捷方式用 App Shortcuts。

## 3. Windows：WinUI 3（Windows App SDK），评估结论：可行，推荐

- **为什么是 WinUI 3**：原生 Fluent、Mica/Acrylic 系统材质、Composition 动画和 Win2D 足够把涂鸦和玻璃做到位，C#/.NET 性能足够。
- **玻璃**：
  - 窗口背景用 **Mica**（`MicaBackdrop`），不算"玻璃面板"，只是窗口底。
  - Tab 栏/迷你播放器（Windows 上建议做成底部播放条 + 侧边栏，以 iPad 布局为蓝本）：WinUI 3 的 `AcrylicBrush` 只支持应用内（in-app）背景模糊，正好用它；要精确控制模糊半径和 tint，就用 Composition：`Compositor.CreateBackdropBrush()` + `GaussianBlurEffect`（Win2D effects）+ tint + 高光层；镜面边缘用 Win2D `PixelShaderEffect`（HLSL，从 Metal 翻译）或预渲染的边缘九宫格。
  - 风险：Composition backdrop 在 XAML 元素上的组合比 Android/iOS 繁琐，需要一个 PoC（1 周内）先验证 60 fps 和观感。
- **涂鸦**：Composition `CompositionSpriteShape`（原生 TrimEnd 动画，最适合描线）+ Win2D `CanvasAnimatedControl`（星尘、wobble）。见 doodle.md §4.3。
- **音频**（两档）：
  - 默认：`Windows.Media.Playback.MediaPlayer` + `MediaPlaybackList`（无缝、SMTC 系统媒体控件自带），解码由 Media Foundation（Win10+ 内置 FLAC）。缺点：拿 PCM 做节拍/EQ 不方便。
  - **推荐主路径**：NAudio + WASAPI。共享模式用于普通播放；**独占模式（`WasapiOut` Exclusive）用于 Hi-Res 直通**（采样率跟随音源，比特精确）。EQ/节拍分析写成 `ISampleProvider` 链。解码用 Media Foundation Reader 或 FLAC 解码库。SMTC 需要自己接 `SystemMediaTransportControls`。
  - 结论：用 NAudio/WASAPI 做主引擎，因为节拍呼吸需要 PCM、Hi-Res 需要独占；`MediaPlayer` 不作为主路径。
- **网络**：从 openapi 生成 C# 客户端（NSwag / Kiota）。

## 4. 是否用 Kotlin Multiplatform 给 Android + Windows 共享核心？

**结论：不建议。各平台原生，共享"契约和真值"，不共享代码。**

- Windows 选的是 WinUI 3（C#）。KMP 没有好的 C# 互操作路径：Kotlin/Native 的 `mingwX64` 产出 C 接口，要在 C# 侧 P/Invoke，协程、异常、对象生命周期全都要手工桥接，调试体验差。省下的代码量（网络 + 模型 + 少量算法）抵不上桥接成本。
- 真正值得共享的部分其实很薄：网络客户端可以从 openapi 各自生成；算法（RNG、调色板、节拍、EQ 系数）只有几百行，按 golden 数据各写一遍更安全，也更利于各自做性能优化（Android 用 `FloatArray`、C# 用 `Span<float>`/SIMD）。
- 唯一会让 KMP 划算的情况：Windows 改用 **Compose Multiplatform Desktop**（JVM + Skia）。那样 Android 和 Windows 可以共享几乎全部 UI 和逻辑，Haze 也支持桌面。但代价是：JVM 启动和内存更重、不是 Fluent 原生观感、没有 Mica、WASAPI 独占需要 JNA 自己桥。和"性能第一、看起来原生"的目标冲突，所以不推荐——如果以后人手紧、Windows 优先级下降，可以再评估这条路。

## 5. 分工

| 范围 | 负责人 |
|---|---|
| 设计规范（`design-spec.md`、`doodle.md` 及后续更新）、图标映射表、golden 截图 | **UI设计师（宝英）** |
| **Mac target**（与 iOS 共享 SwiftUI 代码，含 Mac 外框和抽包） | **UI设计师（宝英）** |
| 每个平台的设计验收（玻璃、涂鸦、动效、截断逐项过） | **UI设计师（宝英）** |
| Core 服务端（含补齐 `/api/v1` 端点） | **CodingBot** |
| Android App | **CodingBot** |
| Windows App | **CodingBot** |
| API 契约 `core/api/openapi.yaml`（v1） | **共同**：CodingBot 起草（以 iOS 实际调用为准），宝英确认客户端字段需求；任何一端改接口先改 openapi |
| golden 数据（RNG、路径、调色板、节拍） | 共同：iOS 侧导出（宝英），Android/Windows 单测接入（CodingBot） |

## 6. 里程碑顺序

1. **M0 准备**（与 M1 并行）：openapi v1 补齐；导出 golden 数据；图标映射表；等 iPad/设置页重做合并。
2. **M1 Mac**（最快复用）：抽 `YimengKit` 包 → macOS target 跑通曲库/播放/正在播放/涂鸦 → Mac 外框（侧边栏、菜单命令、快捷键、菜单栏控制）→ 小组件、Handoff。
3. **M2 Android**：PoC（Haze 玻璃 + 涂鸦描线 + 节拍呼吸，在中端机上测 60 fps）→ Core 全功能 → Navidrome → Media3 服务、离线、EQ、小组件 → crossfade。
4. **M3 Windows**：PoC（Composition 玻璃 + Win2D 涂鸦 + NAudio WASAPI 独占）→ Core 全功能 → SMTC、离线、EQ → 宽屏布局打磨。

每个里程碑结束都由宝英按 design-spec 做设计验收；同一首歌在 iOS 和新平台并排截图比对。
