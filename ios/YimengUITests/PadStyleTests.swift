import XCTest

/// v38: iPad in the app's own style — landscape (sidebar + list + player), vinyl, settings sheets.
final class PadStyleTests: XCTestCase {
    let out = ProcessInfo.processInfo.environment["YM_SHOTS"] ?? "/Users/edy/ymshots"
    func shot(_ name: String) { try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(out)/\(name).png")) }
    func run(_ args: [String], wait: UInt32, _ name: String) {
        let app = XCUIApplication(); app.launchArguments = args; app.launch(); sleep(wait); shot(name); app.terminate()
    }
    func testV38_PadLandscape() throws {
        try XCTSkipIf(UIDevice.current.userInterfaceIdiom != .pad, "pad only")
        XCUIDevice.shared.orientation = .landscapeLeft; sleep(2)
        run(["-static", "-debugPlay"], wait: 16, "v38-ipad-landscape")
        run(["-static", "-debugPlay", "-padFull"], wait: 16, "v38-ipad-vinyl")
        run(["-static", "-screen", "me"], wait: 8, "v38-ipad-me")
        run(["-static", "-screen", "me", "-me", "offline"], wait: 10, "v38-ipad-offline")
        run(["-static", "-screen", "me", "-me", "sound"], wait: 10, "v38-ipad-sound")
        run(["-static", "-screen", "library"], wait: 10, "v38-ipad-library")
        XCUIDevice.shared.orientation = .portrait; sleep(2)
        run(["-static", "-debugPlay"], wait: 14, "v38-ipad-portrait")
        run(["-static", "-debugPlay", "-padFull"], wait: 14, "v38-ipad-vinyl-portrait")
    }
}
