import XCTest

/// Phase C: play something (fills the shared App Group snapshot), then add the 译梦 widget on the simulator home screen.
final class WidgetTests: XCTestCase {
    let out = ProcessInfo.processInfo.environment["YM_SHOTS"] ?? "/Users/edy/ymshots"
    func shot(_ name: String) { try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "\(out)/\(name).png")) }
    func tapAny(_ q: [XCUIElement], timeout: TimeInterval = 5) -> Bool {
        let end = Date().addingTimeInterval(timeout)
        while Date() < end { for e in q where e.exists && e.isHittable { e.tap(); return true }; usleep(300_000) }
        return false
    }
    func testC_Widgets() throws {
        try XCTSkipIf(UIDevice.current.userInterfaceIdiom == .pad, "phone only")
        let app = XCUIApplication()
        app.launchArguments = ["-static", "-debugPlay"]
        app.launch(); sleep(14)
        XCUIDevice.shared.press(.home); sleep(2)
        let sb = XCUIApplication(bundleIdentifier: "com.apple.springboard")
        for f in [0, 1] { _ = f; sb.swipeRight(); sleep(1) }   // today/widget page tends to be easiest, but home page also fine
        XCUIDevice.shared.press(.home); sleep(2)
        sb.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.62)).press(forDuration: 2.0); sleep(2)
        shot("v34-sb-edit")
        _ = tapAny([sb.buttons["编辑"], sb.buttons["Edit"]], timeout: 3); sleep(1)
        _ = tapAny([sb.buttons["添加小组件"], sb.buttons["Add Widget"], sb.buttons["添加"], sb.buttons["Add"]], timeout: 4); sleep(2)
        shot("v34-sb-gallery")
        let search = sb.searchFields.firstMatch
        if search.waitForExistence(timeout: 4) { search.tap(); search.typeText("译梦"); sleep(2) }
        _ = tapAny([sb.cells.containing(.staticText, identifier: "译梦").firstMatch, sb.buttons["译梦"], sb.staticTexts["译梦"]], timeout: 5); sleep(2)
        shot("v34-sb-picker")
        let added = tapAny([sb.buttons["添加小组件"], sb.buttons["Add Widget"]], timeout: 5); sleep(3)
        if !added { sb.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.905)).tap(); sleep(3) }
        XCUIDevice.shared.press(.home); sleep(4)
        shot("v34-widget-home")
    }
}
