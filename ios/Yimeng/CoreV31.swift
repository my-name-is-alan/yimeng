import SwiftUI
import PhotosUI
import UIKit

// MARK: - D58 profile: /me, nickname, avatar upload

struct CoreMe: Codable, Sendable, Equatable {
    var id: String?; var username: String?; var nickname: String?; var avatarUrl: String?; var role: String?
}

extension CoreClient {
    func me() async throws -> CoreMe { try await get("/api/v1/me") }
    func setNickname(_ n: String) async throws -> CoreMe {
        struct B: Encodable { let nickname: String }
        return try await send("PATCH", "/api/v1/me", body: B(nickname: String(n.prefix(32))))
    }
    func deleteAvatar() async throws { try await fire("DELETE", "/api/v1/me/avatar") }
    func platformsNow() async -> [CorePlatform] {
        struct I: Codable { var platforms: [CorePlatform]? }
        return ((try? await get("/api/v1/server/info") as I)?.platforms) ?? CoreState.platforms
    }
}

/// The signed-in Core account (D58). Only used when Core is the active source.
@MainActor
final class CoreProfileStore: ObservableObject {
    static let shared = CoreProfileStore()
    @Published private(set) var me: CoreMe?
    @Published private(set) var avatar: UIImage?
    private var loadedKey = ""

    var displayName: String? { me.flatMap { ($0.nickname?.isEmpty == false ? $0.nickname : $0.username) } }

    func load(force: Bool = false) async {
        guard NavidromeClient.core else { me = nil; avatar = nil; loadedKey = ""; return }
        let k = LibraryStore.shared.host + "|" + LibraryStore.shared.user
        if !force, k == loadedKey, me != nil { return }
        let m: CoreMe
        do { m = try await CoreClient.shared.me() } catch { NSLog("[ym] /me failed: %@", String(describing: error)); return }
        loadedKey = k
        await adopt(m)
    }
    func adopt(_ m: CoreMe) async {
        let changed = m.avatarUrl != me?.avatarUrl || avatar == nil
        me = m
        guard let p = m.avatarUrl, !p.isEmpty else { avatar = nil; return }
        if changed {
            do { let d = try await CoreClient.shared.imageData(p); avatar = UIImage(data: d) }
            catch { NSLog("[ym] avatar fetch failed: %@", String(describing: error)) }
        }
    }
    func rename(_ n: String) async throws {
        let m = try await CoreClient.shared.setNickname(n.trimmingCharacters(in: .whitespacesAndNewlines))
        await adopt(m)
    }
    /// Crop square, downscale to ≤512 px, JPEG ≤1 MiB, then PUT /me/avatar.
    func upload(_ img: UIImage) async throws {
        guard let d = Self.prepare(img) else { throw NavidromeError.api("图片无法处理") }
        let m = try await CoreClient.shared.putAvatar(d, mime: "image/jpeg")
        avatar = nil
        await adopt(m)
    }
    func removeAvatar() async throws {
        try await CoreClient.shared.deleteAvatar()
        avatar = nil
        if var m = me { m.avatarUrl = nil; me = m }
    }
    func clear() { me = nil; avatar = nil; loadedKey = "" }

    nonisolated static func prepare(_ img: UIImage, side: CGFloat = 512) -> Data? {
        guard let cg = img.cgImage ?? UIImage(data: img.pngData() ?? Data())?.cgImage else { return nil }
        // Square centre crop in pixel space, honouring orientation via a redraw.
        let w = img.size.width, h = img.size.height, s = min(w, h)
        guard s > 0 else { return nil }
        let out = min(side, s * img.scale)
        let fmt = UIGraphicsImageRendererFormat(); fmt.scale = 1; fmt.opaque = true
        let r = UIGraphicsImageRenderer(size: CGSize(width: out, height: out), format: fmt).image { _ in
            let k = out / s
            img.draw(in: CGRect(x: -(w - s) / 2 * k, y: -(h - s) / 2 * k, width: w * k, height: h * k))
        }
        _ = cg
        var q: CGFloat = 0.85
        var d = r.jpegData(compressionQuality: q)
        while let x = d, x.count > 1_000_000, q > 0.3 { q -= 0.15; d = r.jpegData(compressionQuality: q) }
        return d
    }
}

// MARK: - 个人资料

struct ProfileView: View {
    @Environment(\.colorScheme) private var scheme
    @Environment(\.dismiss) private var dismiss
    @ObservedObject private var store = CoreProfileStore.shared
    @State private var pick: PhotosPickerItem?
    @State private var name = ""
    @State private var busy = false
    @State private var platforms: [CorePlatform] = []
    @State private var confirmRemove = false
    @FocusState private var nameFocus: Bool

    var body: some View {
        YMPage("个人资料", trailing: {
            Button("保存") { Task { await save() } }.buttonStyle(YMPillButtonStyle())
                .disabled(busy || name == (store.me?.nickname ?? "")).opacity(busy || name == (store.me?.nickname ?? "") ? 0.4 : 1)
        }) {
            VStack(spacing: 12) {
                PhotosPicker(selection: $pick, matching: .images) {
                    ZStack(alignment: .bottomTrailing) {
                        Group {
                            if let a = store.avatar { Image(uiImage: a).resizable().scaledToFill() }
                            else { MonogramAvatar(name: store.displayName ?? "梦", size: 96) }
                        }
                        .frame(width: 96, height: 96).clipShape(Circle())
                        .overlay { if busy { Circle().fill(.black.opacity(0.35)); ProgressView().tint(.white) } }
                        .overlay(Crayon(shape: Circle(), color: Theme.accent(scheme).opacity(0.7), width: 1.4).padding(-6))
                        Image(systemName: "camera.fill").font(.system(size: 12, weight: .bold)).foregroundStyle(.black)
                            .padding(7).background(Circle().fill(Theme.lime))
                    }
                }.buttonStyle(.plain).disabled(busy)
                if store.me?.avatarUrl?.isEmpty == false {
                    Button("移除头像") { confirmRemove = true }.buttonStyle(YMPillButtonStyle(destructive: true)).disabled(busy)
                } else {
                    Text("点击更换头像").font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme))
                }
            }.frame(maxWidth: .infinity).padding(.top, 16)
            YMSection(footer: "昵称最多 32 个字，留空则显示用户名。") {
                HStack(spacing: 14) {
                    YMIcon(name: "pencil")
                    Text("昵称").font(.system(size: 16))
                    TextField(store.me?.username ?? "昵称", text: $name).multilineTextAlignment(.trailing).focused($nameFocus)
                        .submitLabel(.done).onSubmit { Task { await save() } }
                        .onChange(of: name) { _, v in if v.count > 32 { name = String(v.prefix(32)) } }
                }.frame(minHeight: 50)
                YMDivider()
                YMRow(icon: "person", "用户名", store.me?.username ?? "—")
                YMDivider()
                YMRow(icon: "checkmark.seal", "身份", store.me?.role == "admin" ? "管理员" : "成员")
            }
            YMSection(title: "平台账号") {
                let ps = platforms.filter { $0.loggedIn == true }
                if ps.isEmpty { YMRow(icon: "music.note", "还没有登录任何音乐平台") }
                ForEach(Array(ps.enumerated()), id: \.element.origin) { i, p in
                    if i > 0 { YMDivider() }
                    HStack(spacing: 12) {
                        Group {
                            if let s = p.avatarUrl, let u = URL(string: s) { CoverImage(url: u, points: 40) }
                            else { MonogramAvatar(name: p.nickname ?? p.name ?? p.origin, size: 40) }
                        }.frame(width: 40, height: 40).clipShape(Circle())
                        VStack(alignment: .leading, spacing: 2) {
                            HStack(spacing: 6) {
                                Text(p.nickname ?? "—").font(.system(size: 16, weight: .medium))
                                if p.vip == true {
                                    Text("VIP").font(.system(size: 10, weight: .heavy)).foregroundStyle(.black)
                                        .padding(.horizontal, 6).padding(.vertical, 2).background(Capsule().fill(Theme.lime))
                                }
                            }
                            Text(p.name ?? CoreOrigin.label(p.origin)).font(.system(size: 13)).foregroundStyle(Theme.secondary(scheme))
                        }
                        Spacer()
                        SourceBadge(origin: p.origin)
                    }.padding(.vertical, 10)
                }
            }
        }
        .confirmationDialog("移除头像？", isPresented: $confirmRemove, titleVisibility: .visible) {
            Button("移除", role: .destructive) { Task { await remove() } }
        }
        .onChange(of: pick) { _, it in guard let it else { return }; Task { await upload(it) } }
        .task {
            await store.load(force: true)
            name = store.me?.nickname ?? ""
            platforms = await CoreClient.shared.platformsNow()
        }
    }
    private func row(_ k: String, _ v: String) -> some View {
        HStack { Text(k); Spacer(); Text(v).foregroundStyle(.secondary) }
    }
    private func save() async {
        nameFocus = false
        busy = true; defer { busy = false }
        do { try await store.rename(name); name = store.me?.nickname ?? name; SheetCenter.shared.say("昵称已保存") }
        catch let e as CoreAPIError { SheetCenter.shared.say(e.message.isEmpty ? "保存失败" : e.message) }
        catch { SheetCenter.shared.say("保存失败") }
    }
    private func upload(_ it: PhotosPickerItem) async {
        busy = true; defer { busy = false; pick = nil }
        guard let d = try? await it.loadTransferable(type: Data.self), let img = UIImage(data: d) else { SheetCenter.shared.say("读不到这张图片"); return }
        await Self.upload(img)
    }
    static func upload(_ img: UIImage) async {
        do { try await CoreProfileStore.shared.upload(img); SheetCenter.shared.say("头像已更新") }
        catch let e as CoreAPIError {
            SheetCenter.shared.say(e.status == 413 ? "图片太大了" : e.status == 415 ? "只支持 JPEG / PNG / WebP" : (e.message.isEmpty ? "上传失败" : e.message))
        } catch { SheetCenter.shared.say("上传失败") }
    }
    private func remove() async {
        busy = true; defer { busy = false }
        do { try await store.removeAvatar(); SheetCenter.shared.say("头像已移除") } catch { SheetCenter.shared.say("移除失败") }
    }
}
