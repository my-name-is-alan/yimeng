# Mac 桌面布局落地（宝英 2026-10-23 认可）

## 定稿像素
- 首页：`home-v1.png` / `home-v1.html` — 侧栏 + 单列宽内容 + 底悬浮迷你条
- 全屏播放：`nowplaying-v2.png` / `nowplaying-v2.html` — 左歌词 + 右 iOS 梦境方封面（cloud/frame 涂鸦框），**不要黑胶**
- 队列变体：`nowplaying-queue-v1.*`（右侧播放列表）
- Mac 工程副本：`~/code/yimeng-clean/docs/cross-platform/mac-desk-home-v1.png`、`mac-desk-nowplaying-v2.png`

## 铁律（覆盖旧 platform-plan「以 iPad 三栏为蓝本」）
1. **不要** iPad `NavigationSplit` 双栏/三栏常驻播放列
2. 侧栏导航 + 单列宽内容；底 **悬浮迷你播放条**（可玻璃、透封面色）
3. 点迷你条 / 封面进入全屏播放页（独立全窗或全屏 cover）
4. 全屏封面 = iOS 梦境默认 frame/cloud，与手机同源 `DoodleLayer`
5. 导航图标涂鸦描边、彼此可区分；运输条非玻璃（实心黑半透明）

## 建议改动切入
- 审视 `IPad.swift` / `App.swift` 宽屏分支：Mac 宽屏走新 `MacShell`（侧栏+内容+MiniBar），不要复用 iPad 播放列
- 迷你条复用 iOS MiniPlayer 视觉 token，改桌面横向布局
- Now Playing 桌面：`HStack { Lyrics | Cover+Doodle }` + 底运输条；封面槽用 `DreamLayout` frame
- 键盘：⌘+ / ⌘- 歌词缩放（稿上已示意）

## 顺序
先壳（侧栏+内容+迷你条）→ 再全屏播放 → 截图对照 home-v1 / nowplaying-v2

## 已做（2026-10-23 · 第一刀壳）

- **Mac 壳确认**：`YimengMac/MacApp.swift` 的 `MacRoot` = 左侧栏 + 单列宽内容 + 底悬浮迷你条；**无** `NavigationSplit` 常驻播放列（iPad 仍走 `IPad.swift` 三栏）。
- **侧栏**：头像/用户名 + 涂鸦图标五 Tab（首页/曲库/典藏/搜索/我的）+ 底设置；选中用 accent lime。
- **迷你条（对照 home-v1）**：内容区宽悬浮（圆角 20）、封面色 bleed + 玻璃仅迷你条、三栏（信息/中控+lime 进度/词·队列·音量）、白底播放钮；点封面/信息进全屏 NP。
- **截图**：仓库外 `~/ymshots/mac-shell-v1.png`（对照稿 `mac-desk-home-v1.png`）；盒上副本 `/workspace/ym-proto-review/mac-desk/mac-shell-v1.png`。
- **本轮未做**：全屏 nowplaying-v2（左歌词 + 右 iOS 梦境方封面 frame/cloud）；现有 `MacNowPlaying` 仍为居中封面布局，**未引入黑胶**。

### 与定稿差距（壳）
- 侧栏次级区（我喜欢/最近/本地）与顶栏搜索胶囊未按 HTML 细抠——保留真实 Tab 信息架构。
- Home 内容区仍是现有乐签/FM/推荐，不是稿上的「乐馆」分区标签；壳层优先。

## 已做（2026-10-23 · 第二刀全屏 NP）

- `MacNowPlaying` 按 **nowplaying-v2**：左歌名/艺人 + `LiveLyricsView(bare:)`（当前行 lime）；右方封面 + `DoodleLayer` **强制 cloud/frame**（禁黑胶）；底 `MacNPTransport` **实心黑半透明**（非玻璃）+ 白进度条。
- 迷你条点进全屏；chevron / Esc / 运输条「迷你」回壳。
- ⌘= / ⌘- 歌词缩放（`ym.lyricScale`）。
- 截图：仓库外 `~/ymshots/mac-np-v1.png`；盒上 `/workspace/ym-proto-review/mac-desk/mac-np-v1.png`。

### 与定稿差距（NP）
- 歌词区用真实 `LiveLyricsView`，空歌词时显示空态而非稿上假句。
- 运输条右侧控件顺序/图标与 HTML 略有出入（队列/回壳），无独立「均衡器」入口（进音效设置）。
- 涂鸦为运行时 DoodleLayer，非稿上静态 SVG 云朵路径，但同源 frame/cloud 规则。
