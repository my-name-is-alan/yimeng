import SwiftUI
#if canImport(UIKit)
import UIKit
public typealias PlatformImage = UIImage
#elseif canImport(AppKit)
import AppKit
public typealias PlatformImage = NSImage
#endif

enum YMPlatform {
    static var isPad: Bool {
        #if os(iOS)
        return UIDevice.current.userInterfaceIdiom == .pad
        #else
        return true
        #endif
    }
    static var screenWidth: CGFloat {
        #if os(iOS)
        return UIScreen.main.bounds.width
        #elseif os(macOS)
        return NSScreen.main?.frame.width ?? 1200
        #else
        return 390
        #endif
    }
    static var screenScale: CGFloat {
        #if os(iOS)
        return UIScreen.main.scale
        #elseif os(macOS)
        return NSScreen.main?.backingScaleFactor ?? 2
        #else
        return 2
        #endif
    }
}

extension PlatformImage {
    /// Cross-platform CGImage → PlatformImage.
    static func from(cgImage cg: CGImage) -> PlatformImage {
        #if canImport(UIKit)
        return UIImage(cgImage: cg)
        #elseif canImport(AppKit)
        return NSImage(cgImage: cg, size: NSSize(width: cg.width, height: cg.height))
        #endif
    }
    var ymCGImage: CGImage? {
        #if canImport(UIKit)
        return self.cgImage
        #elseif canImport(AppKit)
        var rect = CGRect(origin: .zero, size: self.size)
        return self.cgImage(forProposedRect: &rect, context: nil, hints: nil)
        #endif
    }
}

extension Image {
    init(platformImage img: PlatformImage) {
        #if canImport(UIKit)
        self.init(uiImage: img)
        #elseif canImport(AppKit)
        self.init(nsImage: img)
        #endif
    }
}
