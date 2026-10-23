import XCTest
@testable import Yimeng

final class RecognizeTests: XCTestCase {
    func testDecodeMatches() throws {
        let j = #"{"matches":[{"title":"白日梦","artist":"晚星乐队","album":"夜航","cover":"http://h/api/covers/al1","origin":"netease","track_id":"netease:9","offset_ms":42000},{"title":"无 id"}]}"#
        let r = try CoreClient.decoder.decode(CoreRecognizeResult.self, from: Data(j.utf8))
        XCTAssertEqual(r.matches.count, 2)
        XCTAssertEqual(r.matches[0].trackId, "netease:9"); XCTAssertEqual(r.matches[0].offsetMs, 42000)
        XCTAssertEqual(r.matches[0].song?.id, "netease:9"); XCTAssertEqual(r.matches[0].song?.origin, "netease")
        XCTAssertNil(r.matches[1].song)
    }
    func testDecodeCoverRef() throws {
        let j = #"{"matches":[{"title":"海阔天空","artist":"Beyond","cover":{"url":"/api/v1/images/2500a6","source_url":"http://p2.music.126.net/x.jpg"},"origin":"netease","track_id":"netease:1357375695","offset_ms":0}]}"#
        let r = try CoreClient.decoder.decode(CoreRecognizeResult.self, from: Data(j.utf8))
        XCTAssertEqual(r.matches[0].cover, "/api/v1/images/2500a6"); XCTAssertEqual(r.matches[0].song?.id, "netease:1357375695")
    }
    func testErrorMapping() {
        XCTAssertEqual(RecognizeFailure.from(CoreAPIError(status: 503, code: "fingerprint_unsupported", message: "")), .fingerprintUnsupported)
        XCTAssertEqual(RecognizeFailure.from(CoreAPIError(status: 429, code: "rate_limited", message: "")), .rateLimited)
        XCTAssertEqual(RecognizeFailure.from(CoreAPIError(status: 409, code: "not_logged_in", message: "")), .notLoggedIn)
        XCTAssertEqual(RecognizeFailure.from(CoreAPIError(status: 404, code: "no_match", message: "")), .noMatch)
        XCTAssertEqual(RecognizeFailure.from(CoreAPIError(status: 413, code: "too_long", message: "")), .tooLong)
        XCTAssertEqual(RecognizeFailure.from(CoreAPIError(status: 404, code: "not_found", message: "")), .notSupported)
    }
    func testMultipartBody() {
        let d = Multipart.body(boundary: "B", file: Data([1, 2, 3]), filename: "clip.m4a", mime: "audio/mp4", fields: [("duration_ms", "10000")])
        let s = String(decoding: d, as: UTF8.self)
        XCTAssertTrue(s.hasPrefix("--B\r\nContent-Disposition: form-data; name=\"duration_ms\"\r\n\r\n10000\r\n"))
        XCTAssertTrue(s.contains("name=\"audio\"; filename=\"clip.m4a\"\r\nContent-Type: audio/mp4\r\n\r\n"))
        XCTAssertTrue(s.hasSuffix("\r\n--B--\r\n"))
    }
}
