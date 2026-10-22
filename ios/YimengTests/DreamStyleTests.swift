import XCTest
@testable import Yimeng

final class DreamStyleTests: XCTestCase {
    func testGenreFromTags() {
        XCTAssertEqual(DreamStyle.genreClass(["Alternative Rock"]), "rock")
        XCTAssertEqual(DreamStyle.genreClass(["民谣", "治愈"]), "folk")
        XCTAssertNil(DreamStyle.genreClass(["华语流行"]))
    }
    func testStableSeed() {
        XCTAssertEqual(DreamStyle.jitter("song-42"), DreamStyle.jitter("song-42"))
        let a = DreamStyle.decide(id: "x", tags: [], bpm: 120, measured: 0.5)
        let b = DreamStyle.decide(id: "x", tags: [], bpm: 120, measured: 0.5)
        XCTAssertEqual(a.energy, b.energy); XCTAssertEqual(a.genre, b.genre)
    }
    func testMotifsAppear() {
        let rock = DreamStyle.decide(id: "r", tags: ["rock"], bpm: nil, measured: nil)
        XCTAssertGreaterThan(rock.energy, 0.7)
        XCTAssertEqual(DreamStyle.decide(id: "f", tags: ["folk"], bpm: 150, measured: 0.9).genre, "folk")
        XCTAssertEqual(DreamStyle.decide(id: "q", tags: [], bpm: 72, measured: 0.05).genre, "folk")
        XCTAssertGreaterThan(DreamStyle.decide(id: "z", tags: [], bpm: 170, measured: 0.95).energy, 0.7)
    }
}
