import XCTest

/// Drives real touches in the simulator and writes screenshots to $YM_SHOTS (host path).
final class GestureTests: XCTestCase {
    var app: XCUIApplication!
    let out = ProcessInfo.processInfo.environment["YM_SHOTS"] ?? "/Users/edy/ymshots"

    func shot(_ name: String) {
        let d = XCUIScreen.main.screenshot().pngRepresentation
        try? d.write(to: URL(fileURLWithPath: "\(out)/\(name).png"))
    }
    func pt(_ x: CGFloat, _ y: CGFloat) -> XCUICoordinate { app.coordinate(withNormalizedOffset: CGVector(dx: x, dy: y)) }
    func drag(_ a: (CGFloat, CGFloat), _ b: (CGFloat, CGFloat), hold: TimeInterval = 0.05) {
        pt(a.0, a.1).press(forDuration: hold, thenDragTo: pt(b.0, b.1), withVelocity: .fast, thenHoldForDuration: 0)
    }
    func launch(_ args: [String]) {
        app = XCUIApplication()
        app.launchArguments = ["-static"] + args
        app.launch()
        sleep(7)
    }

    func testA1_SongMenu() {
        launch(["-screen", "library"])
        pt(0.5, 0.42).press(forDuration: 1.2)
        sleep(2); shot("v32-menu-song")
    }
    func testA2_AlbumMenu() {
        launch(["-screen", "library", "-seg", "专辑"])
        pt(0.27, 0.4).press(forDuration: 1.2)
        sleep(2); shot("v32-menu-album")
    }
    func testA3_MiniSwipeUpThenDownAndCover() {
        launch([])
        drag((0.5, 0.835), (0.5, 0.45)); sleep(2); shot("v32-mini-swipe-up")
        // cover swipe left → next track (user-initiated)
        drag((0.75, 0.3), (0.1, 0.3)); sleep(3); shot("v32-cover-swipe")
        drag((0.5, 0.25), (0.5, 0.95)); sleep(2); shot("v32-swipe-down-collapsed")
    }
    func testA4_EdgeBack() {
        launch([])
        app.swipeUp(); sleep(1)
        pt(0.2, 0.4).tap(); sleep(3); shot("v32-edge-before")
        drag((0.005, 0.5), (0.85, 0.5)); sleep(2); shot("v32-edge-after")
    }
    func testA5_LyricsPinch() {
        launch([])
        drag((0.5, 0.835), (0.5, 0.45)); sleep(2)
        app.buttons["quote.bubble"].firstMatch.tap(); sleep(3)
        app.pinch(withScale: 1.8, velocity: 2); sleep(2); shot("v32-lyrics-pinch")
        pt(0.5, 0.62).tap(); sleep(2); shot("v32-lyrics-tapseek")
    }
}
