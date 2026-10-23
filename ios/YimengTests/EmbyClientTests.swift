import XCTest
@testable import Yimeng

/// Sample JSON shaped from the Emby / Jellyfin REST docs (dev.emby.media, api.jellyfin.org). Test target only.
final class EmbyClientTests: XCTestCase {
    private func obj(_ n: String) throws -> Any {
        let url = URL(fileURLWithPath: #filePath).deletingLastPathComponent().appendingPathComponent("Fixtures/\(n).json")
        return try JSONSerialization.jsonObject(with: Data(contentsOf: url))
    }
    private func items(_ n: String) throws -> [[String: Any]] { ((try obj(n) as? [String: Any])?["Items"] as? [[String: Any]]) ?? [] }

    func testNormalize() {
        XCTAssertEqual(EmbyClient.normalize("192.168.1.10:8096")?.absoluteString, "http://192.168.1.10:8096")
        XCTAssertEqual(EmbyClient.normalize("https://e.example.com/emby/")?.absoluteString, "https://e.example.com/emby")
        XCTAssertEqual(EmbyClient.normalize("http://webhost:8096/web/index.html#!/home")?.absoluteString, "http://webhost:8096")
        XCTAssertNil(EmbyClient.normalize(" "))
    }
    func testServerInfoProduct() throws {
        XCTAssertEqual(EmbyClient.parseServerInfo(try obj("emby-sysinfo") as! [String: Any]).product, "Emby")
        XCTAssertEqual(EmbyClient.parseServerInfo(try obj("jellyfin-sysinfo") as! [String: Any]).product, "Jellyfin")
    }
    func testAuthHeaderHasNoSecretsUnlessGiven() {
        let h = EmbyClient.authHeader(userId: nil, token: nil)
        XCTAssertTrue(h.contains("Client=\"Yimeng\"")); XCTAssertTrue(h.contains("DeviceId=")); XCTAssertFalse(h.contains("Token="))
        XCTAssertTrue(EmbyClient.authHeader(userId: "u", token: "t").contains("Token=\"t\""))
    }
    func testAuthHeaderIsASCII() {
        XCTAssertTrue(EmbyClient.authHeader(userId: "u", token: "t").allSatisfy { $0.isASCII })
    }
    func testUsers() throws {
        let u = ((try obj("emby-users") as? [[String: Any]]) ?? []).compactMap(EmbyClient.parseUser)
        XCTAssertEqual(u.map(\.name), ["admin", "妈妈"]); XCTAssertTrue(u[0].isAdmin); XCTAssertFalse(u[1].isAdmin)
        let a = try obj("emby-auth") as? [String: Any]
        XCTAssertEqual(a?["AccessToken"] as? String, "tok-abc")
    }
    func testAlbums() throws {
        let a = try items("emby-albums").compactMap(EmbyClient.parseAlbum)
        XCTAssertEqual(a.count, 2)
        XCTAssertEqual(a[0].artist, "周杰伦"); XCTAssertEqual(a[0].artistId, "ar1"); XCTAssertEqual(a[0].year, 2001)
        XCTAssertEqual(a[0].coverId, "al1"); XCTAssertEqual(a[0].songCount, 10); XCTAssertNil(a[1].coverId)
    }
    func testSongs() throws {
        let s = try items("emby-audio").compactMap(EmbyClient.parseSong)
        XCTAssertEqual(s[0].duration, 234); XCTAssertEqual(s[0].coverId, "al1"); XCTAssertEqual(s[0].suffix, "flac")
        XCTAssertEqual(s[0].bitRate, 1048); XCTAssertEqual(s[0].bitDepth, 16); XCTAssertEqual(s[0].track, 2)
        XCTAssertEqual(s[1].artist, "A / B"); XCTAssertEqual(s[1].coverId, "s2")
    }
    func testStreamURLs() {
        let b = URL(string: "http://nas:8096")!
        let raw = EmbyClient.streamURL(base: b, id: "s1", token: "TK", userId: "u", maxKbps: 0, playSessionId: "p")!.absoluteString
        XCTAssertTrue(raw.hasPrefix("http://nas:8096/Audio/s1/stream?static=true")); XCTAssertTrue(raw.contains("api_key=TK"))
        let tc = EmbyClient.streamURL(base: b, id: "s1", token: "TK", userId: "u", maxKbps: 320, playSessionId: "p")!.absoluteString
        XCTAssertTrue(tc.contains("/Audio/s1/universal")); XCTAssertTrue(tc.contains("MaxStreamingBitrate=320000")); XCTAssertTrue(tc.contains("AudioCodec=mp3"))
        XCTAssertEqual(EmbyClient.imageURL(base: URL(string: "https://e.x/emby")!, id: "al1", size: 300)?.absoluteString,
                       "https://e.x/emby/Items/al1/Images/Primary?maxWidth=300&quality=90")
    }
    func testRealServerShapes() throws {
        // Sanitized from a live Emby 4.10 (strm/cloud library): album art via PrimaryImageItemId, mediasource_ ids.
        let al = EmbyClient.parseAlbum(["Id": "1001", "Name": "X", "Type": "MusicAlbum", "ImageTags": [String: Any](),
                                        "PrimaryImageItemId": "2002", "PrimaryImageTag": "t", "AlbumArtist": "A"])
        XCTAssertEqual(al?.coverId, "2002")
        XCTAssertNil(EmbyClient.parseSong(["Id": "9", "Name": "_1-1_animated_artwork"]))
        let s = EmbyClient.parseSong(["Id": "3003", "Name": "T", "ImageTags": ["Primary": "t"],
                                      "MediaSources": [["Id": "mediasource_3003", "Container": "flac"]]])
        XCTAssertEqual(s?.suffix, "flac")
        let u = EmbyClient.streamURL(base: URL(string: "http://h:8091")!, id: "3003", token: "TK", userId: "u", maxKbps: 0, playSessionId: "p")!.absoluteString
        XCTAssertTrue(u.contains("MediaSourceId=mediasource_3003"))
        XCTAssertEqual(EmbyClient.playBody(id: "3003", position: 0, paused: false, playSessionId: "p", transcoding: false)["MediaSourceId"] as? String, "mediasource_3003")
    }
    func testPlayBody() {
        let b = EmbyClient.playBody(id: "s1", position: 12.5, paused: false, playSessionId: "p", transcoding: false)
        XCTAssertEqual(b["PositionTicks"] as? Int64, 125_000_000); XCTAssertEqual(b["PlayMethod"] as? String, "DirectStream")
    }
    func testLyrics() throws {
        let l = EmbyClient.parseLyrics(try obj("jellyfin-lyrics") as! [String: Any])
        XCTAssertEqual(l.count, 2); XCTAssertEqual(l[1].0, 5.2, accuracy: 0.001)
    }
}
