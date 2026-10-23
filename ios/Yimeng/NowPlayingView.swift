import SwiftUI

/// v10 正在播放: full-bleed palette field + generative doodle composition (frame / orbit / drip by motif),
/// elapsed/total time, single glass control capsule, ambience sheet, lyrics mode.
struct NowPlayingView: View {
    @EnvironmentObject var state: AppState
    @Environment(\.heroNS) var ns
    @ObservedObject private var player = PlayerStore.shared
    @ObservedObject private var stars = StarStore.shared
    @State private var showAmbience = false
    @State private var showMore = false
    @State private var showFonts = false
    @State private var showSourcesFromLyrics = false
    @State private var start = Date().addingTimeInterval(-72)
    var track: DreamTrack { state.track }

    private func times(_ date: Date) -> (TimeInterval, TimeInterval) {
        let total: TimeInterval = player.hasRealPlayback && player.duration > 0 ? player.duration : track.duration
        if player.hasRealPlayback { return (min(player.elapsed, max(total, 1)), total) }
        return (date.timeIntervalSince(start).truncatingRemainder(dividingBy: max(track.duration, 1)), total)
    }

    var body: some View {
        // Only time-driven pieces live in TimelineViews; chrome (top bar, volume, controls, menus) is outside so it
        // never re-renders on a clock tick and never picks up beat/backdrop modulation.
        GeometryReader { g in
            let slot = track.layout.slot(width: g.size.width)
            ZStack(alignment: .topLeading) {
                // Pause mesh under lyrics so the dim panel does not shimmer/flicker.
                DreamField(track: track, animated: !state.lyricsMode)
                if state.lyricsMode {
                    TimelineView(.periodic(from: .now, by: 0.25)) { tl in
                        if player.song != nil {
                            LiveLyricsView(time: times(tl.date).0, accent: Theme.lime,
                                           onConfigureCore: { showSourcesFromLyrics = true }, onFonts: { showFonts = true })
                        } else {
                            LyricsView(track: track, time: times(tl.date).0.truncatingRemainder(dividingBy: 40))
                        }
                    }.transition(.opacity)
                } else {
                    // Background layers: the only things allowed to breathe with the beat.
                    ZStack(alignment: .topLeading) {
                        if state.ambience == .aurora {
                            TimelineView(.animation(minimumInterval: 1 / 30)) { tl in
                                AuroraBands(track: track, time: tl.date.timeIntervalSinceReferenceDate)
                            }.ignoresSafeArea()
                        }
                        DoodleLayer(track: track, slot: slot, ambience: state.ambience == .aurora ? .pure : state.ambience,
                                    intensity: state.intensity, beat: state.beatSync)
                    }
                    .id(state.ambience)
                    .transition(.opacity.combined(with: .scale(scale: 0.97)))
                    .allowsHitTesting(false)
                    cover(slot)
                    TimelineView(.periodic(from: .now, by: 0.25)) { tl in
                        let (e, t) = times(tl.date)
                        info(slot: slot, width: g.size.width, elapsed: e, total: t)
                    }.motionIn(0.16)
                }
                VStack(spacing: 0) {
                        topBar.motionIn(0.05, from: .top)
                        Spacer()
                        FMControls().padding(.bottom, 10)
                        actionRow.padding(.horizontal, Theme.margin + 6).padding(.bottom, 14).motionIn(0.26)
                    controls.motionIn(0.3)
                }
            }
            .foregroundStyle(.white)
            .animation(.spring(response: 0.55, dampingFraction: 0.85), value: state.ambience)
        }
        .modifier(CollapseSwipe())
        #if DEBUG
        .task { if ProcessInfo.processInfo.arguments.contains("-np-sheet") { try? await Task.sleep(for: .seconds(4)); showAmbience = true } }
        .task { if ProcessInfo.processInfo.arguments.contains("-np-more") { try? await Task.sleep(for: .seconds(5)); showMore = true } }
        #endif
        .sheet(isPresented: $showFonts) { LyricFontPicker().presentationDetents([.medium, .large]).presentationCornerRadius(28) }
        .sheet(isPresented: $showSourcesFromLyrics) { SourcesView(embedded: true) }
        .sheet(isPresented: $showMore) {
            if let s = player.song {
                NowPlayingMoreSheet(song: s) { showAmbience = true }.environmentObject(state)
                    .presentationDetents([.height(620), .large])
                    .presentationBackground(Color(white: 0.085))
                    .presentationCornerRadius(28)
                    .presentationDragIndicator(.visible)
            }
        }
        .sheet(isPresented: $showAmbience) {
            AmbienceSheet().environmentObject(state)
                .presentationDetents([.height(330)])
                .presentationBackground(.ultraThinMaterial)
                .presentationCornerRadius(28)
        }
    }

    @ViewBuilder func cover(_ slot: CoverSlot) -> some View {
        let shape = RoundedRectangle(cornerRadius: min(slot.radius, slot.rect.width / 2), style: .continuous)
        let art = Group {
            if let url = state.remoteCoverURL ?? player.coverURL {
                CoverImage(url: url)
            } else {
                Image(track.cover).resizable().scaledToFill()
            }
        }
        .frame(width: slot.rect.width, height: slot.rect.height)
        .clipShape(shape)
        .shadow(color: .black.opacity(0.35), radius: 35, y: 30)
        Group {
            if let ns { art.matchedGeometryEffect(id: "npcover", in: ns, properties: .frame, anchor: .center, isSource: true) } else { art }
        }
        .contentShape(Rectangle())
        .onTapGesture {
            Haptics.tick()
            withAnimation(.smooth(duration: 0.45)) { state.lyricsMode = true }
        }
        .modifier(CoverSwipe())
        .offset(x: slot.rect.minX, y: slot.rect.minY)
        .motionIn(0.08, from: .scale)
    }

    func info(slot: CoverSlot, width: CGFloat, elapsed: TimeInterval, total: TimeInterval? = nil) -> some View {
        let k = width / 402
        return VStack(alignment: track.layout.centered ? .center : .leading, spacing: 2) {
            Text(track.title).font(.system(size: 22, weight: .bold)).lineLimit(1).truncationMode(.middle)
            Text(track.artist).font(.system(size: 16)).opacity(0.75)
            if let s = player.song {
                QualityBadge(song: s).padding(.top, 8)
                if let f = player.fallbackFrom {
                    Text("来自" + (f == "local" ? "本地曲库" : CoreOrigin.label(f))).font(.system(size: 11)).opacity(0.45).padding(.top, 4)
                }
                BeatTagLine(track: track).padding(.top, 6)
            } else {
                Text("\(track.genre) · \(track.bpm) BPM").font(.system(size: 12)).opacity(0.55).padding(.top, 4)
            }
            TimeBar(elapsed: elapsed, total: total ?? track.duration).padding(.top, 22)
        }
        .frame(width: width - 56, alignment: track.layout.centered ? .center : .leading)
        .multilineTextAlignment(track.layout.centered ? .center : .leading)
        .offset(x: 28, y: track.layout.titleY * k)
    }

    var topBar: some View {
        HStack {
            Button { withAnimation(.ui) { state.nowPlaying = false; state.lyricsMode = false } } label: { roundIcon("chevron.down") }
            Spacer()
            Text("译梦 · 梦境 \(track.motif.rawValue.uppercased())").font(.system(size: 12)).tracking(2).opacity(0.7)
            Spacer()
            if let s = player.song {
                Button { StarStore.shared.toggle(s); Haptics.tap() } label: {
                    Image(systemName: StarStore.shared.isStarred(song: s.id) ? "heart.fill" : "heart")
                        .font(.system(size: 16, weight: .semibold))
                        .foregroundStyle(StarStore.shared.isStarred(song: s.id) ? Color(hex: 0xff5fa8) : .white)
                        .frame(width: 40, height: 40).background(Circle().fill(Color.white.opacity(0.14)))
                }
            } else {
                #if DEBUG
                Menu {
                    ForEach(DreamMock.all) { t in Button(t.title) { withAnimation(.ui) { state.track = t } } }
                } label: { roundIcon("ellipsis") }
                #endif
            }
        }
        .buttonStyle(.plain)
        .padding(.horizontal, Theme.margin).padding(.top, 8)
    }

    func roundIcon(_ s: String) -> some View {
        Image(systemName: s).font(.system(size: 16, weight: .semibold)).frame(width: 40, height: 40)
            .background(Circle().fill(Color.white.opacity(0.14)))
    }

    /// Small quiet action row: 歌词 · 音效 · 加到歌单 · 播放列表 · ⋯ — liquid-glass capsule chrome
    var actionRow: some View {
        HStack {
            act("quote.bubble", on: state.lyricsMode) { withAnimation(.smooth(duration: 0.45)) { state.lyricsMode.toggle() } }
            Spacer()
            act("slider.horizontal.3", on: FXStore.shared.s.on) { SheetCenter.shared.sound = true }
            Spacer()
            act("text.badge.plus") { if let s = player.song { SheetCenter.shared.addToPlaylist = s } }
            Spacer()
            act("list.bullet") { SheetCenter.shared.queue = true }
            Spacer()
            act("ellipsis") { if player.hasRealPlayback { showMore = true } else { showAmbience = true } }
        }
        .buttonStyle(.plain)
        .padding(.horizontal, 12)
        .frame(height: 44)
        .background(GlassCapsule())
    }

    private func act(_ icon: String, on: Bool = false, _ action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: icon).font(.system(size: 17)).opacity(on ? 1 : 0.78)
                .frame(width: 40, height: 34)
                .overlay(alignment: .bottom) { if on { Circle().fill(Theme.lime).frame(width: 4, height: 4).offset(y: 2) } }
                .contentShape(Rectangle())
        }.accessibilityIdentifier(icon)
    }

    var controls: some View {
        PlayerTransport()
        .padding(.horizontal, Theme.margin).padding(.bottom, 8)
    }
}

struct TimeBar: View {
    let elapsed: TimeInterval; let total: TimeInterval
    var body: some View {
        let rem = max(0, total - elapsed)
        HStack(spacing: 10) {
            Text(fmtTime(elapsed)).monospacedDigit()
            GeometryReader { g in
                ZStack(alignment: .leading) {
                    Capsule().fill(Color.white.opacity(0.3))
                    Capsule().fill(Color.white).frame(width: g.size.width * CGFloat(min(1, elapsed / max(total, 1))))
                }
            }.frame(height: 3)
            Text("-" + fmtTime(rem)).monospacedDigit()
        }
        .font(.system(size: 12)).opacity(0.85)
        .environment(\.layoutDirection, .leftToRight)
    }
}

/// 梦境 sheet: 涂鸦 / 星尘 / 极光 / 纯净 tiles (live mini renders of the current track), intensity slider, beat toggle.
struct AmbienceSheet: View {
    @EnvironmentObject var state: AppState
    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            VStack(alignment: .leading, spacing: 2) {
                Text("梦境").font(.system(size: 17, weight: .semibold))
                Text("跟随当前歌曲的封面与节奏生成").font(.system(size: 13)).opacity(0.6)
            }
            HStack(spacing: 12) {
                ForEach(Ambience.allCases) { a in
                    Button { withAnimation(.spring(response: 0.5, dampingFraction: 0.82)) { state.ambience = a } } label: {
                        VStack(spacing: 6) {
                            AmbienceTile(track: state.track, ambience: a)
                                .frame(width: 72, height: 100)
                                .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
                                .overlay(RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Color.primary.opacity(state.ambience == a ? 0.9 : 0), lineWidth: 2))
                            Text(a.rawValue).font(.system(size: 12, weight: state.ambience == a ? .semibold : .medium))
                        }
                        .contentShape(Rectangle())   // tile is non-hit-testable; without this only the text was tappable
                    }.buttonStyle(.plain).frame(maxWidth: .infinity)
                }
            }
            HStack {
                Text("强度").font(.system(size: 14))
                Slider(value: $state.intensity, in: 0...1).tint(.primary)
            }
            Toggle("随节拍律动", isOn: $state.beatSync).font(.system(size: 14)).tint(Theme.lime)
        }
        .padding(.horizontal, 24).padding(.top, 24)
    }
}

/// Mini render of the player composition for a given ambience (in production: ImageRenderer at 0.2× cached per track).
struct AmbienceTile: View {
    let track: DreamTrack; let ambience: Ambience
    var body: some View {
        GeometryReader { g in
            let scale = g.size.width / 402
            ZStack(alignment: .topLeading) {
                DreamField(track: track, animated: false)
                let slot = track.layout.slot(width: 402)
                ZStack(alignment: .topLeading) {
                    if ambience == .aurora { AuroraBands(track: track, time: 0) }
                    if ambience == .doodle || ambience == .stardust {
                        DoodleLayer(track: track, slot: slot, ambience: ambience, intensity: 0.7, beat: false)
                    }
                    Image(track.cover).resizable().scaledToFill().frame(width: slot.rect.width, height: slot.rect.height)
                        .clipShape(RoundedRectangle(cornerRadius: min(slot.radius, slot.rect.width / 2)))
                        .offset(x: slot.rect.minX, y: slot.rect.minY)
                }
                .frame(width: 402, height: 874, alignment: .topLeading)
                .scaleEffect(scale, anchor: .topLeading)
            }
            .frame(width: g.size.width, height: g.size.height, alignment: .topLeading)
            .clipped()
        }
        .allowsHitTesting(false)
    }
}

/// v10 lyrics: dark desaturated scrim, narrow fade (only ±2+ blur), marker highlight + squiggle on active line,
/// translation under every line, inline heart.
struct LyricsView: View {
    let track: DreamTrack
    let time: Double
    @State private var liked: Set<Int> = [2]
    var body: some View {
        let lines = track.lyrics
        let idx = lines.lastIndex { $0.t <= time } ?? 0
        ZStack {
            if lines.isEmpty {
                Color(red: 12 / 255, green: 10 / 255, blue: 14 / 255).opacity(0.52).ignoresSafeArea()
                Text("暂无歌词").font(.system(size: 17, weight: .semibold)).opacity(0.7)
            } else {
            Color(red: 12 / 255, green: 10 / 255, blue: 14 / 255).opacity(0.52).ignoresSafeArea()
            GeometryReader { g in
                VStack(alignment: .leading, spacing: 22) {
                    ForEach(Array(lines.enumerated()), id: \.offset) { i, l in
                        line(l, i: i, idx: idx, accent: track.blobs[1])
                            .blurIn(stagger(i, base: 0.1, step: 0.06))
                    }
                }
                .padding(.horizontal, 28)
                .offset(y: g.size.height * 0.36 - CGFloat(idx) * 62)
                .animation(.smooth(duration: 0.6), value: idx)
                .frame(width: g.size.width, height: g.size.height, alignment: .topLeading)
                .clipped()
                .mask(LinearGradient(stops: [.init(color: .clear, location: 0.08), .init(color: .black, location: 0.2),
                                             .init(color: .black, location: 0.8), .init(color: .clear, location: 0.9)], startPoint: .top, endPoint: .bottom))
            }
            }
        }
        .saturation(0.55)
    }

    @ViewBuilder func line(_ l: LyricLine, i: Int, idx: Int, accent: Color) -> some View {
        let active = i == idx
        let past = i < idx
        let d = abs(i - idx)
        VStack(alignment: .leading, spacing: 3) {
            HStack(spacing: 8) {
                Text(l.text).font(.system(size: active ? 24 : 21, weight: .bold))
                    .foregroundStyle((past || active) ? accent.opacity(past ? 0.9 : 1) : Color.white.opacity(0.4))
                    .background(alignment: .leading) {
                        if active {
                            ZStack(alignment: .bottomLeading) {
                                RoundedRectangle(cornerRadius: 4).fill(accent.opacity(0.32)).padding(.vertical, 4).padding(.horizontal, -4)
                                SquiggleLine().stroke(Color.white.opacity(0.85), lineWidth: 2).frame(height: 6).offset(y: 8)
                            }
                        }
                    }
                if active {
                    Button { if liked.contains(i) { liked.remove(i) } else { liked.insert(i) } } label: {
                        Image(systemName: liked.contains(i) ? "heart.fill" : "heart").font(.system(size: 15)).foregroundStyle(accent)
                    }.buttonStyle(.plain)
                }
            }
            if UserDefaults.standard.object(forKey: "ym.lyricTrans") as? Bool ?? true {
                Text(l.en).font(.system(size: 13)).opacity(0.6)
            }
        }
        .opacity(past || active ? 1 : (d == 1 ? 0.7 : 0.45))
        .blur(radius: d >= 3 ? 2.5 : (d == 2 ? 1 : 0))
        .animation(.smooth(duration: 0.5), value: idx)
    }
}

struct SquiggleLine: Shape {
    func path(in r: CGRect) -> Path {
        var p = Path(); p.move(to: CGPoint(x: r.minX, y: r.midY)); var x = r.minX; var up = true
        while x < r.maxX { let nx = min(r.maxX, x + 10); p.addQuadCurve(to: CGPoint(x: nx, y: r.midY), control: CGPoint(x: (x + nx) / 2, y: up ? r.minY : r.maxY)); x = nx; up.toggle() }
        return p
    }
}

/// Kept from v6 for reference/AGSL parity (not used by the v10 player).
struct ParticleField: View {
    let time: Double
    var body: some View { StardustHalo(track: DreamMock.zig, slot: DreamLayout.orbit.slot(width: 390), time: time, intensity: 0.8) }
}
