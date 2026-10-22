import SwiftUI

// MARK: - Scrubbable progress (used by phone + iPad players). Haptic ticks every 10 s while dragging.

struct ScrubBar: View {
    let elapsed: TimeInterval; let total: TimeInterval
    @State private var drag: Double?
    @State private var lastTick = -1
    var body: some View {
        let shown = drag ?? min(1, elapsed / max(total, 1))
        HStack(spacing: 10) {
            Text(fmtTime(shown * total)).monospacedDigit()
            GeometryReader { g in
                ZStack(alignment: .leading) {
                    Capsule().fill(Color.white.opacity(0.3))
                    Capsule().fill(Color.white).frame(width: g.size.width * CGFloat(shown))
                    if drag != nil { Circle().fill(.white).frame(width: 12, height: 12).offset(x: g.size.width * CGFloat(shown) - 6) }
                }
                .frame(height: drag == nil ? 3 : 6).frame(maxHeight: .infinity)
                .contentShape(Rectangle())
                .gesture(DragGesture(minimumDistance: 0).onChanged { v in
                    let f = max(0, min(1, Double(v.location.x / max(g.size.width, 1))))
                    if drag == nil { Haptics.tap() }
                    drag = f
                    let tick = Int(f * total / 10)
                    if tick != lastTick { lastTick = tick; Haptics.tick() }
                }.onEnded { _ in
                    if let f = drag { PlayerStore.shared.seek(to: f * total) }
                    Haptics.tap(); drag = nil; lastTick = -1
                })
            }.frame(height: 22)
            Text(fmtTime(total)).monospacedDigit()
        }
        .font(.system(size: 12)).opacity(0.85)
        .animation(.snappy(duration: 0.2), value: drag == nil)
        .accessibilityElement().accessibilityLabel("播放进度").accessibilityValue(fmtTime(elapsed))
        .accessibilityAdjustableAction { d in PlayerStore.shared.seek(to: elapsed + (d == .increment ? 15 : -15)) }
    }
}

// MARK: - Keyboard shortcuts (hardware keyboard, iPhone + iPad)

struct PlayerShortcuts: View {
    @EnvironmentObject var state: AppState
    @ObservedObject private var player = PlayerStore.shared
    var body: some View {
        ZStack {
            Button("播放/暂停") { state.setPlaying(!(player.hasRealPlayback ? player.isPlaying : state.isPlaying)) }.keyboardShortcut(.space, modifiers: [])
            Button("上一首") { state.skip(-1) }.keyboardShortcut(.leftArrow, modifiers: .command)
            Button("下一首") { state.skip(1) }.keyboardShortcut(.rightArrow, modifiers: .command)
            Button("搜索") { withAnimation(.snappy) { state.nowPlaying = false; state.path = []; state.tab = .search } }.keyboardShortcut("f", modifiers: .command)
        }
        .opacity(0).frame(width: 0, height: 0).accessibilityHidden(true).allowsHitTesting(false)
    }
}

// MARK: - Handoff: current track + position. Core resumes via its progress sync.

enum YMHandoff {
    static let type = "art.yimeng.app.playing"
    @MainActor static func fill(_ a: NSUserActivity) {
        let p = PlayerStore.shared
        guard let s = p.song, let d = try? JSONEncoder().encode(s) else { a.isEligibleForHandoff = false; return }
        a.title = "\(s.title) — \(s.artist)"
        a.isEligibleForHandoff = true
        a.addUserInfoEntries(from: ["song": d, "pos": p.elapsed, "source": MusicSources.scopeId])
        if NavidromeClient.core {   // make sure the other device's Core lookup sees where we are now
            let id = s.id, ms = Int(p.elapsed * 1000), dur = p.duration > 0 ? Int(p.duration * 1000) : nil
            Task { await CoreClient.shared.putProgress(id, ms: ms, durationMs: dur) }
        }
    }
    @MainActor static func continueActivity(_ a: NSUserActivity) {
        guard let d = a.userInfo?["song"] as? Data, let s = try? JSONDecoder().decode(NDSong.self, from: d) else { return }
        let pos = a.userInfo?["pos"] as? Double ?? 0
        if (a.userInfo?["source"] as? String) != MusicSources.scopeId { SheetCenter.shared.say("另一台设备用的是不同的音乐来源"); return }
        PlayerStore.shared.play(songs: [s])          // the user chose to continue here
        if !NavidromeClient.core, pos > 3 {           // Core: startCurrent pulls /progress itself
            Task { try? await Task.sleep(for: .seconds(1)); PlayerStore.shared.seek(to: pos) }
        }
        AppStateBridge.current?.nowPlaying = true
    }
}

// MARK: - iPad: sidebar + list + persistent player column

extension Shell {
    var padLayout: some View {
        NavigationSplitView(columnVisibility: .constant(.all)) {
            PadSidebar()
                .navigationSplitViewColumnWidth(min: 200, ideal: Self.padWide ? 200 : 236, max: 260)
                .toolbar(removing: .sidebarToggle)
                .toolbar(.hidden, for: .navigationBar)
        } content: {
            GeometryReader { g in
                let _ = { YMLayout.pad = true; YMLayout.width = g.size.width }()
                padRoot
                    .id(YMLayout.bucket(g.size.width))
                    // Portrait hides the sidebar: keep the floating glass tab bar so every tab stays one tap away.
                    .overlay(alignment: .bottom) { if padPortrait { padTabBar.padding(.horizontal, 16).padding(.bottom, 8) } }
                    .frame(width: g.size.width, height: g.size.height)
            }
            .toolbar(.hidden, for: .navigationBar)
            .background(YMBackdrop())
            .navigationSplitViewColumnWidth(min: 360, ideal: Self.padWide ? 760 : 560, max: Self.padWide ? 900 : 700)
        } detail: {
            PadPlayerColumn()
                .navigationSplitViewColumnWidth(min: 280, ideal: Self.padWide ? 300 : 480, max: Self.padWide ? 300 : 620)
        }
        .navigationSplitViewStyle(.balanced)
        // The persistent player column is always dark; the rest follows it unless the user explicitly picked 浅色.
        .environment(\.colorScheme, padAppearance == 1 ? .light : .dark)
    }

    /// Debug `-padWide`: a wider content column, to verify the ≥ 700 pt two-column Home.
    static var padWide: Bool {
        #if DEBUG
        return ProcessInfo.processInfo.arguments.contains("-padWide")
        #else
        return false
        #endif
    }
    var padPortrait: Bool {
        let s = (UIApplication.shared.connectedScenes.first as? UIWindowScene)?.screen.bounds.size ?? .zero
        return s.height > s.width
    }
    var padTabBar: some View {
        ZStack(alignment: .bottom) {
            HStack(spacing: 0) {
                ForEach(Tab.allCases, id: \.self) { t in
                    if t == .collection { Color.clear.frame(width: 76) } else { tabItem(t) }
                }
            }
            .padding(.horizontal, 4).frame(height: 62).background(GlassCapsule())
            centreButton.offset(y: -18)
        }
    }

    @ViewBuilder var padRoot: some View {
        switch state.tab {
        case .me: SettingsView()
        default:
            NavigationStack(path: $state.path) {
                Group {
                    switch state.tab {
                    case .home: HomeView()
                    case .library: LibraryView()
                    case .collection: CollectionView()
                    default: LibrarySearchView()
                    }
                }
                .safeAreaInset(edge: .bottom) { Color.clear.frame(height: 0) }
                .toolbar(.hidden, for: .navigationBar)
                .navigationDestination(for: Route.self) { r in routeView(r) }
            }
        }
    }
}

/// iPad sidebar: doodle logo, small tab rows, lime crayon selection mark. Not the system sidebar look.
struct PadSidebar: View {
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @ObservedObject private var acct = CoreAccountStore.shared
    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 10) {
                YMBrandMark(size: 32)
                Text("译梦").font(.system(size: 20, weight: .bold))
            }
            .padding(.leading, 8).padding(.bottom, 18).padding(.top, 8)
            ForEach(Tab.allCases, id: \.self) { t in
                let on = state.tab == t
                Button { withAnimation(.snappy) { if state.tab == t { state.path = [] }; state.tab = t } } label: {
                    HStack(spacing: 12) {
                        ZStack {
                            if on { Crayon(shape: WobblyCircle(), color: Theme.accent(scheme), width: 1.6).frame(width: 34, height: 34) }
                            YMIcon(glyph: t.glyph, size: 18, weight: on ? 2.0 : 1.6, active: on)
                        }.frame(width: 36, height: 36)
                        Text(t.rawValue).font(.system(size: 15, weight: on ? .semibold : .regular))
                            .foregroundStyle(on ? Theme.fg(scheme) : Theme.fg(scheme).opacity(0.75))
                        Spacer()
                        if t == .me, acct.unread > 0 { UnreadBadge(n: acct.unread) }
                    }
                    .padding(.horizontal, 8).frame(height: 46)
                    .background(RoundedRectangle(cornerRadius: 14, style: .continuous).fill(on ? Theme.fill(scheme) : .clear))
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("sidebar.\(t.rawValue)")
            }
            Spacer()
            Crayon(shape: WaveShape(), color: Theme.fg(scheme).opacity(0.25), width: 1.4).frame(width: 90, height: 10).padding(.leading, 10).padding(.bottom, 20)
        }
        .padding(.horizontal, 14)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .foregroundStyle(Theme.fg(scheme))
        .background(Theme.base(scheme).overlay(alignment: .trailing) { Rectangle().fill(Theme.divider(scheme)).frame(width: 0.5) }.ignoresSafeArea())
    }
}

/// Hand-drawn circle (two jittered passes) for selection marks and the logo.
struct WobblyCircle: Shape {
    var seed: UInt64 = 3
    func path(in r: CGRect) -> Path {
        var rng = SeededRNG(seed); var p = Path()
        let c = CGPoint(x: r.midX, y: r.midY), R = min(r.width, r.height) / 2
        for pass in 0..<2 {
            let n = 14
            for i in 0...n {
                let a = Double(i) / Double(n) * 2 * .pi + (pass == 0 ? 0 : 0.3)
                let rr = R * CGFloat(1 + rng.d(-0.05, 0.05))
                let pt = CGPoint(x: c.x + CGFloat(cos(a)) * rr, y: c.y + CGFloat(sin(a)) * rr)
                i == 0 ? p.move(to: pt) : p.addLine(to: pt)
            }
        }
        return p
    }
}

/// The iPhone player's transport (play mode · prev · play/pause · next · rate) in its quiet solid capsule.
struct PlayerTransport: View {
    @EnvironmentObject var state: AppState
    @ObservedObject private var player = PlayerStore.shared
    var big = false
    var body: some View {
        HStack {
            Button { PlayerStore.shared.cyclePlayMode() } label: {
                YMIcon(glyph: { switch player.playMode { case .loop: YMGlyph.repeat; case .one: .repeatOne; case .shuffle: .shuffle; case .order: .order } }(), size: big ? 18 : 16, weight: 1.7)
                    .font(.system(size: 17)).opacity(0.8)
            }
            Spacer()
            Button { state.skip(-1) } label: { YMIcon(glyph: .previous, size: big ? 26 : 24, weight: 1.9, filled: true) }
            Spacer()
            Button { state.setPlaying(!(player.hasRealPlayback ? player.isPlaying : state.isPlaying)) } label: {
                YMIcon(glyph: (player.hasRealPlayback ? player.isPlaying : state.isPlaying) ? .pause : .play, size: big ? 24 : 22, weight: 1.9, filled: true, color: Color(white: 0.07))
                    .frame(width: big ? 56 : 48, height: big ? 56 : 48).background(Circle().fill(.white))
            }
            Spacer()
            Button { state.skip(1) } label: { YMIcon(glyph: .next, size: big ? 26 : 24, weight: 1.9, filled: true) }
            Spacer()
            Text(player.rate == 1 ? "1×" : String(format: "%g×", player.rate)).font(.system(size: 13, weight: .medium).monospacedDigit()).opacity(0.7)
                .lineLimit(1).fixedSize().frame(minWidth: 24)
        }
        .buttonStyle(.plain)
        .padding(.horizontal, 22).frame(height: big ? 72 : 64)
        .background(Capsule().fill(Color.black.opacity(0.32)).overlay(Capsule().strokeBorder(Color.white.opacity(0.08), lineWidth: 0.5)))
        .environment(\.colorScheme, .dark)
    }
}

/// Persistent right column: cover, scrubber, controls, then lyrics or up-next.
struct PadPlayerColumn: View {
    @EnvironmentObject var state: AppState
    @ObservedObject private var player = PlayerStore.shared
    @State private var lyrics = true
    var body: some View {
        GeometryReader { g in
            ZStack {
                if player.song == nil {
                    VStack(spacing: 14) {
                        NoteScribble().stroke(.white.opacity(0.8), style: StrokeStyle(lineWidth: 2.4, lineCap: .round)).frame(width: 60, height: 60)
                        Text("还没有在播放").font(.system(size: 17, weight: .semibold))
                        Text("从左边挑一首歌吧").font(.system(size: 13)).opacity(0.7)
                    }.foregroundStyle(.white)
                } else {
                    let side = min(g.size.width - 64, g.size.height * 0.34, 380)
                    VStack(spacing: 14) {
                        CoverImage(url: state.remoteCoverURL ?? player.coverURL, points: side)
                            .frame(width: side, height: side).clipShape(RoundedRectangle(cornerRadius: 18, style: .continuous))
                            .anchorPreference(key: PadCoverAnchor.self, value: .bounds) { $0 }
                            .shadow(color: .black.opacity(0.35), radius: 24, y: 16)
                            .modifier(CoverSwipe())
                            .padding(.top, 20)
                        VStack(spacing: 3) {
                            Text(state.track.title).font(.system(size: 20, weight: .bold)).lineLimit(1).truncationMode(.middle)
                            Text(state.track.artist).font(.system(size: 15)).opacity(0.75).lineLimit(1)
                            if let s = player.song { QualityBadge(song: s).padding(.top, 6) }
                        }.padding(.horizontal, 24)
                        TimelineView(.periodic(from: .now, by: 0.5)) { _ in ScrubBar(elapsed: player.elapsed, total: max(player.duration, 1)) }
                            .padding(.horizontal, 32)
                        PlayerTransport().padding(.horizontal, 24)
                        HStack(spacing: 22) {
                            pill(lyrics ? "歌词" : "接下来", "quote.bubble", on: lyrics) { lyrics.toggle() }
                            pill("全屏", "arrow.up.left.and.arrow.down.right") { state.padFull = true }
                        }
                        Group {
                            if lyrics {
                                TimelineView(.periodic(from: .now, by: 0.25)) { _ in LiveLyricsView(time: player.elapsed, accent: Theme.lime) }
                                    .clipShape(RoundedRectangle(cornerRadius: 20, style: .continuous))
                            } else { upNext }
                        }
                        .padding(.horizontal, 16).padding(.bottom, 16)
                    }
                    .foregroundStyle(.white)
                }
            }
        }
        // Doodles are laid out around the cover's measured bounds (same space as the layer), so the frame hugs the art.
        .backgroundPreferenceValue(PadCoverAnchor.self) { a in
            GeometryReader { g in
                ZStack {
                    DreamField(track: state.track)
                    if let a, player.song != nil {
                        DoodleLayer(track: state.track, slot: CoverSlot(rect: g[a], radius: 18),
                                    ambience: state.ambience == .aurora ? .pure : state.ambience, intensity: state.intensity * 0.7, beat: state.beatSync)
                    }
                }.allowsHitTesting(false)
            }.ignoresSafeArea()
        }
        .toolbar(.hidden, for: .navigationBar)
        .fullScreenCover(isPresented: $state.padFull) { PadFullPlayer().environmentObject(state) }
    }
    private var controls: some View {
        HStack(spacing: 34) {
            Button { state.skip(-1) } label: { YMIcon(glyph: .previous, size: 24, weight: 1.9, filled: true) }
            Button { state.setPlaying(!player.isPlaying) } label: {
                YMIcon(glyph: player.isPlaying ? .pause : .play, size: 22, weight: 1.9, filled: true, color: Color(white: 0.07))
                    .frame(width: 54, height: 54).background(Circle().fill(.white))
            }
            Button { state.skip(1) } label: { YMIcon(glyph: .next, size: 24, weight: 1.9, filled: true) }
        }.buttonStyle(.plain)
    }
    private func pill(_ t: String, _ icon: String, on: Bool = false, _ a: @escaping () -> Void) -> some View {
        Button(action: a) { Label(t, systemImage: icon).font(.system(size: 13, weight: .semibold)).padding(.horizontal, 14).frame(height: 32)
            .background(Capsule().fill(Color.black.opacity(on ? 0.4 : 0.24)).overlay(Capsule().strokeBorder(Color.white.opacity(on ? 0.2 : 0.08), lineWidth: 0.5))) }.buttonStyle(.plain)
    }
    private var upNext: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 0) {
                let up = Array(player.queue.dropFirst(player.index + 1).prefix(50).enumerated())
                if up.isEmpty { Text("播放队列里没有更多歌曲").opacity(0.7).padding() }
                ForEach(up, id: \.offset) { i, s in
                    Button { PlayerStore.shared.jump(toUpcoming: i) } label: {
                        HStack { VStack(alignment: .leading) { Text(s.title).lineLimit(1); Text(s.artist).font(.system(size: 12)).opacity(0.65) }; Spacer(); Text(s.durationLabel).font(.system(size: 12)).opacity(0.6) }
                            .padding(.vertical, 8).padding(.horizontal, 12).contentShape(Rectangle())
                    }.buttonStyle(.plain).songMenu(s)
                }
            }
        }
        .background(RoundedRectangle(cornerRadius: 20, style: .continuous).fill(Color.black.opacity(0.32)))
    }
}

/// iPad full screen: landscape = turntable, portrait = the regular player.
struct PadFullPlayer: View {
    @EnvironmentObject var state: AppState
    @Environment(\.dismiss) private var dismiss
    var body: some View {
        GeometryReader { g in
            VinylNowPlaying(onClose: { dismiss() })
        }
        .onDisappear { state.padFull = false }
    }
}

// MARK: - Turntable

struct VinylNowPlaying: View {
    @EnvironmentObject var state: AppState
    @ObservedObject private var player = PlayerStore.shared
    var onClose: () -> Void = {}
    @State private var angle: Double = 0
    @State private var lastTick = Date()
    var body: some View {
        GeometryReader { g in
            let land = g.size.width > g.size.height
            let disc = land ? min(g.size.height * 0.78, g.size.width * 0.46) : min(g.size.width * 0.62, g.size.height * 0.4)
            let layout = land ? AnyLayout(HStackLayout(spacing: 48)) : AnyLayout(VStackLayout(spacing: 36))
            ZStack {
                DreamField(track: state.track).ignoresSafeArea()
                layout {
                    turntable(disc)
                    VStack(alignment: .leading, spacing: 10) {
                        Text(state.track.title).font(.system(size: 30, weight: .bold)).lineLimit(1).truncationMode(.middle)
                        Text(state.track.artist).font(.system(size: 19)).opacity(0.75)
                        if let s = player.song { QualityBadge(song: s).padding(.top, 4) }
                        TimelineView(.periodic(from: .now, by: 0.5)) { _ in ScrubBar(elapsed: player.elapsed, total: max(player.duration, 1)) }
                            .padding(.top, 18)
                        PlayerTransport(big: true).padding(.top, 10)
                        TimelineView(.periodic(from: .now, by: 0.25)) { _ in LiveLyricsView(time: player.elapsed, accent: Theme.lime) }
                            .frame(height: g.size.height * (land ? 0.32 : 0.2)).clipShape(RoundedRectangle(cornerRadius: 18, style: .continuous)).padding(.top, 8)
                    }
                    .frame(maxWidth: 460)
                }
                .padding(40)
                VStack { HStack { Button(action: onClose) { CircleIconButton(icon: "chevron.down", dark: true) }.buttonStyle(.plain).accessibilityIdentifier("chevron.down"); Spacer() }; Spacer() }.padding(24)
            }
            .foregroundStyle(.white)
        }
    }
    private func turntable(_ d: CGFloat) -> some View {
        ZStack(alignment: .topTrailing) {
            // Plinth
            RoundedRectangle(cornerRadius: 28, style: .continuous).fill(Color.black.opacity(0.35))
                .overlay(WobblyFrame(seed: 11).stroke(.white.opacity(0.35), lineWidth: 1.4))
                .frame(width: d * 1.18, height: d * 1.08)
            // Platter + record (rotates only while playing — it is the "doodle", not a control)
            TimelineView(.animation(minimumInterval: 1 / 30, paused: !player.isPlaying)) { tl in
                let a = spin(tl.date)
                ZStack {
                    Circle().fill(Color(white: 0.06))
                    ForEach(0..<9, id: \.self) { i in
                        Circle().stroke(Color.white.opacity(i % 3 == 0 ? 0.10 : 0.05), lineWidth: 1).padding(d * (0.08 + CGFloat(i) * 0.035))
                    }
                    CoverImage(url: state.remoteCoverURL ?? player.coverURL, points: d * 0.4)
                        .frame(width: d * 0.4, height: d * 0.4).clipShape(Circle())
                    Circle().fill(Color(white: 0.85)).frame(width: d * 0.035)
                    // sheen
                    Circle().trim(from: 0.05, to: 0.18).stroke(Color.white.opacity(0.12), style: StrokeStyle(lineWidth: d * 0.2)).padding(d * 0.16)
                }
                .frame(width: d, height: d)
                .rotationEffect(.degrees(a))
            }
            .frame(width: d * 1.18, height: d * 1.08, alignment: .leading).padding(.leading, d * 0.05)
            // Tonearm (hand-drawn), swings onto the record when playing
            Tonearm().stroke(.white.opacity(0.9), style: StrokeStyle(lineWidth: 3, lineCap: .round, lineJoin: .round))
                .frame(width: d * 0.22, height: d * 0.78)
                .rotationEffect(.degrees(player.isPlaying ? 18 : 0), anchor: UnitPoint(x: 0.7, y: 0.06))
                .animation(.spring(response: 0.9, dampingFraction: 0.8), value: player.isPlaying)
                .padding(.top, d * 0.06).padding(.trailing, d * 0.03)
        }
    }
    /// 33⅓ rpm, continuous across pauses.
    private func spin(_ now: Date) -> Double {
        let dt = now.timeIntervalSince(lastTick)
        DispatchQueue.main.async { if player.isPlaying { angle = (angle + dt * 200).truncatingRemainder(dividingBy: 360) }; lastTick = now }
        return angle
    }
}

struct Tonearm: Shape {
    func path(in r: CGRect) -> Path {
        var p = Path()
        let pivot = CGPoint(x: r.width * 0.7, y: r.height * 0.06)
        p.addEllipse(in: CGRect(x: pivot.x - 9, y: pivot.y - 9, width: 18, height: 18))
        p.move(to: pivot)
        p.addQuadCurve(to: CGPoint(x: r.width * 0.55, y: r.height * 0.82), control: CGPoint(x: r.width * 0.82, y: r.height * 0.5))
        p.addLine(to: CGPoint(x: r.width * 0.32, y: r.height * 0.95))
        p.addRect(CGRect(x: r.width * 0.2, y: r.height * 0.92, width: r.width * 0.2, height: r.height * 0.07))
        return p
    }
}


#if DEBUG
/// Screenshot hooks: -orient landscape|portrait, -padFull, -debugPlay (plays the first daily — debug only).
enum PadDebug {
    @MainActor static func run(_ state: AppState) async {
        let a = ProcessInfo.processInfo.arguments
        if let i = a.firstIndex(of: "-orient"), i + 1 < a.count, let sc = UIApplication.shared.connectedScenes.first as? UIWindowScene {
            try? await Task.sleep(for: .seconds(1.5))
            sc.requestGeometryUpdate(.iOS(interfaceOrientations: a[i + 1] == "landscape" ? .landscapeRight : .portrait)) { e in NSLog("[ym] orient: %@", e.localizedDescription) }
        }
        if a.contains("-debugPlay") {
            try? await Task.sleep(for: .seconds(4))
            if PlatformStore.shared.dailies.isEmpty { await PlatformStore.shared.refresh() }
            if let d = PlatformStore.shared.dailies.first { PlayerStore.shared.play(songs: d.tracks.filter { !$0.isUnplayable }) }
            if a.contains("-thenPause") { try? await Task.sleep(for: .seconds(3)); if PlayerStore.shared.isPlaying { PlayerStore.shared.toggle() } }
        }
        if a.contains("-gaplessTest") {
            // Seek near the end; log the hand-off so the gap (wall clock between last tick and the next song's first tick) is visible.
            try? await Task.sleep(for: .seconds(6))
            let p = PlayerStore.shared, first = p.song?.id ?? ""
            NSLog("[ym] GAPLESS start id=%@ dur=%.1f cached=%d", first, p.duration, OfflineCache.shared.isCached(first) ? 1 : 0)
            if p.duration > 10 { p.seek(to: p.duration - 4) }
            for _ in 0..<40 {
                try? await Task.sleep(for: .milliseconds(250))
                if p.song?.id != first { NSLog("[ym] GAPLESS switched to=%@ elapsed=%.2f playing=%d", p.song?.id ?? "-", p.elapsed, p.isPlaying ? 1 : 0); break }
            }
        }
        if a.contains("-soundSheet") { try? await Task.sleep(for: .seconds(2)); SheetCenter.shared.sound = true }
        if a.contains("-padFull") { try? await Task.sleep(for: .seconds(3)); state.padFull = true }
    }
}
#endif

struct PadCoverAnchor: PreferenceKey {
    static var defaultValue: Anchor<CGRect>? = nil
    static func reduce(value: inout Anchor<CGRect>?, nextValue: () -> Anchor<CGRect>?) { value = value ?? nextValue() }
}
