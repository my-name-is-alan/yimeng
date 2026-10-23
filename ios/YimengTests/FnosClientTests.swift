import XCTest
@testable import Yimeng

/// Fixtures mirror response shapes documented by community fnOS clients — test-only, never in the app bundle.
final class FnosClientTests: XCTestCase {
    private func fixture(_ n: String) throws -> Data {
        let url = URL(fileURLWithPath: #filePath).deletingLastPathComponent().appendingPathComponent("Fixtures/\(n).json")
        return try Data(contentsOf: url)
    }
    func testNormalize() {
        XCTAssertEqual(FnosClient.normalize("192.168.1.10:5666")?.absoluteString, "http://192.168.1.10:5666")
        XCTAssertEqual(FnosClient.normalize("https://x.fnos.net/music/")?.absoluteString, "https://x.fnos.net")
        XCTAssertEqual(FnosClient.normalize("http://nas:5666/music/api/v1")?.absoluteString, "http://nas:5666")
        XCTAssertNil(FnosClient.normalize(""))
    }
    func testRelayCookie() {
        let relay = FnosClient.normalize("http://abc.fnos.net/music/")!
        XCTAssertEqual(relay.absoluteString, "https://abc.fnos.net")
        XCTAssertEqual(FnosClient.cookieHeader(root: relay, token: nil), "mode=relay")
        XCTAssertEqual(FnosClient.cookieHeader(root: relay, token: "T"), "mode=relay; music-token=T")
        XCTAssertEqual(FnosClient.cookieHeader(root: URL(string: "http://192.168.1.5:5666")!, token: "T"), "music-token=T")
        XCTAssertNil(FnosClient.cookieHeader(root: URL(string: "http://nas:5666")!, token: nil))
    }
    func testSha256() {
        XCTAssertEqual(FnosClient.sha256("abc"), "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad")
    }
    func testPageQuery() {
        XCTAssertEqual(FnosClient.pageQuery(offset: 0, count: 50), ["page": "1", "size": "50"])
        XCTAssertEqual(FnosClient.pageQuery(offset: 100, count: 50), ["page": "3", "size": "50"])
    }
    func testLoginUnwrap() throws {
        let d = try FnosClient.unwrap(fixture("fnos-login")) as? [String: Any]
        XCTAssertEqual(d?["userToken"] as? String, "tok123")
    }
    func testExpiredThrows() throws {
        XCTAssertThrowsError(try FnosClient.unwrap(fixture("fnos-expired")))
    }
    func testParseTracks() throws {
        let d = try FnosClient.unwrap(fixture("fnos-track-list")) as? [String: Any]
        let songs = ((d?["list"] as? [[String: Any]]) ?? []).compactMap(FnosClient.parseSong)
        XCTAssertEqual(songs.count, 2)
        XCTAssertEqual(songs[0].title, "夜曲"); XCTAssertEqual(songs[0].artist, "周杰伦")
        XCTAssertEqual(songs[0].duration, 226); XCTAssertEqual(songs[0].albumId, "a1")
        XCTAssertEqual(songs[0].suffix, "flac"); XCTAssertEqual(songs[0].bitRate, 1411); XCTAssertEqual(songs[0].bitDepth, 16)
        XCTAssertEqual(songs[1].coverId, "ca2"); XCTAssertEqual(songs[1].artist, "A / B")
    }
    func testMediaURLs() {
        let r = URL(string: "http://nas:5666")!
        XCTAssertEqual(FnosClient.streamURL(root: r, guid: "t1")?.absoluteString, "http://nas:5666/music/api/v1/track/stream?guid=t1")
        XCTAssertEqual(FnosClient.coverURL(root: r, coverId: "c1", size: 300)?.absoluteString, "http://nas:5666/music/api/v1/static/cover?coverId=c1&size=300")
    }
}
