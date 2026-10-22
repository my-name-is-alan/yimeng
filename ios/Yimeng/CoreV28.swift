import SwiftUI

/// 接力播放: targets come only from GET /sessions (the caller's own devices, D49), never typed ids.
struct HandoffSheet: View {
    @Environment(\.dismiss) private var dismiss
    @State private var items: [CoreSession] = []
    @State private var phase = 0   // 0 loading, 1 ready, 2 error
    @State private var sending: String?

    var body: some View {
        NavigationStack {
            Group {
                if phase == 0 { ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity) }
                else if targets.isEmpty {
                    VStack(spacing: 10) {
                        Image(systemName: "iphone.and.arrow.forward").font(.system(size: 34)).opacity(0.5)
                        Text(phase == 2 ? "无法获取你的设备列表" : "没有其他在线设备").font(.system(size: 16, weight: .semibold))
                        Text("在另一台设备上用同一账号登录译梦后，就能把正在播放的歌接力过去。").font(.system(size: 13)).opacity(0.6).multilineTextAlignment(.center)
                    }.padding(32).frame(maxWidth: .infinity, maxHeight: .infinity)
                } else {
                    List(targets) { s in
                        Button { Task { await send(s) } } label: {
                            HStack {
                                Image(systemName: icon(s.device.deviceType)).frame(width: 28)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(s.device.name ?? "设备").font(.system(size: 16))
                                    Text(s.online == true ? "在线" : "离线").font(.system(size: 12)).opacity(0.55)
                                }
                                Spacer()
                                if sending == s.id { ProgressView() }
                            }
                        }.disabled(sending != nil || s.online == false)
                    }
                }
            }
            .navigationTitle("接力播放").navigationBarTitleDisplayMode(.inline)
            .toolbar { ToolbarItem(placement: .cancellationAction) { Button("关闭") { dismiss() } } }
        }
        .task { await load() }
    }
    private var targets: [CoreSession] { items.filter { $0.current != true } }
    private func icon(_ t: String?) -> String {
        switch t ?? "" { case "ios": "iphone"; case "web": "globe"; case "desktop", "mac": "desktopcomputer"; case "cli": "terminal"; default: "hifispeaker" }
    }
    private func load() async {
        do { items = try await CoreClient.shared.sessions(); phase = 1 } catch { items = []; phase = 2 }
    }
    private func send(_ s: CoreSession) async {
        sending = s.id; defer { sending = nil }
        do {
            try await CoreClient.shared.handoff(to: s.id)
            if PlayerStore.shared.isPlaying { PlayerStore.shared.toggle() }
            SheetCenter.shared.say("已接力到 \(s.device.name ?? "设备")"); dismiss()
        } catch let e as CoreAPIError where e.status == 404 {
            SheetCenter.shared.say("该设备已不存在"); await load()
        } catch { SheetCenter.shared.say("接力失败") }
    }
}

/// Fresh account (D49: history/playlists/favorites are per account): say what will appear, no grey blocks.
struct NewAccountCard: View {
    let name: String
    @Environment(\.colorScheme) private var scheme
    var body: some View {
        HStack(alignment: .top, spacing: 14) {
            ZStack {
                Crayon(shape: ScribbleCircle(), color: Theme.lime).frame(width: 46, height: 46)
                Image(systemName: "music.note").font(.system(size: 18, weight: .semibold))
            }
            VStack(alignment: .leading, spacing: 5) {
                Text(name.isEmpty ? "欢迎来到译梦" : "欢迎，\(name)").font(.system(size: 16, weight: .bold))
                Text("这是一个全新的账号。听几首歌后，最近播放、常听和你的歌单会出现在这里；它们只属于这个账号。")
                    .font(.system(size: 13)).opacity(0.65).fixedSize(horizontal: false, vertical: true)
            }
            Spacer(minLength: 0)
        }
        .padding(16)
        .background(RoundedRectangle(cornerRadius: 18, style: .continuous).fill(Theme.fg(scheme).opacity(0.05)))
    }
}
