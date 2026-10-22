# 译梦 Windows Proto（玻璃 + 涂鸦）

WinUI 3 + Windows App SDK + Win2D。对应 design-spec §8 玻璃与 doodle.md 涂鸦的 Step-1 原型。

## 需要（在 Win11 机器上）

1. Visual Studio 2022 17.10+，工作负载：**使用 C# 的 WinUI 应用开发**
2. Windows 11 SDK（10.0.19041+）
3. 本机已注册 Cursor 桌面端并保持在线（当前 Mac 侧 `ListMachines` 显示 Win11 **未连接**，无法远程构建）

## 构建

```powershell
cd win\proto
dotnet restore YimengProto.sln
dotnet build YimengProto.sln -c Debug -p:Platform=x64
# 或用 VS 打开 YimengProto.sln → 部署到本地
```

## 已实现（源码）

- 窗口 **Mica** 底
- Tab 栏 / 迷你播放器：**AcrylicBrush**（Backdrop）+ tint（玻璃职责；完整 Composition `CreateBackdropBrush`+自定义半径留到正式开发）
- **Win2D** 涂鸦层：描线 trim 1.6s + 节拍缩放演示

## 未在本机验证

因 Win11 agent 离线，未出截图。连上后跑通再补 `/workspace` 录屏。
