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

- 串流：`/api/stream/{id}?format=original|aac|mp3|opus|flac&bitrate=` 可选 ffmpeg 转码（`YIMENG_FFMPEG`，`off` 关闭；PATH 中无 ffmpeg 时仅原始文件）；转码缓存 `YIMENG_CACHE_DIR/transcode`，LRU 上限 `YIMENG_CACHE_MAX_BYTES`，并发 `YIMENG_TRANSCODE_CONCURRENCY`；每设备默认音质 `PUT /api/devices/me/stream-quality`。官方镜像（distroless static）不含 ffmpeg，需要转码请挂载静态 ffmpeg 并设 `YIMENG_FFMPEG`。
- 随机播放：`POST /api/players/{id}/shuffle`（持久化的播放顺序视图，不改动队列）；`player.state` 带 `next`/`next_lease` 供无缝预取。
- 歌词：`GET /api/tracks/{id}/lyrics`（同名 .lrc → 内嵌 USLT/LYRICS → 可选提供方，LRC 同步解析）。
- 备份：每日 `VACUUM INTO` 快照（`YIMENG_BACKUP_DIR`/`_INTERVAL`/`_KEEP`），迁移前自动备份；`/api/system/backups`、`/api/system/export`、`/api/system/restore`（下次启动生效）。仅 admin 设备（首个配对设备）。设计取舍见 `docs/decisions.md`。
- 历史：自动记录播放（≥50%/4 分钟），`/api/history`、`/api/history/top`、`POST /api/scrobble`；收藏 `/api/favorites`、评分 `/api/ratings`；曲目带 play_count/favorite/rating，`?favorite=true&sort=plays`。
- 歌单：`/api/playlists` 静态/智能（规则 JSON：最近添加、最常播放、字段筛选），M3U 导入/导出。
- 响度：读取 REPLAYGAIN_*/R128；缺失时后台用 ffmpeg ebur128 分析（`YIMENG_LOUDNESS_ANALYSIS`），增益见曲目 `stream.replaygain` 与 `player.state`。
- 系统：`GET /api/system`（版本/曲库统计/扫描/ffmpeg 能力/根目录诊断）；日志 `YIMENG_LOG_FORMAT=json|text`、`YIMENG_LOG_LEVEL`（访问日志不记 query）。
- 发现：局域网 mDNS `_yimeng._tcp`（`YIMENG_MDNS`/`server.mdns`，默认开）。
- 多端：`POST /api/players/{id}/transfer` 移交队列；`/ws` 在线状态（device.online/offline，`player.state.online`）。
- 管理：设备改名/管理员、`/api/settings/library-roots`、元数据覆盖层（抗重扫）、专辑合并/拆分。
- 封面缩略图：`/api/.../cover?size=&format=jpeg|webp|png`（纯 Go 缩放缓存）。
- 歌词远程：默认启用 LRCLIB（`metadata.providers` / `YIMENG_PROVIDERS=lrclib`）；网易云/QQ 为 SPI stub（无安全公开 API）。`GET /api/metadata/providers` 列出已启用的 MetadataProvider。
- HTTPS / 代理 / 限流：`YIMENG_TLS_CERT`+`YIMENG_TLS_KEY`；`YIMENG_TRUSTED_PROXIES`（CIDR，信任后才读 X-Forwarded-For）；`YIMENG_RATE_LIMIT`（默认 50 req/s/客户端）与 `YIMENG_GLOBAL_RATE_LIMIT`；超限 429 + Retry-After。
- 离线下载：`GET /api/tracks/{id}/download`（`Content-Disposition: attachment`；可选 `format`/`bitrate` 转码后整文件下载）。
- Subsonic：`/rest/*.view`（默认开，`YIMENG_SUBSONIC=false` 关）；密码=配对设备 token（`ymd_…`）；不支持 t/s token+salt。
- 流媒体账号：`GET /api/sources`；登录/登出/同步为 admin（凭据仅进 Vault，永不下发）。网易云/QQ/Apple 现为 stub（可存 cookie，播放 API 未接）。
- 格式：本地库索引 AIFF/AIF、APE、DSF、DFF（纯 Go 探测时长/采样率；标签靠 dhowden/tag 能读的部分）。
- 配置：`configs/default.toml` + `YIMENG_CONFIG`；环境变量覆盖（见 `internal/infra/config`）。
- 迁移：`migrations/NNNN_*.sql` 嵌入二进制，启动时按版本执行（`schema_migrations`）。
- 部署：`make dist`（linux amd64/arm64 静态二进制）、`make docker`（buildx 多架构），`compose.example.yaml`。

详见 ../docs/core-architecture.md
