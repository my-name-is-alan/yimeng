import SwiftUI

// Home banner transition, re-implemented natively after the behaviour of the PlutoFish Telegram mini-app carousel:
// slides are stacked in place (no horizontal slide); the outgoing slide is wiped away by a curved edge whose bulge
// is 0 at both ends (sin(πp)), revealing the incoming slide beneath, which does a slow 1.06→1 Ken Burns zoom
// while its caption rises in.

/// Visible region of the outgoing slide. `progress` 0 = fully visible, 1 = fully wiped.
struct CurtainShape: Shape {
    var progress: Double
    var dir: Int
    var animatableData: Double { get { progress } set { progress = newValue } }

    func path(in r: CGRect) -> Path {
        let w = r.width, h = r.height
        let p = max(0, min(1, progress))
        if p <= 0 { return Path(r) }
        let edge = w * (1 - p)
        let b = w * 0.23 * sin(.pi * p)
        let boundary: CGFloat = dir > 0 ? 0 : w
        func x(_ v: CGFloat) -> CGFloat { dir > 0 ? v : w - v }
        var path = Path()
        path.move(to: CGPoint(x: boundary, y: 0))
        path.addLine(to: CGPoint(x: x(edge - b * 0.35), y: 0))
        path.addCurve(to: CGPoint(x: x(edge), y: h * 0.68),
                      control1: CGPoint(x: x(edge + b * 1.2), y: h * 0.28),
                      control2: CGPoint(x: x(edge + b * 1.2), y: h * 0.46))
        path.addCurve(to: CGPoint(x: x(edge - b * 0.75), y: h),
                      control1: CGPoint(x: x(edge - b * 0.35), y: h * 0.82),
                      control2: CGPoint(x: x(edge - b * 0.65), y: h * 0.94))
        path.addLine(to: CGPoint(x: boundary, y: h))
        path.closeSubpath()
        return path
    }
}

struct BannerState: Equatable {
    var active = 0
    var to: Int? = nil
    var dir = 1
    var prog: Double = 0
    var gen = 0
    /// Continuous position used for the backdrop colour cross-fade.
    var position: Double { Double(active) + (to != nil ? Double(dir) * prog : 0) }
}

struct CurtainCarousel: View {
    let albums: [NDAlbum]
    let width: CGFloat
    @Binding var st: BannerState
    var height: CGFloat = 200
    @EnvironmentObject var state: AppState
    @ObservedObject private var library = LibraryStore.shared
    @State private var gesture: (offset: CGFloat, decided: Bool, horizontal: Bool)?
    @State private var lastInteraction = Date.distantPast

    private var n: Int { albums.count }
    private func wrap(_ i: Int) -> Int { ((i % max(n, 1)) + max(n, 1)) % max(n, 1) }
    /// ease-out cubic, like 1 - (1 - t)^3
    private static func curve(_ d: Double) -> Animation { .timingCurve(0.33, 1, 0.68, 1, duration: d) }

    var body: some View {
        let layers: [Int] = st.to.map { [$0, st.active] } ?? [st.active]
        ZStack {
            Color(red: 0.08, green: 0.1, blue: 0.15)
            if n > 0 {
                ForEach(layers, id: \.self) { i in
                    BannerSlide(album: albums[wrap(i)], width: width, height: height)
                        .clipShape(CurtainShape(progress: i == st.active && st.to != nil ? st.prog : 0, dir: st.dir))
                        .zIndex(i == st.active ? 2 : 1)
                }
            }
        }
        .frame(width: width, height: height)
        .clipShape(RoundedRectangle(cornerRadius: 20, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: 20, style: .continuous).strokeBorder(.white.opacity(0.1), lineWidth: 1))
        .shadow(color: .black.opacity(0.45), radius: 20, y: 14)
        .contentShape(Rectangle())
        .onTapGesture { if st.to == nil, n > 0 { state.path.append(.ndAlbum(albums[wrap(st.active)])) } }
        .highPriorityGesture(drag)
        .task {
            // Autoplay every 6 s (a Timer.publish stored on the struct would be re-created on every parent render).
            while !Task.isCancelled {
                try? await Task.sleep(for: .seconds(6))
                guard n > 1, gesture == nil, st.to == nil, Date().timeIntervalSince(lastInteraction) > 6 else { continue }
                goTo(dir: 1)
            }
        }
        .onChange(of: n) { _, _ in st = BannerState() }
    }

    func goTo(dir: Int) {
        guard n > 1 else { return }
        st.gen += 1
        st.dir = dir; st.to = wrap(st.active + dir); st.prog = 0
        DispatchQueue.main.async { settle(true) }
    }

    func settle(_ commit: Bool) {
        guard st.to != nil else { return }
        let target: Double = commit ? 1 : 0
        let d = 0.26 + abs(target - st.prog) * 0.36
        st.gen += 1
        let g = st.gen
        withAnimation(Self.curve(d)) { st.prog = target } completion: {
            guard st.gen == g else { return }
            var t = Transaction(); t.disablesAnimations = true
            withTransaction(t) {
                if commit, let to = st.to { st.active = wrap(to) }
                st.to = nil; st.prog = 0
            }
        }
    }

    var drag: some Gesture {
        DragGesture(minimumDistance: 8)
            .onChanged { v in
                guard n > 1 else { return }
                if gesture == nil {
                    // Grabbing mid-animation keeps the current wipe instead of jumping to either slide.
                    let off: CGFloat = st.to != nil ? -CGFloat(st.dir) * st.prog * width : 0
                    gesture = (off, false, false)
                    st.gen += 1
                }
                guard var g = gesture else { return }
                if !g.decided {
                    g.decided = true
                    g.horizontal = abs(v.translation.width) > abs(v.translation.height)
                    gesture = g
                }
                guard g.horizontal else { return }
                let disp = v.translation.width + g.offset
                let dir = disp <= 0 ? 1 : -1
                var t = Transaction(); t.disablesAnimations = true
                withTransaction(t) {
                    if st.to == nil || st.dir != dir { st.dir = dir; st.to = wrap(st.active + dir) }
                    st.prog = min(1, Double(abs(disp) / max(width, 1)))
                }
            }
            .onEnded { v in
                let g = gesture
                gesture = nil
                lastInteraction = Date()
                guard let g, g.horizontal, st.to != nil else { if st.to != nil { settle(st.prog >= 0.5) }; return }
                let flick = abs(v.translation.width) > 18 && Double(v.velocity.width) * Double(st.dir) < -450
                settle(st.prog >= 0.24 || flick)
            }
    }
}

/// One stacked slide. Ken Burns + caption rise run once when the slide is first revealed (as "next"),
/// and don't replay when it becomes active, so committing never flickers.
private struct BannerSlide: View {
    let album: NDAlbum
    let width: CGFloat; let height: CGFloat
    @ObservedObject private var library = LibraryStore.shared
    @State private var zoom: CGFloat = 1.06
    @State private var up = false
    private static let ease = Animation.timingCurve(0.22, 1, 0.36, 1, duration: 0.76)

    var body: some View {
        ZStack(alignment: .bottomLeading) {
            CoverImage(url: library.coverURL(album, size: 800))
                .frame(width: width, height: height)
                .scaleEffect(zoom)
                .clipped()
            LinearGradient(stops: [.init(color: .black.opacity(0.22), location: 0), .init(color: .clear, location: 0.32)], startPoint: .top, endPoint: .bottom)
            LinearGradient(stops: [.init(color: .black.opacity(0.72), location: 0), .init(color: .black.opacity(0.18), location: 0.46), .init(color: .clear, location: 0.64)], startPoint: .bottom, endPoint: .top)
            Text(NavidromeClient.core ? "来自曲库" : "为你推荐")
                .font(.system(size: 11, weight: .medium)).tracking(0.2).foregroundStyle(.white)
                .padding(.horizontal, 10).frame(height: 22)
                .background(Capsule().fill(.ultraThinMaterial).environment(\.colorScheme, .dark))
                .overlay(Capsule().strokeBorder(.white.opacity(0.16), lineWidth: 1))
                .padding(12)
                .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
                .opacity(up ? 1 : 0).offset(y: up ? 0 : 10)
            HStack(alignment: .bottom) {
                VStack(alignment: .leading, spacing: 3) {
                    Text(album.name).font(.system(size: 22, weight: .heavy)).tracking(-0.4).lineLimit(2)
                        .shadow(color: .black.opacity(0.4), radius: 5, y: 2)
                    Text([album.artist, album.year.map(String.init) ?? ""].filter { !$0.isEmpty }.joined(separator: " · "))
                        .font(.system(size: 13)).opacity(0.75).lineLimit(1)
                }
                .opacity(up ? 1 : 0).offset(y: up ? 0 : 14)
                Spacer(minLength: 12)
                Button {
                    Task { if let (al, s) = try? await library.fetchAlbum(id: album.id), !s.isEmpty { PlayerStore.shared.playAlbum(al, songs: s) } }
                } label: {
                    Label("播放", systemImage: "play.fill").font(.system(size: 12, weight: .semibold))
                        .foregroundStyle(Color(white: 0.07)).padding(.horizontal, 12).frame(height: 28)
                        .background(Capsule().fill(.white)).shadow(color: .black.opacity(0.25), radius: 7, y: 4)
                }
                .buttonStyle(.plain)
                .opacity(up ? 1 : 0).offset(y: up ? 0 : 14)
                .animation(Self.ease.delay(0.28), value: up)
            }
            .foregroundStyle(.white)
            .padding(.horizontal, 16).padding(.bottom, 16)
        }
        .frame(width: width, height: height)
        .onAppear {
            withAnimation(.timingCurve(0.22, 1, 0.36, 1, duration: 7)) { zoom = 1 }
            withAnimation(Self.ease.delay(0.16)) { up = true }
        }
    }
}

/// Banner dots: 5pt, active stretches to 14pt (200ms).
struct BannerDots: View {
    let count: Int; let active: Int
    var body: some View {
        HStack(spacing: 6) {
            ForEach(0..<count, id: \.self) { i in
                Capsule().fill(i == active ? Color.white : Color.white.opacity(0.44))
                    .frame(width: i == active ? 14 : 5, height: 5)
            }
        }
        .animation(.easeInOut(duration: 0.2), value: active)
    }
}

// MARK: - Cover bloom backdrop (album / artist pages)

/// The cover scaled ~2.8×, blurred heavily into a soft colour field, fading to the page colour.
/// `pull` > 0 when the scroll view is pulled down (stretch), < 0 when scrolled up (parallax).
struct CoverBloom: View {
    let url: URL?
    var height: CGFloat = 560
    var pull: CGFloat = 0
    @Environment(\.colorScheme) var scheme

    var body: some View {
        GeometryReader { g in
            let w = g.size.width
            let stretch = max(0, pull)
            ZStack(alignment: .top) {
                Theme.base(scheme)
                if let url {
                    CoverImage(url: url)
                        .frame(width: w, height: w)
                        .scaleEffect(2.8 + stretch / 400)
                        .blur(radius: 80)
                        .saturation(1.35)
                        .opacity(scheme == .dark ? 0.95 : 0.8)
                        .frame(width: w, height: height + stretch)
                        .clipped()
                }
                LinearGradient(stops: [
                    .init(color: Theme.base(scheme).opacity(scheme == .dark ? 0.1 : 0.05), location: 0),
                    .init(color: Theme.base(scheme).opacity(0.25), location: 0.4),
                    .init(color: Theme.base(scheme).opacity(0.8), location: 0.78),
                    .init(color: Theme.base(scheme), location: 1),
                ], startPoint: .top, endPoint: .bottom)
            }
            .frame(width: w, height: height + stretch)
            .offset(y: pull > 0 ? -pull : -pull * 0.45)
        }
        .frame(height: height)
        .allowsHitTesting(false)
    }
}

/// Reads the scroll content's top offset (0 at rest, > 0 when pulled down).
struct ScrollPullReader: View {
    let space: String
    @Binding var pull: CGFloat
    var body: some View {
        GeometryReader { g in
            Color.clear
                .onChange(of: g.frame(in: .named(space)).minY) { _, y in pull = y }
        }
        .frame(height: 0)
    }
}
