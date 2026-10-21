# yimeng core

Go 版 Core（纯 Go，CGO-free，SQLite 用 modernc.org/sqlite）。

```sh
YIMENG_LIBRARY_ROOTS=$HOME/Music YIMENG_DB_PATH=./data/library.db YIMENG_CACHE_DIR=./data/cache \
  go run ./cmd/core
curl localhost:8741/healthz
# first run prints a pairing code in the log (or: curl -XPOST localhost:8741/api/pair/request)
curl -XPOST localhost:8741/api/pair -d '{"code":"ABCD-EF23","name":"laptop"}'   # -> {"token":"ymd_..."}
T=ymd_...   # keep it secret
curl -H "Authorization: Bearer $T" -XPOST localhost:8741/api/library/scan
curl -H "Authorization: Bearer $T" 'localhost:8741/api/albums?sort=year&order=desc&limit=20'
curl -H "Authorization: Bearer $T" -H 'Range: bytes=0-' localhost:8741/api/stream/<trackId>
```

- API 契约：`api/openapi.yaml`（测试保证所有路由均有文档）。
- 扫描：启动时自动扫描；fsnotify 监听 + 去抖；网络挂载 / inotify 上限时退回定时重扫（`YIMENG_WATCH`、`YIMENG_RESCAN_INTERVAL`）。
- 播放：`/api/players/{id}/play|pause|seek|next|prev|stop|repeat|queue`，WebSocket `/ws`（player.state / queue.updated / scan.progress / metadata.patched）。
- 元数据：MusicBrainz 默认关闭；`YIMENG_MB_ENABLED=true YIMENG_MB_CONTACT=you@example.com` 开启（≤1 req/s，Credits + Cover Art Archive 封面）。
- 艺人拆分：`GET/PUT /api/settings/artist-split`（例外名单、可选 & / , 、自定义分隔符）。
- 鉴权：配对码（单次、5 分钟）→ 设备 Token（库中仅存 SHA-256）；`/api/devices` 可吊销。

- 配置：`configs/default.toml` + `YIMENG_CONFIG`；环境变量覆盖（见 `internal/infra/config`）。
- 迁移：`migrations/NNNN_*.sql` 嵌入二进制，启动时按版本执行（`schema_migrations`）。
- 部署：`make dist`（linux amd64/arm64 静态二进制）、`make docker`（buildx 多架构），`compose.example.yaml`。

详见 ../docs/core-architecture.md
