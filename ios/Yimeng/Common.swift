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
    /// 译梦 Core: source badge, 试听 capsule, 40% + toast when unplayable.
    var song: NDSong? = nil
    let index: Int; let title: String; var subtitle: String? = nil; var trailing = ""; var art: String? = nil; var playing = false
    var accessory: AnyView? = nil
    /// Flat song lists (曲库 / 收藏 / 搜索): small cover instead of the index. Album tracklists keep track numbers.
    var showCover = false; var coverURL: URL? = nil
    var onTap: (() -> Void)? = nil
    @Environment(\.colorScheme) var scheme
    var body: some View {
        HStack(spacing: 16) {
            if showCover {
                RemoteCover(url: coverURL, size: 44, corner: 7)
                    .overlay {
                        if playing {
                            RoundedRectangle(cornerRadius: 7, style: .continuous).fill(.black.opacity(0.35))
                            YMIcon(glyph: .recognize, size: 15, weight: 1.8, color: .white)
                        }
                    }
            } else if let art {
                Image(art).resizable().scaledToFill().frame(width: 48, height: 48).clipShape(RoundedRectangle(cornerRadius: 8, style: .continuous))
            } else {
                Group { if playing { YMIcon(glyph: .recognize, size: 14, weight: 1.7) } else { Text("\(index)") } }
                    .font(.system(size: 15)).foregroundStyle(playing ? Theme.accent(scheme) : Theme.secondary(scheme)).frame(width: 24)
            }
            HStack {
                VStack(alignment: .leading, spacing: 2) {
                    Text(title).font(.system(size: 16)).foregroundStyle(playing ? Theme.accent(scheme) : Theme.fg(scheme)).lineLimit(1).truncationMode(.middle)
                    if subtitle != nil || song?.origin != nil {
                        HStack(spacing: 6) {
                            if let subtitle { Text(subtitle).font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme)).lineLimit(1) }
                            SourceBadge(origin: song?.origin)
                            if song?.preview == true { PreviewCapsule() }
                        }
                    }
                }
                Spacer()
                Text(trailing).font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme))
                if let accessory { accessory }
            }
            .frame(maxHeight: .infinity)
            .overlay(alignment: .bottom) { Rectangle().fill(Theme.divider(scheme)).frame(height: 0.5) }
        }
        .frame(height: art == nil && !showCover ? 56 : 64).padding(.horizontal, Theme.margin).scrollReveal()
        .opacity(song?.isUnplayable == true ? 0.4 : 1)
        .contentShape(Rectangle())
        .onTapGesture { if let s = song, s.isUnplayable { SheetCenter.shared.unplayable(s) } else { onTap?() } }
        .modifier(OptionalSongMenu(song: song))
    }
}

struct OptionalSongMenu: ViewModifier {
    let song: NDSong?
    @ViewBuilder func body(content: Content) -> some View { if let song { content.songMenu(song) } else { content } }
}

struct RemoteCover: View {
    let url: URL?
    var size: CGFloat = YMLayout.cover(148)
    var corner: CGFloat = 12
    var body: some View {
        CoverImage(url: url, points: size)
            .frame(width: size, height: size)
            .clipShape(RoundedRectangle(cornerRadius: corner, style: .continuous))
    }
}

struct RemoteAlbumRow: View {
    let albums: [NDAlbum]
    @ObservedObject private var library = LibraryStore.shared
    @EnvironmentObject var state: AppState
    @Environment(\.colorScheme) var scheme
    @Environment(\.heroNS) var ns
    var size: CGFloat = 148
    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 16) {
                ForEach(albums) { a in
                    VStack(alignment: .leading, spacing: 2) {
                        RemoteCover(url: library.coverURL(a), size: size)
                            .zoomSource("nd-\(a.id)", ns)
                        Text(a.name).font(.system(size: 14, weight: .medium)).lineLimit(1).padding(.top, 6)
                        Text(a.artist).font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme)).lineLimit(1)
                    }
                    .frame(width: size)
                    .scrollReveal(.horizontal)
                    .contentShape(Rectangle())
                    .onTapGesture { state.path.append(.ndAlbum(a)) }
                    .albumMenu(a)
                }
            }.padding(.horizontal, Theme.margin)
        }
    }
}
