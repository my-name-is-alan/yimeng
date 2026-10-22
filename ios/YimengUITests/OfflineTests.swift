import XCTest

/// Phase E: gapless hand-off near the end of a daily track, then the 离线缓存 settings page.
final class OfflineTests: XCTestCase {
    let out = ProcessInfo.processInfo.environment["YM_SHOTS"] ?? "/Users/edy/ymshots"
    func shot(_ name: String) { try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(out)/\(name).png")) }
    func testE_OfflineAndGapless() throws {
        let app = XCUIApplication()
        app.launchArguments = ["-static", "-debugPlay", "-gaplessTest"]
        app.launch(); sleep(26)
        shot("v35-gapless-next")
        app.terminate()
        app.launchArguments = ["-static", "-debugPlay", "-soundSheet"]
        app.launch(); sleep(10)
        shot("v35-sound-transitions")
        let link = app.buttons["离线缓存"]
        if !link.exists { app.swipeUp(); sleep(1) }
        XCTAssertTrue(link.waitForExistence(timeout: 5))
        link.tap(); sleep(20)
        shot("v35-offline-settings")
    }
}
