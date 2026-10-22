import SwiftUI
import AVKit

/// ⋯ secondary sheet (QQ音乐-style): header, round shortcuts, list rows, volume at the bottom. Dark, solid, quiet.
struct NowPlayingMoreSheet: View {
    let song: NDSong
    var onAmbience: () -> Void
    @EnvironmentObject var state: AppState
    @ObservedObject private var player = PlayerStore.shared
    @ObservedObject private var fx = FXStore.shared
    @Environment(\.dismiss) var dismiss
    @State private var askSleep = false
    @State private var appeared = false
    private let rates: [Float] = [0.75, 1, 1.25, 1.5, 2]

    var body: some View {
        ScrollView(showsIndicators: false) {
            VStack(alignment: .leading, spacing: 0) {
                header.padding(.top, 22)
                shortcuts.padding(.top, 20).padding(.bottom, 8)
                    .offset(y: appeared ? 0 : 14).opacity(appeared ? 1 : 0)
                divider
                Group {
                    row("square.stack", "专辑", song.album ?? "—", chevron: true) { goAlbum() }
                    row("music.mic", "歌手", song.artist, chevron: true) { goArtist() }
                    row("waveform", "音质", qualityText) {}
                    rateRow
                    row("slider.horizontal.3", "音效设置", fx.s.on ? "已开启 · \(fx.s.preset)" : "关闭", chevron: true) {
                        dismiss(); DispatchQueue.main.asyncAfter(deadline: .now() + 0.35) { SheetCenter.shared.sound = true }
                    }
                }
                .offset(y: appeared ? 0 : 22).opacity(appeared ? 1 : 0)
                divider.padding(.top, 4)
                VStack(alignment: .leading, spacing: 10) {
                    Text("调节音量").font(.system(size: 13)).opacity(0.55)
                    HStack(spacing: 10) {
                        Image(systemName: "speaker.fill").font(.system(size: 11)).opacity(0.6)
                        VolumeSlider()
                        Image(systemName: "speaker.wave.3.fill").font(.system(size: 11)).opacity(0.6)
                    }
                }
                .padding(.top, 14).padding(.bottom, 30)
            }
            .padding(.horizontal, 22)
        }
        .foregroundStyle(.white)
        .background(Color(white: 0.085).ignoresSafeArea())
        .environment(\.colorScheme, .dark)
        .onAppear { withAnimation(.spring(response: 0.5, dampingFraction: 0.82).delay(0.05)) { appeared = true } }
        .confirmationDialog("定时关闭", isPresented: $askSleep, titleVisibility: .visible) {
            ForEach([15, 30, 45, 60, 90], id: \.self) { m in Button("\(m) 分钟后") { player.setSleep(minutes: m); SheetCenter.shared.say("将在 \(m) 分钟后暂停") } }
            Button("播完本首") { player.setSleep(minutes: 0); SheetCenter.shared.say("本首播完后暂停") }
            if player.sleepAt != nil || player.sleepAfterTrack { Button("关闭定时", role: .destructive) { player.setSleep(minutes: nil) } }
            Button("取消", role: .cancel) {}
        }
    }

    private var header: some View {
        HStack(spacing: 14) {
            CoverImage(url: LibraryStore.shared.coverArtURL(id: song.coverId ?? song.albumId, size: 200), points: 56)
                .frame(width: 56, height: 56).clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
            VStack(alignment: .leading, spacing: 4) {
                Text(song.title).font(.system(size: 16, weight: .semibold)).lineLimit(1).truncationMode(.middle)
                Text(song.artist).font(.system(size: 13)).opacity(0.6).lineLimit(1)
                QualityBadge(song: song)
            }
            Spacer(minLength: 0)
        }
    }

    private var shortcuts: some View {
        HStack(alignment: .top, spacing: 0) {
            round("moon.zzz", sleepLabel, on: player.sleepAt != nil || player.sleepAfterTrack) { askSleep = true }
            round("text.badge.plus", "加到歌单") { dismiss(); DispatchQueue.main.asyncAfter(deadline: .now() + 0.35) { SheetCenter.shared.addToPlaylist = song } }
            ShareLink(item: "\(song.title) - \(song.artist)") { roundLabel("square.and.arrow.up", "分享", on: false) }.buttonStyle(.plain).frame(maxWidth: .infinity)
            round("sparkles", "梦境氛围") { dismiss(); DispatchQueue.main.asyncAfter(deadline: .now() + 0.35) { onAmbience() } }
            ZStack { roundLabel("airplayaudio", "设备投放", on: AudioRoute.shared.portType.contains("AirPlay")); RoutePicker().frame(width: 52, height: 52).opacity(0.02).offset(y: -10) }
                .frame(maxWidth: .infinity)
        }
    }

    private var sleepLabel: String {
        if player.sleepAfterTrack { return "本首后" }
        if let at = player.sleepAt { return "\(max(1, Int(at.timeIntervalSinceNow / 60) + 1)) 分钟" }
        return "定时关闭"
    }

    private func round(_ icon: String, _ t: String, on: Bool = false, action: @escaping () -> Void) -> some View {
        Button(action: action) { roundLabel(icon, t, on: on) }.buttonStyle(.plain).frame(maxWidth: .infinity)
    }
    private func roundLabel(_ icon: String, _ t: String, on: Bool) -> some View {
        VStack(spacing: 7) {
            Image(systemName: icon).font(.system(size: 18))
                .frame(width: 48, height: 48)
                .background(Circle().fill(Color.white.opacity(0.08)))
                .overlay(alignment: .topTrailing) { if on { Circle().fill(Theme.lime).frame(width: 6, height: 6).offset(x: -4, y: 4) } }
            Text(t).font(.system(size: 11)).opacity(0.7).lineLimit(1)
        }
    }

    private var rateRow: some View {
        HStack(spacing: 14) {
            Image(systemName: "gauge.with.dots.needle.50percent").font(.system(size: 15)).opacity(0.7).frame(width: 22)
            Text("倍速").font(.system(size: 15))
            Spacer()
            HStack(spacing: 4) {
                ForEach(rates, id: \.self) { r in
                    let on = abs(player.rate - r) < 0.01
                    Button { withAnimation(.spring(response: 0.3, dampingFraction: 0.8)) { player.setRate(r) } } label: {
                        Text(r == 1 ? "1×" : String(format: "%g×", r)).font(.system(size: 12, weight: on ? .semibold : .regular))
                            .padding(.horizontal, 8).frame(height: 26)
                            .foregroundStyle(on ? Color.black : .white.opacity(0.75))
                            .background(Capsule().fill(on ? Color.white : Color.white.opacity(0.06)))
                    }.buttonStyle(.plain)
                }
            }
        }
        .frame(height: 52)
    }

    private func row(_ icon: String, _ t: String, _ v: String, chevron: Bool = false, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: 14) {
                Image(systemName: icon).font(.system(size: 15)).opacity(0.7).frame(width: 22)
                Text(t).font(.system(size: 15))
                Spacer(minLength: 12)
                Text(v).font(.system(size: 13)).opacity(0.55).lineLimit(1)
                if chevron { Image(systemName: "chevron.right").font(.system(size: 11)).opacity(0.4) }
            }
            .frame(height: 52).contentShape(Rectangle())
        }.buttonStyle(.plain)
    }

    private var divider: some View { Rectangle().fill(Color.white.opacity(0.07)).frame(height: 0.5) }

    private var qualityText: String {
        let f = [song.suffix?.uppercased(), song.bitDepth.map { "\($0)bit" },
                 song.samplingRate.map { String(format: "%gkHz", Double($0) / 1000) },
                 song.bitRate.map { "\($0)kbps" }].compactMap { $0 }.joined(separator: " · ")
        let path = song.willTranscode ? "转码 MP3" : fx.s.on ? (FXTap.unsupported(song) ? "直通（音效不可用）" : "音效处理") : "直通"
        return [f, path].filter { !$0.isEmpty }.joined(separator: " · ")
    }

    private func close(then go: @escaping () -> Void) {
        dismiss()
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) {
            withAnimation(.ui) { state.nowPlaying = false; state.lyricsMode = false }
            state.tab = .library
            go()
        }
    }
    private func goAlbum() {
        guard let id = song.albumId else { return }
        let a = NDAlbum(id: id, name: song.album ?? "", artist: song.artist, coverId: song.coverId, year: nil, songCount: nil)
        close { state.path.append(.ndAlbum(a)) }
    }
    private func goArtist() {
        let name = song.artist.components(separatedBy: CharacterSet(charactersIn: "/&,;")).first?.trimmingCharacters(in: .whitespaces) ?? song.artist
        Task { @MainActor in
            var ar = LibraryCache.shared.artists.first { $0.name == name }
            if ar == nil { ar = (try? await LibraryStore.shared.search(query: name))?.artists.first { $0.name == name } }
            guard let ar else { SheetCenter.shared.say("没找到这位歌手"); return }
            close { state.path.append(.ndArtist(ar)) }
        }
    }
}
