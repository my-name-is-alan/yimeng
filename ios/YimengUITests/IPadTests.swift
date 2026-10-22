import XCTest

/// iPad: rotation (needs XCUIDevice — Stage Manager blocks programmatic orientation), keyboard shortcuts, turntable.
final class IPadTests: XCTestCase {
    let out = ProcessInfo.processInfo.environment["YM_SHOTS"] ?? "/Users/edy/ymshots"
    func shot(_ name: String) { try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(out)/\(name).png")) }
    func testB_iPad() throws {
        try XCTSkipUnless(UIDevice.current.userInterfaceIdiom == .pad, "iPad only")
        XCUIDevice.shared.orientation = .portrait
        let app = XCUIApplication()
        app.launchArguments = ["-static", "-debugPlay", "-thenPause"]   // uses the Keychain session already on this simulator
        app.launch(); sleep(14)
        XCUIDevice.shared.orientation = .landscapeLeft; sleep(4)
        shot("v33-ipad-landscape")
        app.typeKey(" ", modifierFlags: []); sleep(2); shot("v33-ipad-kb-space")
        app.typeKey(.rightArrow, modifierFlags: .command); sleep(4); shot("v33-ipad-kb-next")
        app.buttons["全屏"].firstMatch.tap(); sleep(4); shot("v33-ipad-vinyl")
        app.buttons["chevron.down"].firstMatch.tap(); sleep(2)
        app.typeKey("f", modifierFlags: .command); sleep(3); shot("v33-ipad-kb-cmdF")
        app.typeKey(" ", modifierFlags: [])   // leave it paused
        XCUIDevice.shared.orientation = .portrait
    }
}
