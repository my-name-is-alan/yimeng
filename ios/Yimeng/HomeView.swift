import SwiftUI

struct HomeView: View {
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @State private var page: Double = 0          // continuous carousel position
    @State private var drag: CGFloat = 0
    @State private var dragging = false
    let timer = Timer.publish(every: 3.5, on: .main, in: .common).autoconnect()
    var images: [String] { Mock.banners.map(\.image) }
    var autoplay: Bool { !ProcessInfo.processInfo.arguments.contains("-static") }

    var body: some View {
        GeometryReader { geo in
            let w = geo.size.width - Theme.margin * 2
            let p = max(0, min(Double(images.count - 1), page - Double(drag / w)))
            let header = Color.header(images, progress: p, dark: scheme == .dark)
            ZStack(alignment: .top) {
                DiffusedBackdrop(images: images, progress: p).ignoresSafeArea()
                ScrollView(showsIndicators: false) {
                    VStack(alignment: .leading, spacing: 0) {
                        HStack {
                            Text("首页").font(.system(size: 28, weight: .bold)).foregroundStyle(header)
                            Spacer()
                            Image("ym_avatar").resizable().scaledToFill().frame(width: 36, height: 36).clipShape(Circle())
                        }
                        .padding(.horizontal, Theme.margin).padding(.top, 16)

                        carousel(width: w, progress: p).padding(.top, 16)

                        HStack(spacing: 6) {
                            ForEach(0..<images.count, id: \.self) { i in
                                let wt = max(0, 1 - abs(p - Double(i)))
                                Capsule().fill(header).frame(width: 6 + 12 * wt, height: 6).opacity(0.4 + 0.6 * wt)
                            }
                        }.frame(maxWidth: .infinity).padding(.top, 12)

                        SectionHeader(title: "最近播放")
                        AlbumRow(albums: Mock.recent) { a in state.path = [.album(a)] }
                        SectionHeader(title: "新入库")
                        AlbumRow(albums: Mock.newIn) { a in state.path = [.album(a)] }
                        Color.clear.frame(height: 170)
                    }
                }
            }
            .preferredColorScheme(nil)
        }
        .onReceive(timer) { _ in
            guard autoplay, !dragging else { return }
            withAnimation(.easeInOut(duration: 0.6)) { page = page >= Double(images.count - 1) ? 0 : page + 1 }
        }
    }

    func carousel(width w: CGFloat, progress p: Double) -> some View {
        HStack(spacing: 0) {
            ForEach(Mock.banners) { b in
                ZStack(alignment: .bottomLeading) {
                    Image(b.image).resizable().scaledToFill().frame(width: w, height: 200).clipped()
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
                .onTapGesture { state.path = [.album(Mock.nightfall)] }
            }
        }
        .offset(x: -CGFloat(p) * w)
        .frame(width: w, alignment: .leading)
        .clipShape(RoundedRectangle(cornerRadius: 20, style: .continuous))
        .padding(.horizontal, Theme.margin)
        .gesture(DragGesture()
            .onChanged { v in dragging = true; drag = v.translation.width }
            .onEnded { v in
                let target = (page - Double(v.predictedEndTranslation.width / w)).rounded()
                withAnimation(.easeOut(duration: 0.3)) { page = max(0, min(Double(images.count - 1), target)); drag = 0 }
                dragging = false
            })
    }
}
