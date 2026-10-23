import SwiftUI

/// Size-aware layout shared by Home / library / album / artist / playlist. iPhone keeps the original values;
/// the iPad content column publishes its width here (and re-renders on bucket change), so grids, covers and margins scale.
enum YMLayout {
    nonisolated(unsafe) static var pad = false
    nonisolated(unsafe) static var width: CGFloat = 390
    static var margin: CGFloat { pad ? (width > 760 ? 44 : width > 560 ? 32 : 24) : 20 }
    /// Grid columns for album / playlist / artist tiles.
    static var columns: Int { pad ? max(3, min(6, Int((width - margin * 2) / 200))) : 2 }
    static func grid(spacing: CGFloat = 16) -> [GridItem] { Array(repeating: GridItem(.flexible(), spacing: spacing), count: columns) }
    /// Grid tile edge, sized to the content column (iPad) or the screen (iPhone).
    @MainActor static func tile(spacing: CGFloat = 16) -> CGFloat {
        let w = pad ? width : YMPlatform.screenWidth
        let n = CGFloat(columns)
        return ((w - margin * 2 - spacing * (n - 1)) / n).rounded(.down)
    }
    /// Horizontal-row cover size.
    static func cover(_ base: CGFloat) -> CGFloat { pad ? (base * (width > 760 ? 1.45 : 1.25)).rounded() : base }
    /// Columns for Home cards (daily picks, FM): 1 on iPhone, 2–3 on iPad.
    static var cardColumns: Int { wide ? (width >= 1100 ? 3 : 2) : 1 }
    /// Content column wide enough for side-by-side Home cards (sized to the content column, not the screen).
    static var wide: Bool { pad && width >= 700 }
    /// Bucket that changes when any of the above would change (used as a view id to re-layout).
    static func bucket(_ w: CGFloat) -> Int { Int(w / 80) }
}
