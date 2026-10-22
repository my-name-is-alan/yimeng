package art.yimeng.proto.core

import android.content.Context
import art.yimeng.proto.BuildConfig
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONArray
import org.json.JSONObject
import java.util.concurrent.TimeUnit

data class CoreTrack(
    val id: String,
    val title: String,
    val artist: String,
    val coverUrl: String?,
    val origin: String? = null,
)

data class CorePlaylist(
    val id: String,
    val name: String,
    val coverUrl: String?,
    val origin: String? = null,
)

data class HomeSection(
    val id: String,
    val title: String,
    val kind: String,
    val origin: String?,
    val tracks: List<CoreTrack>,
    val playlists: List<CorePlaylist>,
)

data class HomePayload(
    val sections: List<HomeSection>,
    val recentlyPlayed: List<CoreTrack>,
    val playlists: List<CorePlaylist>,
    val meName: String,
)

object CoreApi {
    @Volatile var accessToken: String = ""
        private set
    @Volatile var baseUrl: String = BuildConfig.CORE_BASE.trimEnd('/')
        private set
    @Volatile var userLabel: String = ""
        private set
    @Volatile var lastError: String? = null
        private set

    private val http = OkHttpClient.Builder()
        .connectTimeout(8, TimeUnit.SECONDS)
        .readTimeout(20, TimeUnit.SECONDS)
        .build()
    private val jsonMedia = "application/json; charset=utf-8".toMediaType()

    fun loadAccountFromAssets(ctx: Context): Pair<String, String>? {
        return try {
            ctx.assets.open("core-account.env").bufferedReader().use { br ->
                val map = mutableMapOf<String, String>()
                br.forEachLine { line ->
                    val i = line.indexOf('=')
                    if (i > 0 && !line.trimStart().startsWith("#")) {
                        map[line.substring(0, i).trim()] = line.substring(i + 1).trim()
                    }
                }
                map["YM_CORE_BASE_ANDROID"]?.let { baseUrl = it.trimEnd('/') }
                val u = map["YM_CORE_USER"] ?: return null
                val p = map["YM_CORE_PASS"] ?: return null
                u to p
            }
        } catch (_: Exception) {
            null
        }
    }

    suspend fun ensureLogin(ctx: Context): Boolean = withContext(Dispatchers.IO) {
        if (accessToken.isNotEmpty()) return@withContext true
        val creds = loadAccountFromAssets(ctx) ?: run {
            lastError = "缺少 core-account.env（debug assets）"
            return@withContext false
        }
        try {
            val body = JSONObject()
                .put("username", creds.first)
                .put("password", creds.second)
                .put("device_name", "android-proto")
                .put("device_type", "android")
                .toString()
            val req = Request.Builder()
                .url("$baseUrl/api/v1/auth/login")
                .post(body.toRequestBody(jsonMedia))
                .build()
            http.newCall(req).execute().use { resp ->
                val text = resp.body?.string().orEmpty()
                if (!resp.isSuccessful) {
                    lastError = "login ${resp.code}"
                    return@withContext false
                }
                val o = JSONObject(text)
                accessToken = o.optString("access_token").ifEmpty { o.optString("token") }
                userLabel = o.optJSONObject("user")?.optString("nickname")
                    ?.ifEmpty { o.optJSONObject("user")?.optString("username") }
                    ?: creds.first
                lastError = null
                accessToken.isNotEmpty()
            }
        } catch (e: Exception) {
            lastError = e.message
            false
        }
    }

    private fun getJson(path: String): JSONObject? {
        val req = Request.Builder()
            .url("$baseUrl$path")
            .header("Authorization", "Bearer $accessToken")
            .get()
            .build()
        http.newCall(req).execute().use { resp ->
            val text = resp.body?.string().orEmpty()
            if (!resp.isSuccessful) {
                lastError = "$path → ${resp.code}"
                return null
            }
            return JSONObject(text)
        }
    }

    private fun absCover(raw: String?): String? {
        if (raw.isNullOrBlank()) return null
        if (raw.startsWith("http://") || raw.startsWith("https://")) return raw
        if (raw.startsWith("/")) return baseUrl.trimEnd('/') + raw
        return raw
    }

    private fun coverFrom(o: JSONObject): String? {
        val direct = o.optString("cover_url")
        if (direct.isNotBlank()) return absCover(direct)
        when (val cover = o.opt("cover")) {
            is String -> if (cover.isNotBlank()) return absCover(cover)
            is JSONObject -> {
                val u = cover.optString("url").ifEmpty { cover.optString("large") }
                if (u.isNotBlank()) return absCover(u)
            }
        }
        return null
    }

    private fun parseTrack(o: JSONObject): CoreTrack = CoreTrack(
        id = o.optString("id"),
        title = o.optString("title").ifEmpty { "未知曲目" },
        artist = o.optString("artist_name").ifEmpty {
            o.optJSONArray("artists")?.optJSONObject(0)?.optString("name").orEmpty()
        },
        coverUrl = coverFrom(o),
        origin = o.optString("origin").ifEmpty { null },
    )

    private fun parsePlaylist(o: JSONObject): CorePlaylist = CorePlaylist(
        id = o.optString("id"),
        name = o.optString("name").ifEmpty { o.optString("title") },
        coverUrl = coverFrom(o),
        origin = o.optString("origin").ifEmpty { null },
    )

    suspend fun fetchHome(): HomePayload? = withContext(Dispatchers.IO) {
        val o = getJson("/api/v1/home") ?: return@withContext null
        val sections = mutableListOf<HomeSection>()
        val arr = o.optJSONArray("sections") ?: JSONArray()
        for (i in 0 until arr.length()) {
            val s = arr.getJSONObject(i)
            val tracks = mutableListOf<CoreTrack>()
            val tArr = s.optJSONArray("tracks") ?: JSONArray()
            for (j in 0 until tArr.length()) tracks += parseTrack(tArr.getJSONObject(j))
            val pls = mutableListOf<CorePlaylist>()
            val pArr = s.optJSONArray("playlists") ?: JSONArray()
            for (j in 0 until pArr.length()) pls += parsePlaylist(pArr.getJSONObject(j))
            sections += HomeSection(
                id = s.optString("id"),
                title = s.optString("title"),
                kind = s.optString("kind"),
                origin = s.optString("origin").ifEmpty { null },
                tracks = tracks,
                playlists = pls,
            )
        }
        fun tracksFrom(key: String): List<CoreTrack> {
            val a = o.optJSONArray(key) ?: return emptyList()
            return (0 until a.length()).map { parseTrack(a.getJSONObject(it)) }
        }
        fun playlistsFrom(key: String): List<CorePlaylist> {
            val a = o.optJSONArray(key) ?: return emptyList()
            return (0 until a.length()).map { parsePlaylist(a.getJSONObject(it)) }
        }
        HomePayload(
            sections = sections,
            recentlyPlayed = tracksFrom("recently_played_tracks").ifEmpty {
                // albums as faux tracks
                (o.optJSONArray("recently_played") ?: JSONArray()).let { a ->
                    (0 until a.length()).map {
                        val x = a.getJSONObject(it)
                        CoreTrack(x.optString("id"), x.optString("title"), x.optString("artist_name"), x.optString("cover_url").ifEmpty { null })
                    }
                }
            },
            playlists = playlistsFrom("playlists"),
            meName = userLabel,
        )
    }

    suspend fun fetchFm(origin: String = "netease"): List<CoreTrack> = withContext(Dispatchers.IO) {
        val o = getJson("/api/v1/online/$origin/fm") ?: return@withContext emptyList()
        val a = o.optJSONArray("items") ?: o.optJSONArray("tracks") ?: return@withContext emptyList()
        (0 until a.length()).map { parseTrack(a.getJSONObject(it)) }
    }

    suspend fun search(q: String): List<CoreTrack> = withContext(Dispatchers.IO) {
        if (q.isBlank()) return@withContext emptyList()
        // Proto empty-state probe: never hits network (online fuzzy always returns hits)
        if (q.startsWith("____") || q == "∅") return@withContext emptyList()
        val enc = java.net.URLEncoder.encode(q, "UTF-8")
        val o = getJson("/api/v1/search?q=$enc&online=1") ?: return@withContext emptyList()
        val out = LinkedHashMap<String, CoreTrack>()
        fun addArr(a: JSONArray?) {
            if (a == null) return
            for (i in 0 until a.length()) {
                val tr = parseTrack(a.getJSONObject(i))
                if (tr.id.isNotBlank()) out.putIfAbsent(tr.id, tr)
            }
        }
        addArr(o.optJSONObject("tracks")?.optJSONArray("items"))
        addArr(o.optJSONArray("tracks"))
        // online platforms nest results under online[].result.tracks
        val online = o.optJSONArray("online")
        if (online != null) {
            for (i in 0 until online.length()) {
                val res = online.getJSONObject(i).optJSONObject("result")
                addArr(res?.optJSONArray("tracks"))
            }
        }
        out.values.toList()
    }

    suspend fun favorites(): List<CoreTrack> = withContext(Dispatchers.IO) {
        val o = getJson("/api/v1/favorites?type=track&limit=40") ?: return@withContext emptyList()
        val a = o.optJSONArray("items") ?: return@withContext emptyList()
        (0 until a.length()).map { parseTrack(a.getJSONObject(it)) }
    }
}
