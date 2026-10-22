# 译梦 Core 开发者指南

译梦 Core 提供两套标准接口，第三方开发者可以直接开发 App、脚本或网页应用：

| 接口 | 路径 | 适合 |
| --- | --- | --- |
| **OpenSubsonic** | `/rest/*` | 现成的 Subsonic 客户端（Symfonium、DSub、Feishin、play:Sub、substreamer…），或想复用 Subsonic 生态的新 App |
| **开放 API v1** | `/api/v1/*` | 需要译梦全部能力（在线音源、歌词、听歌报告、多端播放…）的新 App 与脚本 |

完整的接口定义见 `core/api/openapi.yaml`（OpenAPI 3.1）。

## 1. 获取凭据

在网页管理端 **管理 → 开发者** 中创建：

- **API 密钥**（`yk_` 开头）：用于开放 API，也可作为 Subsonic 的 `apiKey`。
- **Subsonic 应用密码**：给只支持「用户名 + 密码」的 Subsonic 客户端使用。**不是你的登录密码**，每个 App 单独一个，可随时撤销。

密钥只在创建时显示一次，请立即保存。凭据只属于你自己的账号，其他账号看不到你的歌单、收藏、播放记录。

### 权限范围（scopes）

| scope | 允许 |
| --- | --- |
| `read` | 所有读取接口（GET/HEAD），始终包含 |
| `playback` | 播放器控制、队列、串流/下载、记录播放、一起听 |
| `library-write` | 歌单、收藏、元数据编辑等其他写操作 |
| `admin` | 用户、系统、设置、音源、插件（仅管理员账号可授予） |

API 密钥**永远不能**管理凭据、设备、登录会话、配对或 Last.fm 密钥——这些只能在网页端由本人操作。缺少权限时返回 `403`，`error` 为 `insufficient_scope`，响应头 `WWW-Authenticate` 会写明所需 scope。

## 2. 开放 API（/api/v1）

```bash
export YM=http://nas.local:8741
export KEY=yk_xxxxxxxx   # 你的 API 密钥

# 读取专辑
curl -H "Authorization: Bearer $KEY" "$YM/api/v1/albums?limit=20"

# 搜索
curl -H "Authorization: Bearer $KEY" "$YM/api/v1/search?q=周杰伦"

# 创建歌单（需要 library-write）
curl -X POST -H "Authorization: Bearer $KEY" -H "Content-Type: application/json" \
     -d '{"name":"我的歌单"}' "$YM/api/v1/playlists"

# 串流（需要 playback）。<audio> 无法带请求头时可用 ?access_token=
curl -H "Authorization: Bearer $KEY" -o song.flac "$YM/api/v1/stream/<track_id>"
```

错误格式统一为：

```json
{ "error": "insufficient_scope", "code": "insufficient_scope", "message": "API key lacks the library-write scope" }
```

### 认证流程选择

1. **个人脚本 / 自用 App**：在「开发者」页创建 API 密钥，按需勾选 scope，写进 App 配置。
2. **给家人用的 App**：让每个用户在自己的账号下创建 API 密钥并粘贴到 App 里；或者 App 使用账号密码登录接口：
   ```bash
   curl -X POST -H "Content-Type: application/json" \
        -d '{"username":"me","password":"…","device_name":"我的App"}' "$YM/api/v1/auth/login"
   # → {"token":"ymd_…", ...}  之后用 Bearer ymd_… 调用（完整权限，可在「设备」页撤销）
   ```
3. **Subsonic 客户端**：见下一节。

### 版本策略

- 路径前缀 `/api/v1` 是大版本；每个响应带 `X-Yimeng-API-Version`（小版本，目前 `1.3`）。
- v1 内只做**向后兼容的新增**（新字段、新接口）。客户端请忽略未知字段。
- 要删除的字段会先在 openapi 中标记 `deprecated: true`，至少保留一个小版本。
- 不兼容的变更只会出现在 `/api/v2`，并与 v1 并存一段时间。

### CORS（浏览器网页应用）

默认不允许跨域。管理员在 **设置 → 运行设置 → 跨域来源 (CORS)** 填入允许的来源（例如 `https://app.example.com`），或设置环境变量 `YIMENG_CORS_ORIGINS`（逗号分隔，`*` 表示任意）。Core 不使用 Cookie，跨域请求请用 `Authorization` 请求头，不会发送 `Access-Control-Allow-Credentials`。

## 3. OpenSubsonic（/rest）

服务器地址填 `http://nas.local:8741`，所有接口同时支持 `/rest/name` 和 `/rest/name.view`，GET 或表单 POST；默认返回 XML，加 `f=json` 返回 JSON。

### 认证方式（按推荐顺序）

1. **apiKey**（OpenSubsonic 扩展 `apiKeyAuthentication`）：
   ```bash
   curl "$YM/rest/ping?apiKey=$KEY&v=1.16.1&c=myapp&f=json"
   ```
   同时传 `u` 会返回错误 43。
2. **token + salt**（经典 Subsonic）：用户名 + **应用密码**：
   ```python
   import hashlib, secrets, requests
   salt = secrets.token_hex(6)
   token = hashlib.md5((APP_PASSWORD + salt).encode()).hexdigest()
   r = requests.get(f"{YM}/rest/getAlbumList2", params=dict(
       u="me", t=token, s=salt, v="1.16.1", c="myapp", f="json", type="newest"))
   ```
3. **明文 p=**：只有创建应用密码时开启了「允许明文密码」才可用，默认拒绝（错误 41）。

登录密码**永远不能**用于 Subsonic。同一用户名 + IP 15 分钟内失败 10 次会被暂时锁定，并在仪表盘产生安全提醒。

### 支持的接口

ping、getLicense、getOpenSubsonicExtensions、getMusicFolders、getIndexes、getArtists、getArtist、getAlbum、getSong、getAlbumList2（getAlbumList）、search3（search2）、getPlaylists、getPlaylist、createPlaylist、updatePlaylist、deletePlaylist、star、unstar、getStarred2（getStarred）、scrobble、stream、download、getCoverArt、getLyrics、getLyricsBySongId（songLyrics 扩展）、getUser、savePlayQueue、getPlayQueue。

scope 对应：浏览类 = `read`；stream / download / scrobble / 播放队列 = `playback`；歌单写入与收藏 = `library-write`。应用密码默认拥有 read + playback + library-write。缺少权限返回错误 50。

### 在线曲目

在线音源（网易云、QQ 音乐等）的曲目只有在你的平台账号有完整播放权时才会出现在 Subsonic 结果中。**仅试听**或**不可播放**的曲目不会返回；直接请求其 stream 会被拒绝（HTTP 403 `preview_only`），不会给出 30 秒片段冒充整首。译梦不做任何解灰、跨平台抓取或 DRM 绕过。

### 封面与艺人图片

- `getCoverArt?id=<专辑/歌曲 id>` 或 `id=ar-<艺人 id>`，支持 `size`（120/300/600/1200 已预生成，其它尺寸按需生成）。
- 艺人对象带 `coverArt: "ar-<id>"`；有缓存的在线图片时还带 `artistImageUrl`。
- `getArtistInfo2` / `getAlbumInfo2` 返回 `smallImageUrl` / `mediumImageUrl` / `largeImageUrl`（指向 Core 图片缓存的绝对地址），在线艺人还有简介。

### 错误码

| code | 含义 |
| --- | --- |
| 0 | 服务器内部错误 |
| 10 | 缺少参数 |
| 40 | 用户名/密码错误或已锁定 |
| 41 | 不允许明文密码 |
| 43 | 同时提供了多种认证方式 |
| 44 | API 密钥无效 |
| 50 | 权限不足（scope） |
| 70 | 不存在 / 不可用 |

## 4. 图片缓存

所有封面与艺人图片都由 Core 缓存：平台 CDN 图片只下载一次，保存原图并预生成 120/300/600/1200 尺寸和模糊占位（blurhash、主色）。

```bash
# 艺人图片（本地或在线艺人）。顺序：管理员上传 → 平台艺人图 → 元数据自动匹配（仅名字完全一致）→ 最新专辑封面
curl -H "Authorization: Bearer $KEY" -o a.jpg "$YM/api/v1/artists/<artist_id>/image?size=600"

# 在线歌曲 / 歌单返回的 cover.url 形如 /api/v1/images/<key>，无需令牌，可直接用于 <img>
curl -o c.jpg "$YM/api/v1/images/<key>?size=300&format=jpeg"
```

- 响应带强 `ETag`，请用 `If-None-Match` 做客户端缓存。
- 平台风控期间仍返回已缓存的图片。
- `/api/v1/images/{key}` 只能访问 Core 自己登记过的、白名单图床上的图片，不是开放代理。
- 管理员可在 **设置 → 图片缓存** 查看命中率、占用并清除；上限 `YIMENG_IMAGE_CACHE_MB`（默认 1024），刷新周期 `YIMENG_IMAGE_CACHE_TTL_HOURS`（默认 720 小时）。

## 5. 安全建议

- 每个 App 用单独的凭据，只授予需要的 scope，不用时立即撤销。
- 优先使用 apiKey 或 token+salt；明文 `p=` 只给无法升级的老客户端。
- 不要把密钥写进公开仓库或前端代码；网页 App 应让用户自行输入自己的密钥。
- 局域网外访问请启用 TLS（`YIMENG_TLS_CERT` / `YIMENG_TLS_KEY`）或放在 HTTPS 反向代理后。
