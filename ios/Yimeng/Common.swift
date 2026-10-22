import SwiftUI

struct SectionHeader: View {
    let title: String; var more = true
    @Environment(\.colorScheme) var scheme
    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            Text(title).font(.system(size: 20, weight: .bold))
            Spacer()
            if more { Text("全部").font(.system(size: 15)).foregroundStyle(Theme.secondary(scheme)) }
        }
        .padding(.horizontal, Theme.margin).padding(.top, 32).padding(.bottom, 12)
    }
}

struct AlbumRow: View {
    let albums: [Album]; var size: CGFloat = 148
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @Environment(\.heroNS) var ns
    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 16) {
                ForEach(albums) { a in
                    VStack(alignment: .leading, spacing: 2) {
                        Image(a.cover).resizable().scaledToFill().frame(width: size, height: size)
                            .clipShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                            .zoomSource(a.id, ns)
                        Text(a.title).font(.system(size: 14, weight: .medium)).lineLimit(1).padding(.top, 6)
                        Text(a.artist).font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme)).lineLimit(1)
                    }
                    .frame(width: size).scrollReveal(.horizontal).contentShape(Rectangle()).onTapGesture { state.path.append(.album(a)) }
                }
            }.padding(.horizontal, Theme.margin)
        }
    }
}

struct TrackRow: View {
    let index: Int; let title: String; var subtitle: String? = nil; var trailing = ""; var art: String? = nil; var playing = false
    @Environment(\.colorScheme) var scheme
    var body: some View {
        HStack(spacing: 16) {
            if let art {
                Image(art).resizable().scaledToFill().frame(width: 48, height: 48).clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
            } else {
                Group { if playing { Image(systemName: "waveform") } else { Text("\(index)") } }
                    .font(.system(size: 15)).foregroundStyle(playing ? Theme.accent(scheme) : Theme.secondary(scheme)).frame(width: 24)
            }
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text(title).font(.system(size: 16)).foregroundStyle(playing ? Theme.accent(scheme) : Theme.fg(scheme))
                    if let subtitle { Text(subtitle).font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme)) }
                }
                Spacer()
                Text(trailing).font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme))
            }
            .frame(maxHeight: .infinity)
            .overlay(alignment: .bottom) { Rectangle().fill(Theme.divider(scheme)).frame(height: 0.5) }
        }
        .frame(height: art == nil ? 56 : 64).padding(.horizontal, Theme.margin).scrollReveal()
    }
}
