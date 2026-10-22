import SwiftUI
import AppKit

@main
struct YimengMacApp: App {
    @AppStorage("ym.appearance") private var appearance = 2
    init() {
        if ProcessInfo.processInfo.arguments.contains("-shotMac") {
            DispatchQueue.main.async { MacShot.capture() }
        }
    }
    var body: some Scene {
        WindowGroup {
            MacRoot()
                .preferredColorScheme(appearance == 1 ? .light : appearance == 2 ? .dark : nil)
                .frame(minWidth: 980, minHeight: 640)
        }
        .windowStyle(.automatic)
        .defaultSize(width: 1280, height: 800)
        .commands {
            CommandGroup(replacing: .appInfo) {
                Button("关于译梦") {
                    NSApp.orderFrontStandardAboutPanel(options: [
                        .applicationName: "译梦",
                        .credits: NSAttributedString(string: "梦中的涂鸦 · 梦境环")
                    ])
                }
            }
        }
    }
}

enum MacTab: String, CaseIterable, Identifiable {
    case home = "首页", library = "曲库", collection = "典藏", search = "搜索", me = "我的"
    var id: String { rawValue }
    var glyph: YMGlyph {
        switch self {
        case .home: .home
        case .library: .library
        case .collection: .collection
        case .search: .search
        case .me: .me
        }
    }
}

struct MacRoot: View {
    @State private var tab: MacTab = .home
    @State private var playing = false
    @Environment(\.colorScheme) private var scheme
    var body: some View {
        NavigationSplitView {
            MacSidebar(tab: $tab)
                .navigationSplitViewColumnWidth(min: 200, ideal: 220, max: 260)
        } content: {
            MacContent(tab: tab)
                .navigationSplitViewColumnWidth(min: 380, ideal: 560)
        } detail: {
            MacPlayerColumn(playing: $playing)
                .navigationSplitViewColumnWidth(min: 320, ideal: 400)
        }
        .navigationSplitViewStyle(.balanced)
        .background(Theme.base(scheme).ignoresSafeArea())
        .onAppear { NSApp.mainWindow?.title = "译梦" }
    }
}

struct MacSidebar: View {
    @Binding var tab: MacTab
    @Environment(\.colorScheme) private var scheme
    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 10) {
                YMBrandMark(size: 34)
                Text("译梦").font(.system(size: 20, weight: .bold))
            }
            .padding(.leading, 8).padding(.bottom, 18).padding(.top, 12)
            ForEach(MacTab.allCases) { t in
                let on = tab == t
                Button {
                    withAnimation(.snappy) { tab = t }
                } label: {
                    HStack(spacing: 12) {
                        ZStack {
                            if on {
                                Crayon(shape: WobblyCircle(), color: Theme.accent(scheme), width: 1.6)
                                    .frame(width: 34, height: 34)
                            }
                            YMIcon(glyph: t.glyph, size: 18, weight: on ? 2.0 : 1.6, active: on)
                        }
                        .frame(width: 36, height: 36)
                        Text(t.rawValue).font(.system(size: 15, weight: on ? .semibold : .regular))
                        Spacer()
                    }
                    .padding(.horizontal, 8).frame(height: 46)
                    .background(RoundedRectangle(cornerRadius: 14, style: .continuous).fill(on ? Theme.fill(scheme) : .clear))
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
            Spacer()
            Crayon(shape: WaveShape(), color: Theme.fg(scheme).opacity(0.25), width: 1.4)
                .frame(width: 90, height: 10).padding(.leading, 10).padding(.bottom, 20)
        }
        .padding(.horizontal, 14)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .foregroundStyle(Theme.fg(scheme))
        .background(Theme.base(scheme))
    }
}

struct MacContent: View {
    let tab: MacTab
    @Environment(\.colorScheme) private var scheme
    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack(spacing: 12) {
                YMBrandMark(size: 28)
                Text(tab.rawValue).font(.system(size: 28, weight: .bold))
                Spacer()
            }
            Text(copy).font(.system(size: 14)).foregroundStyle(Theme.secondary(scheme))
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 120), spacing: 16)], spacing: 16) {
                ForEach(demoGlyphs, id: \.0) { name, g in
                    VStack(spacing: 8) {
                        ZStack {
                            RoundedRectangle(cornerRadius: 16, style: .continuous).fill(Theme.fill(scheme))
                            YMIcon(glyph: g, size: 28, weight: 1.8)
                        }
                        .frame(height: 72)
                        Text(name).font(.system(size: 11)).foregroundStyle(Theme.secondary(scheme))
                    }
                }
            }
            Spacer()
        }
        .padding(28)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .background(Theme.base(scheme))
        .foregroundStyle(Theme.fg(scheme))
    }
    private var copy: String {
        switch tab {
        case .home: "共享 梦境环 图标语言 · macOS 壳（里程碑）"
        case .library: "曲库视图稍后接入共享 LibraryView"
        case .collection: "典藏与 iOS 共用 YMIcon / YMBrandMark"
        case .search: "搜索稍后接入 LibrarySearchView"
        case .me: "设置行已走 YMLeadingIcon → YMGlyph"
        }
    }
    private var demoGlyphs: [(String, YMGlyph)] {
        [
            ("首页", .home), ("曲库", .library), ("典藏", .collection), ("搜索", .search), ("我的", .me),
            ("播放", .play), ("暂停", .pause), ("上一首", .previous), ("下一首", .next),
            ("随机", .shuffle), ("循环", .repeat), ("歌词", .lyrics), ("队列", .queue), ("更多", .more),
            ("喜欢", .heart), ("下载", .download), ("分享", .share), ("设置", .settings),
            ("返回", .back), ("关闭", .close), ("识曲", .recognize)
        ]
    }
}

struct MacPlayerColumn: View {
    @Binding var playing: Bool
    @Environment(\.colorScheme) private var scheme
    var body: some View {
        VStack(spacing: 18) {
            Spacer(minLength: 20)
            ZStack {
                RoundedRectangle(cornerRadius: 8, style: .continuous)
                    .fill(Theme.fill(scheme))
                    .frame(width: 260, height: 260)
                    .overlay(YMBrandMark(size: 96).opacity(0.55))
                Crayon(shape: WobblyFrameMac(), color: Theme.fg(scheme), width: 2)
                    .frame(width: 280, height: 280)
            }
            Text("未在播放").font(.system(size: 18, weight: .semibold))
            Text("译梦 · 梦境环").font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme))
            HStack(spacing: 28) {
                Button { } label: { YMIcon(glyph: .shuffle, size: 18, weight: 1.7) }.buttonStyle(.plain)
                Button { } label: { YMIcon(glyph: .previous, size: 26, weight: 1.9, filled: true) }.buttonStyle(.plain)
                Button { playing.toggle() } label: {
                    ZStack {
                        Circle().fill(Theme.accent(scheme)).frame(width: 56, height: 56)
                        YMIcon(glyph: playing ? .pause : .play, size: 22, weight: 1.9, filled: true, color: Color(white: 0.07))
                    }
                }.buttonStyle(.plain)
                Button { } label: { YMIcon(glyph: .next, size: 26, weight: 1.9, filled: true) }.buttonStyle(.plain)
                Button { } label: { YMIcon(glyph: .queue, size: 18, weight: 1.7) }.buttonStyle(.plain)
            }
            .padding(.top, 8)
            Spacer()
        }
        .padding(24)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .foregroundStyle(Theme.fg(scheme))
        .background(Theme.base(scheme))
    }
}

/// Local wobble circle (same language as iPad) so Mac shell does not pull IPad.swift.
struct WobblyCircle: Shape {
    var seed: UInt64 = 3
    func path(in r: CGRect) -> Path {
        var rng = SeededRNG(seed); var p = Path()
        let c = CGPoint(x: r.midX, y: r.midY), R = min(r.width, r.height) / 2
        for pass in 0..<2 {
            let n = 14
            for i in 0...n {
                let a = Double(i) / Double(n) * 2 * .pi + (pass == 0 ? 0 : 0.3)
                let rr = R * CGFloat(1 + rng.d(-0.05, 0.05))
                let pt = CGPoint(x: c.x + CGFloat(cos(a)) * rr, y: c.y + CGFloat(sin(a)) * rr)
                i == 0 ? p.move(to: pt) : p.addLine(to: pt)
            }
        }
        return p
    }
}

struct WobblyFrameMac: Shape {
    var seed: UInt64 = 7
    func path(in r: CGRect) -> Path {
        var rng = SeededRNG(seed); var p = Path()
        let inset: CGFloat = 4
        let rect = r.insetBy(dx: inset, dy: inset)
        let pts: [CGPoint] = [
            CGPoint(x: rect.minX + CGFloat(rng.d(0, 3)), y: rect.minY + CGFloat(rng.d(0, 3))),
            CGPoint(x: rect.maxX - CGFloat(rng.d(0, 3)), y: rect.minY + CGFloat(rng.d(0, 3))),
            CGPoint(x: rect.maxX - CGFloat(rng.d(0, 3)), y: rect.maxY - CGFloat(rng.d(0, 3))),
            CGPoint(x: rect.minX + CGFloat(rng.d(0, 3)), y: rect.maxY - CGFloat(rng.d(0, 3)))
        ]
        p.move(to: pts[0])
        for i in 0..<4 {
            let a = pts[i], b = pts[(i + 1) % 4]
            let mid = CGPoint(x: (a.x + b.x) / 2 + CGFloat(rng.d(-2, 2)), y: (a.y + b.y) / 2 + CGFloat(rng.d(-2, 2)))
            p.addQuadCurve(to: b, control: mid)
        }
        p.closeSubpath()
        return p
    }
}



enum MacShot {
    @MainActor static func capture() {
        let root = MacRoot()
            .environment(\.colorScheme, .dark)
            .preferredColorScheme(.dark)
            .frame(width: 1280, height: 800)
            .background(Theme.base(.dark))
        let host = NSHostingView(rootView: root)
        host.frame = NSRect(x: 0, y: 0, width: 1280, height: 800)
        host.appearance = NSAppearance(named: .darkAqua)
        host.layoutSubtreeIfNeeded()
        guard let img = host.bitmapImageRepForCachingDisplay(in: host.bounds) else {
            fputs("[ym] shotMac bitmap failed\n", stderr); NSApp.terminate(nil); return
        }
        host.cacheDisplay(in: host.bounds, to: img)
        guard let png = img.representation(using: .png, properties: [:]) else {
            fputs("[ym] shotMac png failed\n", stderr); NSApp.terminate(nil); return
        }
        let url = FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent("ymshots/v41-mac-shell.png")
        try? FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        try? png.write(to: url)
        fputs("[ym] wrote \(url.path) \(png.count)\n", stderr)
        NSApp.terminate(nil)
    }
}
