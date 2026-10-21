# 译梦 Core · 核心架构

> 范围：仅 NAS 上的 Core 进程与其内部模块  
> 上游：[`architecture.md`](./architecture.md) 系统总览  
> 版本：v0.2 · 2026-10-21 · 语言=Go · 对应仓库 `~/code/yimeng/core`

---

## 1. Core 定位

Core 是局域网内**唯一真相源**：

| 拥有 | 不拥有 |
|------|--------|
| 曲库索引、合并实体、元数据缓存 | 屏幕渲染、快捷键 |
| 播放队列、Player 会话、取流协调 | 本机音频设备枚举与解码出声 |
| 平台凭证（Vault） | 长期存放在客户端的 Token |
| 扫描任务、缓存文件 | DRM 破解 / 非官方取流 |

客户端（Win / Mac / iOS / Android / Web）一律经 Gateway 访问；业务决策不在 UI 里做。

---

## 2. 进程与包结构

P0：**单进程、单二进制（或单 Node 服务）**，内部按模块分包，避免一上来微服务。

```text
yimeng/core/
  cmd/core/              # 入口：读配置、组装依赖、启 HTTP/WS
  internal/
    gateway/             # HTTP + WebSocket + 鉴权中间件
    app/                 # 用例编排（打开专辑、点播放、触发扫描）
    domain/              # 纯领域模型与规则（无 IO）
    library/             # 扫描、索引、合并
    metadata/            # MB / 平台 enrichment、Credits
    playback/            # Queue、Transport、Player 注册
    vault/               # 凭证加解密与刷新
    cache/               # 封面、波形、临时对象
    adapters/            # 各 Source 实现
      local/
      musicbrainz/
      apple/             # P2
      netease/           # P3
      qq/                # P3
    infra/
      db/                # SQLite（P0）迁移与仓储
      fs/                # 路径、只读挂载校验
      crypto/            # Vault 密钥派生
      config/            # 环境与配置
  api/openapi.yaml       # 对外契约（与 packages/shared 对齐）
  migrations/            # SQL 迁移
  configs/default.toml
```

依赖方向（严格单向，禁止反向引用）：

```text
gateway → app → (library | metadata | playback | vault)
app / library / metadata / playback → domain
library / metadata / playback → adapters (via SPI)
* → infra
adapters ↛ gateway / app   （适配器不感知 HTTP）
```

---

## 3. 逻辑分层（Core 内部）

```text
┌──────────────────────────────────────────────┐
│  Gateway        REST + WS + Pairing Auth     │
├──────────────────────────────────────────────┤
│  Application    用例：GetAlbum、Play、Scan…   │
├──────────────────────────────────────────────┤
│  Domain         Artist/Album/Track/Queue…    │
├──────────────────────────────────────────────┤
│  Services       Library · Metadata · Playback│
├──────────────────────────────────────────────┤
│  Adapter SPI    SourcePort / MetaPort        │
├──────────────────────────────────────────────┤
│  Infra          DB · FS · Cache · Crypto     │
└──────────────────────────────────────────────┘
```

- **Gateway**：协议边界；只做鉴权、DTO 映射、错误码。
- **Application**：一个用例一条编排；事务边界在这里打开。
- **Domain**：无网络、无 SQL；合并打分、队列规则放这里。
- **Services**：带状态的长驻能力（扫描队列、播放会话表）。
- **SPI**：适配器用接口接入，可单独 mock / 熔断。

---

## 4. 核心模块职责

### 4.1 Gateway

| 能力 | 说明 |
|------|------|
| HTTP | 曲库查询、搜索、设置、配对、触发扫描 |
| WebSocket | 播放态、队列、扫描进度、源状态 |
| Auth | 局域网配对码 → 设备 Token；公网强制 TLS（默认关） |
| 限流 | 按设备；Adapter 侧另有平台限流 |

错误约定：`4xx` 客户端可修；`503` + `source_unavailable` 表示某适配器挂了，其它源仍可用。

### 4.2 Library Service

职责：

1. 注册音乐根路径（多 path）。
2. 扫描本地：inode/mtime/hash 增量。
3. 拉取平台「用户库」（需 Vault，分期接入）。
4. 写入 **Source 层** 原始记录，再跑 **Merge** 生成规范实体。
5. 对外只暴露已合并的 `Artist / Album / Track`。

两层存储概念：

```text
source_*     各适配器原样落入（可脏、可重复）
canonical_*  合并后的统一实体（UI 只读这一层）
link_*       canonical ↔ source 的多对多映射
```

合并打分（与总览一致，落地为可测函数）：

1. MusicBrainz Recording / Release ID 命中 → 强合并。
2. ISRC 相同 → 强合并。
3. 否则：规范化标题 + 主艺人 + 时长±2s + 专辑名 加权；超过阈值才合并，否则并存并标「可能重复」。

### 4.3 Metadata Service

- 输入：canonical 实体或显式 `mb_id`。
- 输出：Credits、相关艺人/专辑、别名、发行年表。
- 策略：本地 DB 优先；缺失则异步向 MusicBrainz（及后期平台）补全，不阻塞专辑页首屏（首屏可先返回空 Credits + `enriching: true`，WS 补丁）。

### 4.4 Playback Orchestrator

概念：

| 对象 | 含义 |
|------|------|
| `Player` | 已配对、能出声的端（Web/桌面/后续设备） |
| `Session` | 某 Player 当前队列 + Transport |
| `Transport` | `idle \| playing \| paused \| buffering` |

状态机（单 Session）：

```text
idle → buffering → playing ⇄ paused → idle
         ↑____________|  (seek / 换曲重新 buffering)
任意态 --stop--> idle
```

取流：

```text
resolve(track_id):
  sources = links(track_id) 按偏好排序（local > 用户设定平台）
  for s in sources:
    try adapter.resolve_stream(s) → StreamLease{url, expires_at, format}
  fail → error playable_unavailable
```

- `StreamLease` 短 TTL；过期前 Orchestrator 可续签，客户端只拿当前 lease。
- **音频字节默认不经过 Core**（P0 本地 file URL / 后续平台官方流直达客户端）。Core 只协调许可与地址。例外：若某源强制同源代理，再加可选 `proxy` 模式（默认关）。

### 4.5 Credential Vault

- 存储： independently encrypted blob per `source`（apple / netease / qq）。
- 密钥：安装时生成 master key，放 NAS 文件权限 `0600`，或用户口令 KDF；**永不**下发客户端。
- API：仅 `playback` / `library` / 对应 adapter 经 Vault 门面取临时凭证；Gateway 无「导出 Token」接口。
- 刷新：Adapter 报 `auth_expired` → Vault 调 `refresh` → 失败则标 `source_needs_reauth`，WS 通知 UI。

### 4.6 Cache

| 键空间 | 内容 | 淘汰 |
|--------|------|------|
| `cover:` | 规范化封面 | LRU + 体积上限 |
| `wave:` | 波形可选 | 懒生成 |
| `http:` | 对 MB 等的短缓存 | TTL |

缓存可丢；丢了只变慢不变错。

---

## 5. Adapter SPI

```text
SourcePort:
  id() -> "local" | "apple" | "netease" | "qq" | "musicbrainz"
  health() -> ok | degraded | down

  # 目录（只读）
  search(q) -> Hit[]
  get_artist / get_album / get_track (source_id)

  # 用户库（需登录；local 扫盘代替）
  sync_library(cursor?) -> SyncPage

  # 播放（MusicBrainz 不实现）
  resolve_stream(source_track_id) -> StreamLease

  # 凭证
  refresh_credentials() -> void
```

规则：

- 一个 Adapter 崩溃 / 限流，**熔断该源**，不影响 Gateway 与其它源。
- 禁止 Adapter 直接写 canonical 表；只回传 DTO，由 Library/Metadata 入库。
- 禁止 Adapter 读其它 Adapter 的密钥。

P0 实现：`local` +（只读）`musicbrainz` 可后置。  
P2：`apple`。P3：`netease` / `qq`。

---

## 6. 数据模型（P0 SQLite 草图）

```sql
-- 规范实体
artists(id TEXT PK, name TEXT NOT NULL, mb_id TEXT, sort_name TEXT, created_at, updated_at)
albums(id, title, year INT, cover_path, mb_release_id, created_at, updated_at)
tracks(id, title, album_id, duration_ms, isrc, mb_recording_id, created_at, updated_at)
track_artists(track_id, artist_id, role TEXT)  -- primary|feat|…
album_artists(album_id, artist_id, role TEXT)
credits(id, track_id, role TEXT, name TEXT, mb_id)

-- 来源映射
source_tracks(id, source TEXT, source_track_id TEXT, raw_json, path TEXT, mtime, hash,
              UNIQUE(source, source_track_id))
track_links(track_id, source_track_id, confidence REAL)

-- 播放
players(id, name, device_type, last_seen_at, token_hash)
sessions(player_id PK, state TEXT, position_ms, updated_at)
queue_items(player_id, idx INT, track_id, UNIQUE(player_id, idx))

-- 凭证与设置
vault_entries(source TEXT PK, blob BLOB, updated_at)
settings(key TEXT PK, value TEXT)
scan_jobs(id, root TEXT, status, progress, error, started_at, finished_at)
```

索引要点：`tracks(album_id)`、`source_tracks(source, source_track_id)`、`artists(mb_id)`、全文搜索 P0 可用 `FTS5(title, name)`。

---

## 7. 对外 API（契约摘要）

与 `packages/shared`、OpenAPI 同步；此处为架构级清单。

### HTTP

| 方法 | 路径 | 用途 |
|------|------|------|
| POST | `/pair` | 配对码换设备 Token |
| GET | `/sources` | 源列表与健康 / 登录态 |
| GET | `/library/artists` | 艺人列表 |
| GET | `/artists/{id}` | 艺人页聚合 |
| GET | `/albums/{id}` | 专辑页聚合 |
| GET | `/search?q=` | 全局搜索 |
| POST | `/library/scan` | 触发本地扫描 |
| GET | `/library/scan/{id}` | 扫描状态 |
| GET | `/players` | 已注册 Player |
| POST | `/players/{id}/play` | 播放 / 换队列 |
| POST | `/players/{id}/pause` | 暂停 |
| POST | `/players/{id}/seek` | 进度 |
| POST | `/players/{id}/queue` | 改队列 |

专辑聚合 DTO 应对齐已定 UI：`album` + `tracks[]` + `credits[]` + `related_albums[]`。

### WebSocket `/ws`

服务端推送：

- `player.state` — transport、当前曲、position
- `queue.updated`
- `scan.progress`
- `source.status`
- `metadata.patched` — Credits 异步补齐

客户端上报：

- `player.progress` — position（节流）
- `player.ended` — 自然播完 → Orchestrator 切下一首

---

## 8. 关键路径（Core 内）

### 8.1 `GET /albums/{id}`

```text
Gateway
  → App.GetAlbum
    → Library.GetAlbum(id)           # canonical + tracks
    → Metadata.EnsureCredits(id)     # 同步读缓存；缺则投递异步任务
    → Library.RelatedAlbums(id)
  → 组装 AlbumPageDTO
```

### 8.2 `POST /players/{id}/play`

```text
Gateway (auth device owns player)
  → App.Play
    → Playback.SetQueue / LoadTrack
    → Playback.ResolveStream(track)
         → Vault.Get(source)（若需要）
         → Adapter.resolve_stream
    → WS broadcast player.state + lease
```

### 8.3 本地扫描

```text
Gateway → App.StartScan(root)
  → Library 建 scan_job
  → 后台：列目录 → source_tracks upsert
  → Merge 批次更新 canonical
  → WS scan.progress
  → 可选：投递 Metadata enrich
```

---

## 9. 配置与部署

`configs/default.toml` 关键项：

```toml
[server]
bind = "0.0.0.0:8741"

[library]
roots = ["/music"]

[db]
path = "/var/yimeng/library.db"

[vault]
path = "/var/yimeng/vault"
# master_key_file = "/var/yimeng/master.key"

[cache]
dir = "/var/yimeng/cache"
max_bytes = 2147483648

[auth]
pair_code_ttl_seconds = 300
```

Docker（与总览一致）：音乐目录**只读**挂载；`vault` / `db` / `cache` 读写卷。

---

## 10. 安全基线（Core 必做）

1. Vault blob 加密；备份脚本排除明文；日志脱敏（Token、Cookie、流 URL query）。
2. 设备 Token 哈希存储；可吊销单设备。
3. 路径穿越防护：本地流只允许 `library.roots` 下文件。
4. Adapter 出站超时 + 并发上限，防拖垮 NAS。
5. 默认不监听公网；若反代，外层 TLS + 关闭配对码改长期 Token。

---

## 11. 技术选型（已拍板）

| 项 | 选择 | 理由 |
|----|------|------|
| 语言 | **Go** | 单二进制、NAS 常驻占用低、部署简单 |
| DB | **SQLite + 迁移** | 个人单机足够；日后再评估 Postgres |
| 实时 | WebSocket | 播放态够用 |
| 容器 | Docker Compose 一服务 | 与 NAS 习惯一致 |
| 与前端契约 | **OpenAPI** → 生成 / 手写 `packages/shared` 类型 | Go 与 TS 解耦，契约为唯一对齐点 |

*拍板：2026-10-21 · 宝英选 Go。*

---

## 12. P0 落地顺序（只做 Core）

1. 仓库骨架 + 配置 + SQLite 迁移  
2. Gateway 健康检查 + 配对鉴权  
3. `local` Adapter + 扫描写入 `source_tracks`  
4. 最小 Merge → `albums/tracks` API（供已定专辑/艺人 UI）  
5. Playback：单 Player、本地 `file` lease、WS 状态  
6. （可选）MusicBrainz Credits 异步补齐  

明确不进 P0：Apple/网易/QQ、多 Zone、公网暴露、转码。

---

## 13. 与仓库目录的对应

| 路径 | 内容 |
|------|------|
| `yimeng/core/` | 本文所述代码 |
| `yimeng/packages/shared/` | DTO / 事件类型，与 OpenAPI 生成或手写对齐 |
| `yimeng/web/` | P0 调用 Core 的预览客户端 |
| `yimeng/docs/core-architecture.md` | 本文件 |

---

## 14. 开放问题（实现前拍板）

1. ~~Core 语言~~ → **已定 Go**。
2. SQLite 路径与音乐根目录在你 NAS 上的实际位置。
3. P0 本地播放：Core 用 HTTP range 提供本地文件（Web 预览必需），桌面端是否另支持直读 NAS 路径可后补。

---

*下一步：确认 P0 本地播放方式（HTTP range 静态档 vs 桌面自读路径）后，按 §12 继续实现。*
