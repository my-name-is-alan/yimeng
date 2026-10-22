import SwiftUI
import SafariServices

// MARK: - Models (D55 report, D56 Last.fm)

struct CoreReportItem: Codable, Hashable, Sendable { var id: String?; var name: String?; var artistName: String?; var plays: Int?; var listenedMs: Int? }
struct CoreReport: Codable, Sendable {
    var period: String?; var year: Int?; var month: Int?; var tz: String?
    var plays: Int?; var listenedMs: Int?; var uniqueTracks: Int?; var uniqueArtists: Int?; var uniqueAlbums: Int?; var activeDays: Int?
    var topTracks: [CoreReportItem]?; var topArtists: [CoreReportItem]?; var topAlbums: [CoreReportItem]?
    var byHour: [Int]?; var byWeekday: [Int]?; var byDay: [Int]?; var byMonth: [Int]?
    var isEmpty: Bool { (plays ?? 0) == 0 }
}
struct CoreLastFM: Codable, Sendable { var configured: Bool?; var connected: Bool?; var enabled: Bool?; var username: String?; var pending: Int? }

extension CoreClient {
    func report(period: String, year: Int? = nil, month: Int? = nil) async throws -> CoreReport {
        var q = ["period": period, "tz": TimeZone.current.identifier, "top": "10"]
        if let year { q["year"] = "\(year)" }
        if let month, period == "month" { q["month"] = "\(month)" }
        return try await get("/api/v1/me/report", query: q)
    }
    func lastfm() async throws -> CoreLastFM { try await get("/api/v1/me/lastfm") }
    /// Write-only: keys go straight into the request body, never into logs or app storage.
    func setLastfmKeys(apiKey: String, secret: String) async throws -> CoreLastFM {
        struct B: Encodable { let apiKey: String; let apiSecret: String }
        return try await send("PUT", "/api/v1/me/lastfm/keys", body: B(apiKey: apiKey, apiSecret: secret))
    }
    func lastfmAuthURL() async throws -> URL? {
        struct R: Codable { var authUrl: String? }
        struct E: Encodable {}
        let r: R = try await send("POST", "/api/v1/me/lastfm/auth", body: E()); return r.authUrl.flatMap(URL.init(string:))
    }
    func lastfmComplete() async throws -> CoreLastFM { struct E: Encodable {}; return try await send("POST", "/api/v1/me/lastfm/auth/complete", body: E()) }
    func lastfmEnable(_ on: Bool) async throws -> CoreLastFM { struct B: Encodable { let enabled: Bool }; return try await send("PUT", "/api/v1/me/lastfm", body: B(enabled: on)) }
    func lastfmDisconnect() async throws { try await fire("DELETE", "/api/v1/me/lastfm") }
    /// Account avatar if Core exposes one (GET /me avatar_url; asked for, may not exist yet → nil).
    func meAvatar() async -> URL? {
        struct Me: Codable { var avatarUrl: String? }
        guard let m: Me = try? await get("/api/v1/me"), let s = m.avatarUrl, !s.isEmpty, let b = snapshot().base else { return nil }
        return s.hasPrefix("http") ? URL(string: s) : URL(string: s, relativeTo: b)?.absoluteURL
    }
}

// MARK: - 听歌报告

struct ListeningReportView: View {
    @Environment(\.colorScheme) private var scheme
    @Environment(\.dismiss) private var dismiss
    @State private var period = "month"
    @State private var report: CoreReport?
    @State private var phase = 0   // 0 loading 1 ok 2 error
    private let cal = Calendar.current

    var body: some View {
        ScrollView(showsIndicators: false) {
            VStack(alignment: .leading, spacing: 0) {
                HStack {
                    Button { dismiss() } label: { CircleIconButton(icon: "xmark") }.buttonStyle(.plain)
                    Spacer()
                    DoodleTabs(items: ["本月", "今年"], selected: Binding(get: { period == "month" ? "本月" : "今年" }, set: { period = $0 == "本月" ? "month" : "year" }))
                        .fixedSize()
                        .frame(width: 150)
                }
                .padding(.top, 10)
                VStack(alignment: .leading, spacing: 4) {
                    Text("听歌报告").font(.system(size: 30, weight: .heavy))
                    Text(periodTitle).font(.system(size: 14)).foregroundStyle(Theme.secondary(scheme))
                    ScribbleUnderline().stroke(Theme.lime, style: StrokeStyle(lineWidth: 3, lineCap: .round)).frame(width: 120, height: 8)
                }.padding(.top, 18)

                if phase == 0 { DoodleLoader(text: "正在翻听歌日记…").frame(maxWidth: .infinity).padding(.top, 60) }
                else if phase == 2 { EmptyState(kind: .network, action: ("重试", { Task { await load() } })).padding(.top, 40) }
                else if let r = report, !r.isEmpty { content(r) }
                else { empty }
            }
            .padding(.horizontal, Theme.margin).padding(.bottom, 60)
        }
        .background(Theme.base(scheme).ignoresSafeArea())
        .foregroundStyle(Theme.fg(scheme))
        .task(id: period) { await load() }
    }

    private var periodTitle: String {
        let y = report?.year ?? cal.component(.year, from: .now)
        if period == "year" { return "\(y) 年" }
        return "\(y) 年 \(report?.month ?? cal.component(.month, from: .now)) 月"
    }
    private func load() async {
        phase = 0
        var m: Int? = nil
        #if DEBUG
        if ProcessInfo.processInfo.arguments.contains("-reportPast") { m = 1 }
        if ProcessInfo.processInfo.arguments.contains("-reportYear"), period == "month", report == nil { period = "year" }
        #endif
        do { report = try await CoreClient.shared.report(period: period, month: m); phase = 1 } catch { phase = 2 }
    }

    private var empty: some View {
        VStack(spacing: 14) {
            ZStack {
                ScribbleCircle().stroke(Theme.lime.opacity(0.8), style: StrokeStyle(lineWidth: 3, lineCap: .round)).frame(width: 110, height: 110)
                NoteScribble().stroke(Theme.fg(scheme).opacity(0.8), style: StrokeStyle(lineWidth: 3, lineCap: .round, lineJoin: .round)).frame(width: 46, height: 46)
            }
            Text(period == "month" ? "这个月还没有听歌记录" : "今年还没有听歌记录").font(.system(size: 17, weight: .bold))
            Text("去听几首喜欢的歌吧，这里会画出你的听歌时间、最爱的歌手和专辑。").font(.system(size: 13))
                .foregroundStyle(Theme.secondary(scheme)).multilineTextAlignment(.center)
        }.frame(maxWidth: .infinity).padding(.top, 70)
    }

    @ViewBuilder private func content(_ r: CoreReport) -> some View {
        let mins = (r.listenedMs ?? 0) / 60000
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(mins >= 600 ? "\(mins / 60)" : "\(mins)").font(.system(size: 84, weight: .black, design: .rounded)).rotationEffect(.degrees(-3))
                Text(mins >= 600 ? "小时" : "分钟").font(.system(size: 20, weight: .bold))
                StarScribble().stroke(Theme.lime, lineWidth: 2.5).frame(width: 26, height: 26).offset(y: -40)
            }
            Text("一共听了这么久").font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme))
        }.padding(.top, 24)

        HStack(spacing: 10) {
            stat(r.plays ?? 0, "次播放", 0)
            stat(r.uniqueTracks ?? 0, "首歌", 1)
            stat(r.uniqueArtists ?? 0, "位歌手", 2)
            stat(r.activeDays ?? 0, "天在听", 3)
        }.padding(.top, 18)

        section("听歌时间")
        DoodleBars(values: bars(r), labels: barLabels(r)).frame(height: 150).padding(.top, 8)

        if let t = r.topTracks, !t.isEmpty {
            section("最常听的歌")
            ForEach(Array(t.prefix(5).enumerated()), id: \.offset) { i, x in rankRow(i, x, coverId: x.id) }
        }
        if let a = r.topArtists, !a.isEmpty {
            section("最爱的歌手")
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 14) {
                    ForEach(Array(a.prefix(8).enumerated()), id: \.offset) { i, x in
                        VStack(spacing: 6) {
                            ZStack(alignment: .topLeading) {
                                MonogramAvatar(name: x.name ?? "?", size: 72, seed: i)
                                if i == 0 { CrownScribble().stroke(Theme.lime, style: StrokeStyle(lineWidth: 2.5, lineCap: .round, lineJoin: .round)).frame(width: 28, height: 20).offset(x: -6, y: -12).rotationEffect(.degrees(-14)) }
                            }
                            Text(x.name ?? "").font(.system(size: 13, weight: .semibold)).lineLimit(1).frame(width: 80)
                            Text("\(x.plays ?? 0) 次").font(.system(size: 11)).foregroundStyle(Theme.secondary(scheme))
                        }
                    }
                }.padding(.vertical, 6)
            }
        }
        if let a = r.topAlbums, !a.isEmpty {
            section("最常听的专辑")
            ForEach(Array(a.prefix(5).enumerated()), id: \.offset) { i, x in rankRow(i, x, coverId: x.id) }
        }
    }

    private func stat(_ n: Int, _ label: String, _ i: Int) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text("\(n)").font(.system(size: 26, weight: .heavy, design: .rounded))
            Text(label).font(.system(size: 11)).foregroundStyle(Theme.secondary(scheme))
        }
        .frame(maxWidth: .infinity, alignment: .leading).padding(12)
        .background(RoundedRectangle(cornerRadius: 14, style: .continuous).stroke(Theme.fg(scheme).opacity(0.18), style: StrokeStyle(lineWidth: 1.4, dash: [5, 3])))
        .rotationEffect(.degrees([-1.2, 0.8, -0.6, 1.1][i % 4]))
    }
    private func section(_ t: String) -> some View {
        HStack(spacing: 8) {
            Text(t).font(.system(size: 19, weight: .bold))
            ScribbleUnderline().stroke(Theme.lime.opacity(0.9), style: StrokeStyle(lineWidth: 2.5, lineCap: .round)).frame(width: 40, height: 6).offset(y: 6)
        }.padding(.top, 30).padding(.bottom, 6)
    }
    private func rankRow(_ i: Int, _ x: CoreReportItem, coverId: String?) -> some View {
        HStack(spacing: 12) {
            RankBadge(rank: i + 1)
            CoverImage(url: LibraryStore.shared.coverArtURL(id: coverId, size: 120), points: 48)
                .frame(width: 48, height: 48).clipShape(RoundedRectangle(cornerRadius: 9, style: .continuous))
            VStack(alignment: .leading, spacing: 2) {
                Text(x.name ?? "").font(.system(size: 15, weight: .semibold)).lineLimit(1).truncationMode(.middle)
                Text(x.artistName ?? "").font(.system(size: 12)).foregroundStyle(Theme.secondary(scheme)).lineLimit(1)
            }
            Spacer()
            Text("\(x.plays ?? 0) 次").font(.system(size: 12, weight: .medium).monospacedDigit()).foregroundStyle(Theme.secondary(scheme))
        }.padding(.vertical, 6)
    }
    /// Minutes are not bucketed by the server, so bars use plays: by_day for a month, days folded into months for a year.
    private func bars(_ r: CoreReport) -> [Double] {
        if period == "year" {
            if let m = r.byMonth, m.count == 12 { return m.map(Double.init) }
            let d = r.byDay ?? []
            if d.count == 12 { return d.map(Double.init) }  // Core buckets a year's by_day per month
            var out = [Double](repeating: 0, count: 12)
            let y = r.year ?? cal.component(.year, from: .now)
            for (i, v) in d.enumerated() {
                if let date = cal.date(from: DateComponents(year: y, month: 1, day: 1)).flatMap({ cal.date(byAdding: .day, value: i, to: $0) }) {
                    out[cal.component(.month, from: date) - 1] += Double(v)
                }
            }
            return out
        }
        return (r.byDay ?? []).map(Double.init)
    }
    private func barLabels(_ r: CoreReport) -> [String] {
        period == "year" ? (1...12).map { "\($0)月" } : (r.byDay ?? []).indices.map { $0 % 5 == 0 ? "\($0 + 1)" : "" }
    }
}

/// Hand-drawn bars: wobbly crayon strokes, tallest gets a star. No animation loops.
struct DoodleBars: View {
    let values: [Double]
    let labels: [String]
    @Environment(\.colorScheme) private var scheme
    @State private var shown = false
    var body: some View {
        GeometryReader { g in
            let n = max(values.count, 1), mx = max(values.max() ?? 0, 1)
            let w = g.size.width / CGFloat(n), h = g.size.height - 18
            ZStack(alignment: .bottomLeading) {
                Path { p in p.move(to: CGPoint(x: 0, y: h)); p.addLine(to: CGPoint(x: g.size.width, y: h + 1)) }
                    .stroke(Theme.fg(scheme).opacity(0.25), style: StrokeStyle(lineWidth: 1.5, lineCap: .round))
                ForEach(values.indices, id: \.self) { i in
                    let v = values[i], bh = max(v > 0 ? 6 : 2, CGFloat(v / mx) * (h - 14))
                    WobblyBar(seed: i).fill(v == mx && v > 0 ? Theme.lime : Theme.fg(scheme).opacity(v > 0 ? 0.55 : 0.12))
                        .frame(width: max(3, w * 0.62), height: shown ? bh : 2)
                        .position(x: w * (CGFloat(i) + 0.5), y: h - (shown ? bh : 2) / 2)
                    if v == mx && v > 0 {
                        StarScribble().stroke(Theme.lime, lineWidth: 2).frame(width: 14, height: 14).position(x: w * (CGFloat(i) + 0.5), y: h - bh - 12)
                    }
                    if i < labels.count, !labels[i].isEmpty {
                        Text(labels[i]).font(.system(size: 9)).foregroundStyle(Theme.secondary(scheme)).position(x: w * (CGFloat(i) + 0.5), y: h + 10)
                    }
                }
            }
        }
        .onAppear { withAnimation(.spring(response: 0.7, dampingFraction: 0.8)) { shown = true } }
    }
}
struct WobblyBar: Shape {
    let seed: Int
    func path(in r: CGRect) -> Path {
        let j = { (k: Int) -> CGFloat in CGFloat(((seed * 37 + k * 11) % 7) - 3) * 0.35 }
        var p = Path()
        p.move(to: CGPoint(x: r.minX + j(1), y: r.maxY))
        p.addQuadCurve(to: CGPoint(x: r.minX + j(2), y: r.minY + 2), control: CGPoint(x: r.minX - 1 + j(3), y: r.midY))
        p.addQuadCurve(to: CGPoint(x: r.maxX + j(4), y: r.minY + 1), control: CGPoint(x: r.midX, y: r.minY - 2 + j(5)))
        p.addQuadCurve(to: CGPoint(x: r.maxX + j(6), y: r.maxY), control: CGPoint(x: r.maxX + 1 + j(7), y: r.midY))
        p.closeSubpath()
        return p
    }
}

// MARK: - Last.fm

struct LastfmView: View {
    @State private var st: CoreLastFM?
    @State private var key = ""
    @State private var secret = ""
    @State private var busy = false
    @State private var authURL: URL?
    @State private var err: String?
    @State private var confirmDisconnect = false

    @Environment(\.colorScheme) private var scheme
    var body: some View {
        YMPage("Last.fm") {
            if let s = st {
                YMSection(footer: "你的听歌记录由译梦服务端代为上传到 Last.fm（只含这个账号的播放）。") {
                    YMRow(icon: "dot.radiowaves.left.and.right", "状态",
                          s.connected == true ? "已连接" + (s.username.map { " · \($0)" } ?? "") : s.configured == true ? "已填写密钥，待授权" : "未连接",
                          detailColor: s.connected == true ? Theme.accent(scheme) : nil)
                    if s.connected == true {
                        YMDivider()
                        YMToggleRow(icon: "arrow.up.circle", "同步听歌记录", isOn: Binding(get: { s.enabled ?? false }, set: { v in Task { await run { try await CoreClient.shared.lastfmEnable(v) } } }))
                        YMDivider()
                        YMRow(icon: "tray.full", "待上传", "\(s.pending ?? 0) 条")
                    }
                }
                if s.connected != true {
                    YMSection(title: "你的 Last.fm API 账号", footer: "在 last.fm/api/account/create 申请。密钥只发给你的译梦服务端并加密保存，不会显示或存在本机。") {
                        YMFieldRow(title: "API Key", prompt: "32 位", text: $key, secure: true)
                        YMDivider()
                        YMFieldRow(title: "Shared Secret", prompt: "32 位", text: $secret, secure: true)
                    }
                    let ok = !(busy || (s.configured != true && !(Self.valid(key) && Self.valid(secret))))
                    Button(busy ? "请稍候…" : (s.configured == true && key.isEmpty ? "去 Last.fm 授权" : "保存并去授权")) { Task { await connect() } }
                        .buttonStyle(PrimaryButtonStyle()).disabled(!ok).opacity(ok ? 1 : 0.4).padding(.top, 24)
                } else {
                    Button("断开 Last.fm") { confirmDisconnect = true }.buttonStyle(YMDestructiveButtonStyle()).padding(.top, 24)
                }
            } else if let err {
                YMSection { YMRow(icon: "exclamationmark.circle", err) }
            } else {
                ProgressView().frame(maxWidth: .infinity).padding(.top, 60)
            }
        }
        .task { await reload() }
        .sheet(item: Binding(get: { authURL.map { DebugID(id: $0.absoluteString) } }, set: { if $0 == nil { authURL = nil } }), onDismiss: {
            Task { await run { try await CoreClient.shared.lastfmComplete() } }   // back from the browser: confirm
        }) { d in SafariView(url: URL(string: d.id)!).ignoresSafeArea() }
        .confirmationDialog("断开 Last.fm？", isPresented: $confirmDisconnect, titleVisibility: .visible) {
            Button("断开", role: .destructive) { Task { try? await CoreClient.shared.lastfmDisconnect(); await reload() } }
        } message: { Text("会删除密钥、授权和未上传的记录。") }
    }
    static func valid(_ s: String) -> Bool { s.count == 32 && s.allSatisfy { $0.isHexDigit && !$0.isUppercase } }

    private func reload() async {
        do { st = try await CoreClient.shared.lastfm(); err = nil }
        catch let e as CoreAPIError where e.status == 403 { err = "设备配对登录不能使用 Last.fm，请用账号登录。" }
        catch let e as CoreAPIError where e.status == 501 { err = "服务端未启用 Last.fm。" }
        catch { err = "无法获取 Last.fm 状态" }
    }
    private func connect() async {
        busy = true; defer { busy = false }
        do {
            if !key.isEmpty { st = try await CoreClient.shared.setLastfmKeys(apiKey: key, secret: secret); key = ""; secret = "" }
            authURL = try await CoreClient.shared.lastfmAuthURL()
        } catch let e as CoreAPIError { SheetCenter.shared.say(e.code == "lastfm_no_keys" ? "请先填写密钥" : e.message.isEmpty ? "授权失败" : e.message) }
        catch { SheetCenter.shared.say("授权失败") }
    }
    private func run(_ f: () async throws -> CoreLastFM) async {
        do { st = try await f() }
        catch let e as CoreAPIError where e.code == "lastfm_not_authorized" { SheetCenter.shared.say("还没在 Last.fm 上同意授权") }
        catch { SheetCenter.shared.say("操作失败") }
    }
}

struct SafariView: UIViewControllerRepresentable {
    let url: URL
    func makeUIViewController(context: Context) -> SFSafariViewController { SFSafariViewController(url: url) }
    func updateUIViewController(_ vc: SFSafariViewController, context: Context) {}
}

// MARK: - Avatars

/// Real account avatar per source, else nil (→ monogram). Never a user-picked photo.
@MainActor
final class AvatarResolver: ObservableObject {
    static let shared = AvatarResolver()
    @Published private(set) var url: URL?
    private var key = ""
    func refresh() async {
        let lib = LibraryStore.shared
        let k = MusicSources.scopeId + "|" + lib.host + "|" + lib.user
        guard k != key else { return }
        key = k; url = nil
        if NavidromeClient.emby { url = lib.embyAvatar; return }
        if NavidromeClient.core {
            // D58: /me avatar (CoreProfileStore, authenticated bytes) → monogram. Platform avatars only on 个人资料.
            await CoreProfileStore.shared.load()
            return
        }
        if !NavidromeClient.fnos, let u = lib.navidromeAvatarURL() {
            // Navidrome: real only when it redirects to Gravatar; its built-in placeholder is not a person.
            var r = URLRequest(url: u); r.httpMethod = "GET"
            if let (_, resp) = try? await URLSession.shared.data(for: r), let h = resp as? HTTPURLResponse, h.statusCode == 200,
               (h.value(forHTTPHeaderField: "Content-Type") ?? "").hasPrefix("image/"), resp.url?.host?.contains("gravatar") == true {
                url = resp.url
            }
        }
    }
}

struct AvatarView: View {
    var size: CGFloat = 36
    @ObservedObject private var avatar = AvatarResolver.shared
    @ObservedObject private var library = LibraryStore.shared
    @AppStorage("ym.nickname") private var nickname = ""
    @ObservedObject private var profile = CoreProfileStore.shared
    var body: some View {
        Group {
            if NavidromeClient.core { if let a = profile.avatar { Image(uiImage: a).resizable().scaledToFill() } else { MonogramAvatar(name: profile.displayName ?? library.user, size: size) } }
            else if NavidromeClient.emby, let u = library.embyAvatar { CoverImage(url: u, points: size) }
            else if let u = avatar.url { CoverImage(url: u, points: size) }
            else { MonogramAvatar(name: displayName, size: size) }
        }
        .frame(width: size, height: size).clipShape(Circle())
        .task(id: library.host + library.user + "\(library.connected)") { await avatar.refresh() }
    }
    private var displayName: String {
        if !nickname.isEmpty { return nickname }
        if NavidromeClient.emby, !library.embyUserName.isEmpty { return library.embyUserName }
        return library.user.isEmpty ? "梦" : library.user
    }
}

/// Doodle monogram: first character on a palette blob with a crayon ring.
struct MonogramAvatar: View {
    let name: String
    var size: CGFloat = 36
    var seed: Int? = nil
    static let palette: [Color] = [Color(hex: 0x7a1cff), Color(hex: 0xe85cff), Color(hex: 0x3b82f6), Color(hex: 0x14b8a6), Color(hex: 0xff7a1a), Color(hex: 0xff2d7a)]
    var body: some View {
        let ch = String(name.trimmingCharacters(in: .whitespaces).first ?? "梦").uppercased()
        let i = seed ?? abs(name.unicodeScalars.reduce(0) { $0 &* 31 &+ Int($1.value) })
        let c = Self.palette[i % Self.palette.count]
        ZStack {
            Circle().fill(LinearGradient(colors: [c, c.opacity(0.7)], startPoint: .topLeading, endPoint: .bottomTrailing))
            ScribbleCircle().stroke(Color.white.opacity(0.55), style: StrokeStyle(lineWidth: max(1.2, size / 34), lineCap: .round)).padding(size * 0.07)
            Text(ch).font(.system(size: size * 0.44, weight: .heavy, design: .rounded)).foregroundStyle(.white).rotationEffect(.degrees(-6))
        }.frame(width: size, height: size).clipShape(Circle())
    }
}
