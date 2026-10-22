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
            List {
                ForEach(Tab.allCases, id: \.self) { t in
                    Button { withAnimation(.snappy) { if state.tab == t { state.path = [] }; state.tab = t } } label: {
                        Label(t.rawValue, systemImage: t.icon)
                    }
                    .listRowBackground(state.tab == t ? Theme.lime.opacity(0.25).clipShape(RoundedRectangle(cornerRadius: 10)) : nil)
                    .foregroundStyle(.primary)
                }
            }
            .navigationTitle("译梦")
            .navigationSplitViewColumnWidth(min: 180, ideal: 220, max: 260)
        } content: {
            GeometryReader { g in
                padRoot
                    // Portrait hides the sidebar: keep the floating glass tab bar so every tab stays one tap away.
                    .overlay(alignment: .bottom) { if padPortrait { padTabBar.padding(.horizontal, 16).padding(.bottom, 8) } }
                    .frame(width: g.size.width, height: g.size.height)
            }
            .navigationSplitViewColumnWidth(min: 340, ideal: 460)
        } detail: {
            PadPlayerColumn()
        }
        .navigationSplitViewStyle(.balanced)
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

/// Persistent right column: cover, scrubber, controls, then lyrics or up-next.
struct PadPlayerColumn: View {
    @EnvironmentObject var state: AppState
    @ObservedObject private var player = PlayerStore.shared
    @State private var lyrics = true
    var body: some View {
        GeometryReader { g in
            ZStack {
                DreamField(track: state.track).ignoresSafeArea()
                if player.song == nil {
                    VStack(spacing: 14) {
                        NoteScribble().stroke(.white.opacity(0.8), style: StrokeStyle(lineWidth: 2.4, lineCap: .round)).frame(width: 60, height: 60)
                        Text("还没有在播放").font(.system(size: 17, weight: .semibold))
                        Text("从左边挑一首歌吧").font(.system(size: 13)).opacity(0.7)
                    }.foregroundStyle(.white)
                } else {
                    VStack(spacing: 14) {
                        let side = min(g.size.width - 64, g.size.height * 0.34, 380)
                        CoverImage(url: state.remoteCoverURL ?? player.coverURL, points: side)
                            .frame(width: side, height: side).clipShape(RoundedRectangle(cornerRadius: 18, style: .continuous))
                            .shadow(color: .black.opacity(0.35), radius: 24, y: 16)
                            .modifier(CoverSwipe())
                            .padding(.top, 20)
                        VStack(spacing: 3) {
                            Text(state.track.title).font(.system(size: 20, weight: .bold)).lineLimit(1)
                            Text(state.track.artist).font(.system(size: 15)).opacity(0.75).lineLimit(1)
                        }
                        TimelineView(.periodic(from: .now, by: 0.5)) { _ in ScrubBar(elapsed: player.elapsed, total: max(player.duration, 1)) }
                            .padding(.horizontal, 32)
                        controls
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
        .toolbar(.hidden, for: .navigationBar)
        .fullScreenCover(isPresented: $state.padFull) { PadFullPlayer().environmentObject(state) }
    }
    private var controls: some View {
        HStack(spacing: 34) {
            Button { state.skip(-1) } label: { Image(systemName: "backward.fill").font(.system(size: 24)) }
            Button { state.setPlaying(!player.isPlaying) } label: {
                Image(systemName: player.isPlaying ? "pause.fill" : "play.fill").font(.system(size: 22)).foregroundStyle(Color(white: 0.07))
                    .frame(width: 54, height: 54).background(Circle().fill(.white))
            }
            Button { state.skip(1) } label: { Image(systemName: "forward.fill").font(.system(size: 24)) }
        }.buttonStyle(.plain)
    }
    private func pill(_ t: String, _ icon: String, on: Bool = false, _ a: @escaping () -> Void) -> some View {
        Button(action: a) { Label(t, systemImage: icon).font(.system(size: 13, weight: .semibold)).padding(.horizontal, 14).frame(height: 32)
            .background(Capsule().fill(.white.opacity(on ? 0.26 : 0.12))) }.buttonStyle(.plain)
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
        .background(RoundedRectangle(cornerRadius: 20, style: .continuous).fill(.black.opacity(0.25)))
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
                        Text(state.track.title).font(.system(size: 30, weight: .bold)).lineLimit(2)
                        Text(state.track.artist).font(.system(size: 19)).opacity(0.75)
                        if let s = player.song { QualityBadge(song: s).padding(.top, 4) }
                        TimelineView(.periodic(from: .now, by: 0.5)) { _ in ScrubBar(elapsed: player.elapsed, total: max(player.duration, 1)) }
                            .padding(.top, 18)
                        HStack(spacing: 40) {
                            Button { state.skip(-1) } label: { Image(systemName: "backward.fill").font(.system(size: 26)) }
                            Button { state.setPlaying(!player.isPlaying) } label: {
                                Image(systemName: player.isPlaying ? "pause.fill" : "play.fill").font(.system(size: 24)).foregroundStyle(Color(white: 0.07))
                                    .frame(width: 60, height: 60).background(Circle().fill(.white))
                            }
                            Button { state.skip(1) } label: { Image(systemName: "forward.fill").font(.system(size: 26)) }
                        }.buttonStyle(.plain).padding(.top, 10)
                        TimelineView(.periodic(from: .now, by: 0.25)) { _ in LiveLyricsView(time: player.elapsed, accent: Theme.lime) }
                            .frame(height: g.size.height * (land ? 0.32 : 0.2)).clipShape(RoundedRectangle(cornerRadius: 18, style: .continuous)).padding(.top, 8)
                    }
                    .frame(maxWidth: 460)
                }
                .padding(40)
                VStack { HStack { Button(action: onClose) { Image(systemName: "chevron.down").font(.system(size: 16, weight: .semibold)).frame(width: 44, height: 44).background(Circle().fill(.white.opacity(0.14))) }.buttonStyle(.plain).accessibilityIdentifier("chevron.down"); Spacer() }; Spacer() }.padding(24)
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
