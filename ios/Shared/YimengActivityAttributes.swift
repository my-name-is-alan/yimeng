import Foundation
#if canImport(ActivityKit)
import ActivityKit

/// Shared between the app and the widget extension.
struct YimengActivityAttributes: ActivityAttributes {
    public struct ContentState: Codable, Hashable {
        var title: String
        var artist: String
        var cover: String
        var startDate: Date
        var duration: TimeInterval
        var isPlaying: Bool
        var endDate: Date { startDate.addingTimeInterval(duration) }
    }
    var app: String
}
#endif
