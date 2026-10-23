# 译梦 iOS (SwiftUI)

Regenerate project: `xcodegen generate`
Build+run on simulator:

    xcodebuild -project Yimeng.xcodeproj -scheme Yimeng -sdk iphonesimulator -destination "id=6D67C3C0-5812-468B-B4F0-377E2F6B8FB3" -derivedDataPath build CODE_SIGNING_ALLOWED=NO build
    xcrun simctl install booted build/Build/Products/Debug-iphonesimulator/Yimeng.app
    xcrun simctl launch booted art.yimeng.app            # optional: -screen home|album|artist|nowplaying|lyrics|library  -static

Requires Metal Toolchain (xcodebuild -downloadComponent MetalToolchain).
