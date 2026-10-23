import XCTest
import UIKit
@testable import Yimeng

final class CoreClientTests: XCTestCase {
    func testTrackDecodeAndMap() throws {
        let j = ##"{"id":"qq:003aBc","title":"仅可试听的歌","artists":[{"name":"Q 音歌手"}],"album":{"name":"Q 音专辑"},"origin":"qq","playable":true,"preview":true,"preview_window":{"start_ms":30000,"end_ms":60000},"duration_ms":48000,"sort_letter":"J","isrc":"CN1","popularity":0.5,"cover":{"url":"http://h/api/covers/qq_003?size=600&exp=1&sig=ab","blurhash":"LEHV6nWB2yk8pyo0adR*.7kCMdnj","colors":["#7a1cff","#ff3d8b","#33d17a"]}}"##
        let t = try CoreClient.decoder.decode(CoreTrack.self, from: Data(j.utf8))
        let s = CoreClient.nd(t)
        XCTAssertEqual(s.origin, "qq"); XCTAssertEqual(s.preview, true); XCTAssertEqual(s.sortLetter, "J")
        XCTAssertEqual(s.duration, 48); XCTAssertFalse(s.isUnplayable); XCTAssertEqual(s.isrc, "CN1")
    }
    func testUnplayable() {
        var s = NDSong(id: "kugou:1", title: "x", artist: "a", album: nil, albumId: nil, coverId: nil, duration: 1, track: nil)
        s.playable = false; s.origin = "kugou"
        XCTAssertTrue(s.isUnplayable); XCTAssertTrue(s.unplayableToast.contains("酷狗"))
    }
    func testSignedCoverResizeKeepsSig() {
        let u = URL(string: "http://h/api/covers/al1?size=600&exp=1&sig=ab")!
        // Core signs path+device+exp only, so the size param may be rewritten.
        XCTAssertEqual(CoverCache.sized(u, points: 44).query?.contains("sig=ab"), true)
        XCTAssertEqual(CoverCache.key(u), CoverCache.key(URL(string: "http://h/api/covers/al1?size=600&exp=2&sig=cd")!))
    }
    func testD58EntitlementAndImport() throws {
        // `unavailable` wins over any preview hint: greyed, no 试听.
        let j = ##"{"id":"netease:1","title":"t","preview":true,"playable":true,"entitlement":"unavailable"}"##
        let s = CoreClient.nd(try CoreClient.decoder.decode(CoreTrack.self, from: Data(j.utf8)))
        XCTAssertEqual(s.preview, false); XCTAssertEqual(s.playable, false); XCTAssertTrue(s.isUnplayable)
        let f = CoreClient.nd(try CoreClient.decoder.decode(CoreTrack.self, from: Data(#"{"id":"netease:2","title":"t","entitlement":"full"}"#.utf8)))
        XCTAssertEqual(f.preview, false); XCTAssertFalse(f.isUnplayable)
        let r = try CoreClient.decoder.decode(CoreCloneResult.self, from: Data(#"{"tracks":80,"added":80,"skipped_duplicates":20,"unavailable":3}"#.utf8))
        XCTAssertEqual(CoreCloneResult.summary(r), "导入 80 首，跳过 20 首重复，3 首不可播")
        let n = try CoreClient.decoder.decode(CoreCloneResult.self, from: Data(#"{"tracks":0,"added":0,"skipped_duplicates":67}"#.utf8))
        XCTAssertEqual(CoreCloneResult.summary(n), "没有新歌，67 首都已在歌单里")
        let me = try CoreClient.decoder.decode(CoreMe.self, from: Data(#"{"id":"usr_1","username":"u","nickname":"N","avatar_url":"/api/v1/users/usr_1/avatar?v=1","role":"user"}"#.utf8))
        XCTAssertEqual(me.avatarUrl, "/api/v1/users/usr_1/avatar?v=1")
        let big = UIGraphicsImageRenderer(size: CGSize(width: 3000, height: 1600), format: { let f = UIGraphicsImageRendererFormat(); f.scale = 1; return f }()).image { c in UIColor.systemPink.setFill(); c.fill(CGRect(x: 0, y: 0, width: 3000, height: 1600)) }
        let d = try XCTUnwrap(CoreProfileStore.prepare(big))
        let out = try XCTUnwrap(UIImage(data: d))
        XCTAssertEqual(out.size.width, 512); XCTAssertEqual(out.size.height, 512); XCTAssertLessThan(d.count, 1_048_576)
    }
    func testLiveShapes() throws {
        let j = ##"{"id":"qq:1","title":"t","artist_name":"A","album":{"id":"qq:al","name":"B"},"preview":false,"entitlement":"preview","cover_url":"https://x/c.jpg"}"##
        let s = CoreClient.nd(try CoreClient.decoder.decode(CoreTrack.self, from: Data(j.utf8)))
        XCTAssertEqual(s.preview, true); XCTAssertEqual(s.artist, "A"); XCTAssertEqual(s.origin, "qq")
        let tok = try CoreClient.decoder.decode(CoreTokens.self, from: Data(#"{"token":"ymd_x","device":{"id":"d"}}"#.utf8))
        XCTAssertEqual(tok.accessToken, "ymd_x")
        let st = try CoreClient.decoder.decode(CoreTokens.self, from: Data(#"{"access_token":"a","refresh_token":"r","expires_in":900,"device":{"id":"d","user_id":"usr_1"},"user":{"id":"usr_1","username":"yimeng-test"}}"#.utf8))
        XCTAssertEqual(st.userId, "usr_1"); XCTAssertEqual(st.username, "yimeng-test"); XCTAssertEqual(st.refreshToken, "r")
        let d = try CoreClient.decoder.decode(CoreDaily.self, from: Data(#"{"date":"2026-10-22","items":[{"id":"qq:1","title":"t"}]}"#.utf8))
        XCTAssertEqual(d.list.count, 1)
    }
    @MainActor func testQuoteHighlightPrefersHook() {
        let l = ["作词：某人", "夜色漫过窗台", "一拍一拍 心跳成歌", "风把星星吹亮", "一拍一拍 心跳成歌"]
        XCTAssertEqual(YueqianStore.highlight(l), "一拍一拍 心跳成歌")
    }
    func testStableHash() { XCTAssertEqual("2026-10-22".hashValue32, "2026-10-22".hashValue32) }
}
