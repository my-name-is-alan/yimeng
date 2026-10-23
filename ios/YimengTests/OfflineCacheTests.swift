import XCTest
@testable import Yimeng

final class OfflineCacheTests: XCTestCase {
    typealias E = OfflineCache.Entry
    func e(_ id: String, _ mb: Int64, plays: Int, age: TimeInterval, daily: Bool = false) -> E {
        E(id: id, file: id, bytes: mb * 1_048_576, lastUsed: Date(timeIntervalSinceNow: -age), plays: plays, pinnedDaily: daily)
    }
    func testUnderLimitEvictsNothing() {
        XCTAssertEqual(OfflineCache.evict([e("a", 10, plays: 0, age: 0)], limit: 100 * 1_048_576, keep: nil), [])
    }
    func testEvictsLeastPlayedThenOldest() {
        let es = [e("hot", 40, plays: 9, age: 900), e("old", 40, plays: 1, age: 500), e("new", 40, plays: 1, age: 10), e("daily", 40, plays: 0, age: 1000, daily: true)]
        // 160 MB → limit 90: drop "old" (1 play, older) then "daily"(0+1, older than "new") 
        XCTAssertEqual(OfflineCache.evict(es, limit: 90 * 1_048_576, keep: nil), ["daily", "old"])
    }
    func testKeepsJustAdded() {
        let es = [e("x", 60, plays: 0, age: 0), e("y", 60, plays: 5, age: 100)]
        XCTAssertEqual(OfflineCache.evict(es, limit: 70 * 1_048_576, keep: "x"), ["y"])
    }
    func testSafeNamesAndExt() {
        XCTAssertFalse(OfflineCache.safe("netease:12/3").contains("/"))
        XCTAssertNotEqual(OfflineCache.safe("a:1"), OfflineCache.safe("a_1"))
        XCTAssertEqual(OfflineCache.ext(mime: "audio/flac", suggested: nil), "flac")
        XCTAssertEqual(OfflineCache.ext(mime: nil, suggested: "song.m4a"), "m4a")
        XCTAssertEqual(OfflineCache.ext(mime: "audio/mpeg", suggested: "stream"), "mp3")
    }
}
