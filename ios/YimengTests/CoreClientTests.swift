import XCTest
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
