# 译梦

个人非商用音乐管理 / 播放产品。Core 自托管于 NAS，客户端覆盖桌面、移动与 Web。

## 目录

| 目录 | 用途 |
|------|------|
| `core/` | NAS 上的核心服务：Gateway、Library、Metadata、Playback、Vault |
| `web/` | Web 预览与联调（P0） |
| `win/` | Windows 桌面客户端 |
| `mac/` | macOS 桌面客户端 |
| `ios/` | iOS 客户端 |
| `android/` | Android 客户端 |
| `packages/shared/` | 跨端共享类型、API 契约、设计 Token |
| `docs/` | 架构与设计文档 |

## 分期（摘要）

- **P0** 本地曲库 + Web 预览联调
- **P1** MusicBrainz 元数据
- **P2** Apple Music Adapter
- **P3** 网易云 / QQ 音乐 + 多源合并
- **P4** 多设备 / Zones

平台凭证只存 Core Vault；不绕 DRM。
