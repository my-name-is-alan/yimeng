# Windows / CGO 测试修复记录

2026-10-23 · Go 1.27.3 · Windows amd64 · CGO_ENABLED=0

## 结果

在 `core` 目录执行 `go test ./... -count=1` 和 `go vet ./...`，全部通过。
全量测试没有排除包或使用 `-skip`。转码测试使用本机 ffmpeg 运行。

## 修复

- 转码读句柄：Windows 使用 FILE_SHARE_READ / FILE_SHARE_WRITE / FILE_SHARE_DELETE，让多个渐进读者可以在文件完成并重命名后继续读取，取消时也能清理临时文件。缓存发布失败被明确传给读者，并清理 `.part`，不报告成功 EOF。
- WebP：测试按构建能力判断结果。开启 CGO 的构建要求 WebP，未开启时要求 JPEG 回退，并实际解码响应验证格式和尺寸，未跳过测试。
- 默认路径：Windows 原生运行使用用户 Music 与 LOCALAPPDATA/Yimeng。Linux/NAS 默认不变；配置测试使用本平台的绝对路径。NAS 示例 TOML 的 Windows 覆盖和拒绝不完整路径均有断言。
- Vault：Unix 检查并收紧 POSIX 模式，Windows 在创建文件时即应用禁止继承的 ACL，仅授予当前进程用户和 LocalSystem。加载宽松 ACL 时修复并回读验证；失败则拒绝返回密钥。测试实际放宽权限后验证修复结果，未用模拟的 Windows 权限位掩盖安全问题。
- 安全提醒测试：注册数据库关闭清理，避免 Windows 临时目录删除时文件仍被占用。
- 图片 LRU：缓存文件发布与字节计数、淘汰和统计快照同步；显式淘汰等待正在运行的淘汰完成，临时文件不计入可用缓存。避免旧统计覆盖新写入计数及最后一轮淘汰被跳过。

## 回归验证

- 转码、图片缓存、Vault、安全提醒及配置这五组包：连续 5 轮通过。
- 原图片 LRU 偶发用例：连续 30 轮通过。
- 新增覆盖：读者持有期间重命名/删除、缓存发布失败清理、并发图片发布/替换/淘汰计数一致性。
- 原有多用户隔离回归包含在此次全量测试中。

从 `core` 目录执行：

```powershell
$env:CGO_ENABLED = '0'
go test ./... -count=1
go vet ./...
go test ./internal/transcode ./internal/covers ./internal/vault ./internal/secalert ./internal/infra/config -count=5
```

## 验证范围

本机未提供可用的 C 编译器，因此未运行 CGO_ENABLED=1 的 WebP 编码分支或 `-race`，不将其描述为已通过。
Windows ACL 只约束文件访问，无法阻止拥有系统接管权限的人取得数据；Unix/Linux 行为保留独立实现，交叉编译不等同于 Linux 运行测试。
