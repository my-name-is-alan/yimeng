using System;
using System.Collections.Generic;
using System.IO;
using System.Net.Http;
using System.Net.Http.Headers;
using System.Text;
using System.Text.Json;
using System.Threading.Tasks;

namespace YimengProto.Core;

public sealed record CoreTrack(string Id, string Title, string Artist, string? CoverUrl, string? Origin = null);
public sealed record CorePlaylist(string Id, string Name, string? CoverUrl, string? Origin = null);
public sealed record HomeSection(string Id, string Title, string Kind, string? Origin, List<CoreTrack> Tracks, List<CorePlaylist> Playlists);
public sealed record HomePayload(List<HomeSection> Sections, List<CoreTrack> RecentlyPlayed, List<CorePlaylist> Playlists, string MeName);

public static class CoreApi
{
    // Bypass system proxy (FlClash/Meta) so LAN Core is reachable from Win.
    static readonly HttpClient Http = CreateHttp();
    static HttpClient CreateHttp()
    {
        var h = new HttpClientHandler { UseProxy = false, CheckCertificateRevocationList = false };
        return new HttpClient(h) { Timeout = TimeSpan.FromSeconds(20) };
    }
    public static string BaseUrl { get; private set; } = "http://127.0.0.1:8741";
    public static string AccessToken { get; private set; } = "";
    public static string UserLabel { get; private set; } = "";
    public static string? LastError { get; private set; }

    public static void LoadAccountFile(string path)
    {
        if (!File.Exists(path)) return;
        foreach (var line in File.ReadAllLines(path))
        {
            var i = line.IndexOf('=');
            if (i <= 0 || line.TrimStart().StartsWith('#')) continue;
            var k = line[..i].Trim();
            var v = line[(i + 1)..].Trim();
            if (k == "YM_CORE_BASE_WIN") BaseUrl = v.TrimEnd('/');
            // credentials applied in EnsureLogin via file re-read
        }
    }

    public static async Task<bool> EnsureLoginAsync(string accountPath)
    {
        if (!string.IsNullOrEmpty(AccessToken)) return true;
        string? user = null, pass = null;
        if (File.Exists(accountPath))
        {
            foreach (var line in File.ReadAllLines(accountPath))
            {
                var i = line.IndexOf('=');
                if (i <= 0) continue;
                var k = line[..i].Trim();
                var v = line[(i + 1)..].Trim();
                if (k == "YM_CORE_USER") user = v;
                else if (k == "YM_CORE_PASS") pass = v;
                else if (k == "YM_CORE_BASE_WIN") BaseUrl = v.TrimEnd('/');
            }
        }
        user ??= Environment.GetEnvironmentVariable("YM_CORE_USER");
        pass ??= Environment.GetEnvironmentVariable("YM_CORE_PASS");
        var baseEnv = Environment.GetEnvironmentVariable("YM_CORE_BASE_WIN");
        if (!string.IsNullOrEmpty(baseEnv)) BaseUrl = baseEnv.TrimEnd('/');
        if (string.IsNullOrEmpty(user) || string.IsNullOrEmpty(pass))
        {
            LastError = "缺少账号（core-account.env / 环境变量）";
            return false;
        }
        try
        {
            var body = $"{{\"username\":\"{Escape(user)}\",\"password\":\"{Escape(pass)}\",\"device_name\":\"win-proto\",\"device_type\":\"desktop\"}}";
            using var req = new HttpRequestMessage(HttpMethod.Post, $"{BaseUrl}/api/v1/auth/login")
            {
                Content = new StringContent(body, Encoding.UTF8, "application/json"),
            };
            using var resp = await Http.SendAsync(req);
            var text = await resp.Content.ReadAsStringAsync();
            if (!resp.IsSuccessStatusCode)
            {
                LastError = $"login {(int)resp.StatusCode}";
                return false;
            }
            using var doc = JsonDocument.Parse(text);
            var root = doc.RootElement;
            AccessToken = root.TryGetProperty("access_token", out var at) ? at.GetString() ?? "" : root.GetProperty("token").GetString() ?? "";
            if (root.TryGetProperty("user", out var u))
                UserLabel = u.TryGetProperty("nickname", out var n) ? n.GetString() ?? user : u.TryGetProperty("username", out var un) ? un.GetString() ?? user : user;
            else UserLabel = user;
            LastError = null;
            return !string.IsNullOrEmpty(AccessToken);
        }
        catch (Exception ex)
        {
            LastError = ex.Message;
            return false;
        }
    }

    static string Escape(string s) => s.Replace("\\", "\\\\").Replace("\"", "\\\"");

    static async Task<JsonDocument?> GetDocAsync(string path)
    {
        using var req = new HttpRequestMessage(HttpMethod.Get, $"{BaseUrl}{path}");
        req.Headers.Authorization = new AuthenticationHeaderValue("Bearer", AccessToken);
        using var resp = await Http.SendAsync(req);
        var text = await resp.Content.ReadAsStringAsync();
        if (!resp.IsSuccessStatusCode)
        {
            LastError = $"{path} → {(int)resp.StatusCode}";
            return null;
        }
        return JsonDocument.Parse(text);
    }

    static CoreTrack ParseTrack(JsonElement o) => new(
        o.TryGetProperty("id", out var id) ? id.GetString() ?? "" : "",
        o.TryGetProperty("title", out var t) ? t.GetString() ?? "未知" : "未知",
        o.TryGetProperty("artist_name", out var a) ? a.GetString() ?? "" : "",
        o.TryGetProperty("cover_url", out var c) ? c.GetString() : null,
        o.TryGetProperty("origin", out var org) ? org.GetString() : null
    );

    static CorePlaylist ParsePl(JsonElement o) => new(
        o.TryGetProperty("id", out var id) ? id.GetString() ?? "" : "",
        o.TryGetProperty("name", out var n) ? n.GetString() ?? "" : "",
        o.TryGetProperty("cover_url", out var c) ? c.GetString() : null,
        o.TryGetProperty("origin", out var org) ? org.GetString() : null
    );

    public static async Task<HomePayload?> FetchHomeAsync()
    {
        using var doc = await GetDocAsync("/api/v1/home");
        if (doc == null) return null;
        var root = doc.RootElement;
        var sections = new List<HomeSection>();
        if (root.TryGetProperty("sections", out var secs))
        {
            foreach (var s in secs.EnumerateArray())
            {
                var tracks = new List<CoreTrack>();
                if (s.TryGetProperty("tracks", out var ta))
                    foreach (var x in ta.EnumerateArray()) tracks.Add(ParseTrack(x));
                var pls = new List<CorePlaylist>();
                if (s.TryGetProperty("playlists", out var pa))
                    foreach (var x in pa.EnumerateArray()) pls.Add(ParsePl(x));
                sections.Add(new HomeSection(
                    s.GetProperty("id").GetString() ?? "",
                    s.GetProperty("title").GetString() ?? "",
                    s.TryGetProperty("kind", out var k) ? k.GetString() ?? "" : "",
                    s.TryGetProperty("origin", out var o) ? o.GetString() : null,
                    tracks, pls));
            }
        }
        var recent = new List<CoreTrack>();
        if (root.TryGetProperty("recently_played_tracks", out var rt))
            foreach (var x in rt.EnumerateArray()) recent.Add(ParseTrack(x));
        var playlists = new List<CorePlaylist>();
        if (root.TryGetProperty("playlists", out var pl))
            foreach (var x in pl.EnumerateArray()) playlists.Add(ParsePl(x));
        return new HomePayload(sections, recent, playlists, UserLabel);
    }

    public static async Task<List<CoreTrack>> FetchFmAsync(string origin = "netease")
    {
        using var doc = await GetDocAsync($"/api/v1/online/{origin}/fm");
        var list = new List<CoreTrack>();
        if (doc == null) return list;
        var root = doc.RootElement;
        var arr = root.TryGetProperty("items", out var i) ? i : root.TryGetProperty("tracks", out var t) ? t : default;
        if (arr.ValueKind == JsonValueKind.Array)
            foreach (var x in arr.EnumerateArray()) list.Add(ParseTrack(x));
        return list;
    }

    public static async Task<List<CoreTrack>> SearchAsync(string q)
    {
        var list = new List<CoreTrack>();
        if (string.IsNullOrWhiteSpace(q)) return list;
        var enc = Uri.EscapeDataString(q);
        using var doc = await GetDocAsync($"/api/v1/search?q={enc}&online=1");
        if (doc == null) return list;
        var root = doc.RootElement;
        JsonElement tracks = default;
        if (root.TryGetProperty("tracks", out var t))
            tracks = t.ValueKind == JsonValueKind.Object && t.TryGetProperty("items", out var items) ? items : t;
        if (tracks.ValueKind == JsonValueKind.Array)
            foreach (var x in tracks.EnumerateArray()) list.Add(ParseTrack(x));
        return list;
    }
}
