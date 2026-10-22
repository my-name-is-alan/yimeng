import SwiftUI
import Combine

/// 首页 — infinite banner carousel. Position `p` is an unbounded continuous page index; slides are rendered
/// for the visible window (floor(p)-1 … floor(p)+2) using modular indexing, so last→first wraps with no rewind.
struct HomeView: View {
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @State private var page: Double = 0       // settled (animatable) position
    @State private var drag: CGFloat = 0      // live finger translation
    @State private var dragging = false
    @State private var lastInteraction = Date.distantPast
    let timer = Timer.publish(every: 3.5, on: .main, in: .common).autoconnect()
    var images: [String] { Mock.banners.map(\.image) }
    var autoplay: Bool { !ProcessInfo.processInfo.arguments.contains("-static") }

    var body: some View {
        GeometryReader { geo in
            let w = geo.size.width - Theme.margin * 2
            let p = page - Double(drag / w)
            let header = Color.header(images, progress: p, dark: scheme == .dark)
            ScrollView(showsIndicators: false) {
                VStack(alignment: .leading, spacing: 0) {
                    HStack {
                        Text("首页").font(.system(size: 28, weight: .bold)).foregroundStyle(header)
                        Spacer()
                        Image("ym_avatar").resizable().scaledToFill().frame(width: 36, height: 36).clipShape(Circle())
                    }
                    .padding(.horizontal, Theme.margin).padding(.top, 16)
                    .motionIn(0)

                    carousel(width: w, progress: p).padding(.top, 16).motionIn(0.08, from: .scale)

                    HStack(spacing: 6) {
                        ForEach(0..<images.count, id: \.self) { i in
                            let wt = DiffusedBackdrop.weight(i, p, images.count)
                            Capsule().fill(header).frame(width: 6 + 12 * wt, height: 6).opacity(0.4 + 0.6 * wt)
                        }
                    }.frame(maxWidth: .infinity).padding(.top, 12).motionIn(0.16, from: .fade)

                    Group { SectionHeader(title: "最近播放"); AlbumRow(albums: Mock.recent) }.motionIn(stagger(2))
                    Group { SectionHeader(title: "新入库"); AlbumRow(albums: Mock.newIn) }.motionIn(stagger(3))
                    SectionHeader(title: "常听艺人").motionIn(stagger(4))
                    Button { state.path.append(.artist) } label: {
                        HStack(spacing: 12) {
                            Image("ym_artist_coldplay").resizable().scaledToFill().frame(width: 56, height: 56).clipShape(Circle())
                            VStack(alignment: .leading) { Text("Coldplay").font(.system(size: 16, weight: .semibold)); Text("本周 32 次播放").font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme)) }
                            Spacer(); Image(systemName: "chevron.right").foregroundStyle(Theme.secondary(scheme))
                        }.padding(.horizontal, Theme.margin).contentShape(Rectangle())
                    }.buttonStyle(.plain).motionIn(stagger(5)).scrollReveal()
                    Color.clear.frame(height: 170)
                }
            }
            .background(alignment: .top) { DiffusedBackdrop(images: images, progress: p).motionIn(0, from: .fade).ignoresSafeArea() }
        }
        .onAppear { BackdropCache.warm(images) }
        .onReceive(timer) { _ in
            guard autoplay, !dragging, Date().timeIntervalSince(lastInteraction) > 3 else { return }
            withAnimation(.smooth(duration: 0.6)) { page = page.rounded() + 1 }
        }
    }

    func carousel(width w: CGFloat, progress p: Double) -> some View {
        let n = Mock.banners.count
        let base = Int(floor(p))
        return ZStack(alignment: .leading) {
            ForEach((base - 1)...(base + 2), id: \.self) { v in
                let b = Mock.banners[((v % n) + n) % n]
                let rel = Double(v) - p                 // -1 … 2
                ZStack(alignment: .bottomLeading) {
                    Image(b.image).resizable().scaledToFill()
                        .frame(width: w * 1.2, height: 200)
                        .offset(x: -CGFloat(rel) * w * 0.18)   // parallax
                        .frame(width: w, height: 200).clipped()
                    LinearGradient(colors: [.clear, .black.opacity(0.15), .black.opacity(0.75)], startPoint: .top, endPoint: .bottom)
                    HStack(alignment: .bottom) {
                        VStack(alignment: .leading, spacing: 4) {
                            Text(b.kicker).font(.system(size: 12, weight: .semibold)).foregroundStyle(.white.opacity(0.7))
                            Text(b.title).font(.system(size: 20, weight: .bold)).foregroundStyle(.white)
                            Text(b.meta).font(.system(size: 13)).foregroundStyle(.white.opacity(0.7))
                        }
                        Spacer()
                        PlayCircle()
                    }.padding(20)
                }
                .frame(width: w, height: 200)
                .offset(x: CGFloat(rel) * w)
                .onTapGesture { state.path.append(.album(Mock.nightfall)) }
            }
        }
        .frame(width: w, height: 200, alignment: .leading)
        .clipShape(RoundedRectangle(cornerRadius: 20, style: .continuous))
        .contentShape(Rectangle())
        .padding(.horizontal, Theme.margin)
        .highPriorityGesture(DragGesture(minimumDistance: 8)
            .onChanged { v in dragging = true; drag = v.translation.width }
            .onEnded { v in
                let projected = page - Double(v.predictedEndTranslation.width / w)
                let current = page - Double(v.translation.width / w)
                let target = max(page.rounded() - 1, min(page.rounded() + 1, projected.rounded()))
                page = current; drag = 0                                      // hand off without a jump
                withAnimation(.interpolatingSpring(stiffness: 220, damping: 28)) { page = target }
                dragging = false; lastInteraction = Date()
            })
    }
}
