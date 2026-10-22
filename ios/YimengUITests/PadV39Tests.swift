import XCTest

/// v39: iPad size-aware grids, ambience, player doodle frame, reduce motion.
final class PadV39Tests: XCTestCase {
    let out = ProcessInfo.processInfo.environment["YM_SHOTS"] ?? "/Users/edy/ymshots"
    func shot(_ name: String) { try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(out)/\(name).png")) }
    func run(_ args: [String], wait: UInt32, _ name: String) {
        let app = XCUIApplication(); app.launchArguments = args; app.launch(); sleep(wait); shot(name); app.terminate()
    }
    func testV39_Pad() throws {
        try XCTSkipIf(UIDevice.current.userInterfaceIdiom != .pad, "pad only")
        XCUIDevice.shared.orientation = .landscapeLeft; sleep(2)
        run(["-debugPlay"], wait: 16, "v39-ipad-home-land")
        run(["-screen", "library", "-debugPlay"], wait: 12, "v39-ipad-library-land")
        run(["-seg", "歌手", "-screen", "library"], wait: 12, "v39-ipad-artists-land")
        run(["-open", "album", "-debugPlay"], wait: 16, "v39-ipad-album-land")
        run(["-debugPlay", "-reduceMotion"], wait: 14, "v39-ipad-reducemotion")
        XCUIDevice.shared.orientation = .portrait; sleep(2)
        run(["-debugPlay"], wait: 16, "v39-ipad-home-port")
        run(["-screen", "library"], wait: 12, "v39-ipad-library-port")
        run(["-debugPlay", "-padFull"], wait: 14, "v39-ipad-player-port")
    }
}
