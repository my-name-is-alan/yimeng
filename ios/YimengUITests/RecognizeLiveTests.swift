import XCTest

/// v40: 听歌识曲 against the real Core — needs `-recognizeFile` clip on the host (skips otherwise).
final class RecognizeLiveTests: XCTestCase {
    func testV40_RecognizeAndPlay() throws {
        let clip = "/Users/edy/ymshots/hk-clip.m4a"
        try XCTSkipUnless(FileManager.default.fileExists(atPath: clip), "no clip")
        let app = XCUIApplication(); app.launchArguments = ["-screen", "search", "-recognizeFile", clip]; app.launch()
        let play = app.buttons["播放 海阔天空"].firstMatch
        XCTAssertTrue(play.waitForExistence(timeout: 30), "result row with play button")
        try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "/Users/edy/ymshots/v40-recognize-result.png"))
        play.tap(); sleep(6)
        try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: "/Users/edy/ymshots/v40-recognize-played.png"))
    }
}
