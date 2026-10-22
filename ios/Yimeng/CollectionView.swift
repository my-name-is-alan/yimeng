import SwiftUI

/// 典藏: 3D card stack. Focus card is lifted, tilted flat and glows in its palette colour; others recede in depth.
/// Stickers (NEW / 循环) sit below the card, never on the cover edge.
struct CollectionView: View {
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @State private var focus: Double = 0
    @State private var drag: CGFloat = 0
    let items = DreamMock.collection

    var body: some View {
        let f = max(0, min(Double(items.count - 1), focus - Double(drag / 220)))
        let cur = items[Int(f.rounded())]
        ZStack {
            DreamField(track: cur).animation(.smooth(duration: 0.6), value: cur.id)
            VStack(alignment: .leading, spacing: 0) {
                HStack(alignment: .firstTextBaseline) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text("典藏").font(.system(size: 28, weight: .bold))
                        Text("\(items.count) 张专辑 · 本地无损").font(.system(size: 13)).opacity(0.7)
                    }
                    Spacer()
                    Image(systemName: "arrow.up.arrow.down").font(.system(size: 16, weight: .semibold))
                        .frame(width: 36, height: 36).background(Circle().fill(Color.white.opacity(0.14)))
                }
                .padding(.horizontal, Theme.margin).padding(.top, 16).motionIn(0)

                ZStack {
                    ForEach(Array(items.enumerated()).reversed(), id: \.offset) { i, t in
                        card(t, rel: Double(i) - f)
                    }
                }
                .frame(maxWidth: .infinity).frame(height: 420)
                .contentShape(Rectangle())
                .gesture(DragGesture()
                    .onChanged { drag = -$0.translation.height }
                    .onEnded { v in
                        let target = (focus - Double(-v.predictedEndTranslation.height / 220)).rounded()
                        focus = f; drag = 0
                        withAnimation(.interpolatingSpring(stiffness: 200, damping: 26)) { focus = max(0, min(Double(items.count - 1), target)) }
                    })
                .motionIn(0.08, from: .scale)

                VStack(spacing: 6) {
                    HStack(spacing: 8) { sticker("NEW", rotation: -6); sticker("♻︎ 循环 42 次", rotation: 4) }
                    Text(cur.title).font(.system(size: 22, weight: .bold)).padding(.top, 8)
                    Text("\(cur.artist) · 2024").font(.system(size: 14)).opacity(0.7)
                    HStack(spacing: 12) {
                        Button { state.track = cur; withAnimation(.ui) { state.nowPlaying = true } } label: { Label("播放", systemImage: "play.fill") }
                            .buttonStyle(PrimaryButtonStyle()).environment(\.colorScheme, .dark).frame(width: 120)
                        Button {} label: { Text("打开") }.buttonStyle(SecondaryButtonStyle()).environment(\.colorScheme, .dark).frame(width: 100)
                    }.padding(.top, 12)
                }
                .frame(maxWidth: .infinity).motionIn(0.2)
                Spacer()
            }
            .foregroundStyle(.white)
        }
    }

    func card(_ t: DreamTrack, rel: Double) -> some View {
        let isFocus = abs(rel) < 0.5
        let lift = max(0, 1 - abs(rel))
        return Image(t.cover).resizable().scaledToFill()
            .frame(width: 250, height: 250)
            .clipShape(RoundedRectangle(cornerRadius: 18, style: .continuous))
            .shadow(color: t.blobs[0].opacity(0.75 * lift), radius: 30 * lift)
            .shadow(color: .black.opacity(0.35), radius: 20, y: 14)
            .rotation3DEffect(.degrees(rel > 0 ? -38 : 0), axis: (x: 1, y: 0, z: 0), anchor: .top, perspective: 0.6)
            .scaleEffect(1 - min(0.3, abs(rel) * 0.08) + 0.04 * lift)
            .offset(y: rel > 0 ? -CGFloat(rel) * 34 + 40 : -CGFloat(rel) * 260 + 40)
            .opacity(rel < -1 ? 0 : (rel > 4 ? 0 : 1))
            .zIndex(isFocus ? 100 : -abs(rel))
    }

    func sticker(_ s: String, rotation: Double) -> some View {
        Text(s).font(.system(size: 11, weight: .heavy)).foregroundStyle(Color(white: 0.1))
            .padding(.horizontal, 10).padding(.vertical, 4)
            .background(Capsule().fill(Color.white))
            .rotationEffect(.degrees(rotation))
    }
}
