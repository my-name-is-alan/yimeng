import XCTest

/// Phase F: 识曲 entry on 搜索 (Core only) → 10 s clip → Core result/error; haptics settings on 我的.
final class RecognizeUITests: XCTestCase {
    let out = ProcessInfo.processInfo.environment["YM_SHOTS"] ?? "/Users/edy/ymshots"
    func shot(_ name: String) { try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(out)/\(name).png")) }
    func testF_Recognize() throws {
        try XCTSkipIf(UIDevice.current.userInterfaceIdiom == .pad, "phone only")
        let app = XCUIApplication()
        app.launchArguments = ["-static", "-screen", "search"]
        app.launch(); sleep(6)
        let entry = app.buttons["search.recognize"]
        XCTAssertTrue(entry.waitForExistence(timeout: 10))
        shot("v36-search-entry")
        entry.tap(); sleep(2)
        shot("v36-recognize-idle")
        app.buttons["recognize.go"].tap(); sleep(2)
        let sb = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        for l in ["允许", "Allow", "好", "OK"] where sb.buttons[l].exists { sb.buttons[l].tap(); break }
        sleep(3)
        shot("v36-recognize-listening")
        sleep(14)
        shot("v36-recognize-result")
        app.terminate()
        app.launchArguments = ["-static", "-screen", "me"]
        app.launch(); sleep(5)
        let t = app.switches["触感反馈"]
        for _ in 0..<4 where !t.isHittable { app.swipeUp(); sleep(1) }
        shot("v36-haptics-settings")
    }
}
