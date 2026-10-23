import XCTest

/// v40: Home ambience, wide 2-column Home, album/artist detail.
final class PadV40Tests: XCTestCase {
    let out = ProcessInfo.processInfo.environment["YM_SHOTS"] ?? "/Users/edy/ymshots"
    func shot(_ name: String) { try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(out)/\(name).png")) }
    func run(_ args: [String], wait: UInt32, _ name: String) {
        let app = XCUIApplication(); app.launchArguments = args; app.launch(); sleep(wait); shot(name); app.terminate()
    }
    func testV39_Pad() throws {
        try XCTSkipIf(UIDevice.current.userInterfaceIdiom != .pad, "pad only")
        XCUIDevice.shared.orientation = .landscapeLeft; sleep(2)
        run(["-debugPlay"], wait: 30, "v40-ipad-home-land")
        run(["-debugPlay", "-padWide"], wait: 16, "v40-ipad-home-wide")
        run(["-open", "album", "-debugPlay"], wait: 18, "v40-ipad-album")
        run(["-open", "artist", "-debugPlay"], wait: 18, "v40-ipad-artist")
        XCUIDevice.shared.orientation = .portrait; sleep(2)
        run(["-debugPlay"], wait: 16, "v40-ipad-home-port")
    }
}
