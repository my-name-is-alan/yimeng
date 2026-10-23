import WidgetKit
import SwiftUI
import UIKit
import ActivityKit

@main
struct YimengWidgetsBundle: WidgetBundle {
    var body: some Widget { NowPlayingWidget(); CardWidget(); FMWidget(); YimengLiveActivity() }
}

private let lime = Color(red: 0.784, green: 0.961, blue: 0.259)

struct CoverThumb: View {
    let name: String
    var size: CGFloat
    var radius: CGFloat = 12
    var circle = false
    @ViewBuilder
    var body: some View {
        let content = Group {
            if let ui = UIImage(named: name)?.preparingThumbnail(of: CGSize(width: max(size * 3, 64), height: max(size * 3, 64))) ?? UIImage(named: name) {
                Image(uiImage: ui).resizable().scaledToFill()
            } else {
                Color.gray.opacity(0.35)
            }
        }
        .frame(width: size, height: size)
        if circle { content.clipShape(Circle()) }
        else { content.clipShape(RoundedRectangle(cornerRadius: radius, style: .continuous)) }
    }
}

struct YimengLiveActivity: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: YimengActivityAttributes.self) { context in
            // Lock screen / banner
            HStack(spacing: 12) {
                CoverThumb(name: context.state.cover, size: 52, radius: 12)
                VStack(alignment: .leading, spacing: 4) {
                    Text(context.state.title).font(.system(size: 15, weight: .semibold))
                    Text(context.state.artist).font(.system(size: 12)).foregroundStyle(.secondary)
                    ProgressBlock(state: context.state)
                }
                Image(systemName: context.state.isPlaying ? "pause.fill" : "play.fill").font(.system(size: 22))
            }
            .padding(16)
            .activityBackgroundTint(Color.black.opacity(0.55))
            .activitySystemActionForegroundColor(.white)
        } dynamicIsland: { context in
            DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    CoverThumb(name: context.state.cover, size: 44, radius: 10)
                }
                DynamicIslandExpandedRegion(.trailing) {
                    Image(systemName: "waveform").font(.system(size: 20)).foregroundStyle(lime)
                }
                DynamicIslandExpandedRegion(.center) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(context.state.title).font(.system(size: 15, weight: .semibold)).lineLimit(1)
                        Text(context.state.artist).font(.system(size: 12)).foregroundStyle(.secondary).lineLimit(1)
                    }.frame(maxWidth: .infinity, alignment: .leading)
                }
                DynamicIslandExpandedRegion(.bottom) {
                    VStack(spacing: 10) {
                        ProgressBlock(state: context.state)
                        HStack(spacing: 36) {
                            Image(systemName: "backward.fill")
                            Image(systemName: context.state.isPlaying ? "pause.fill" : "play.fill").font(.system(size: 26))
                            Image(systemName: "forward.fill")
                        }.font(.system(size: 20))
                    }
                }
            } compactLeading: {
                CoverThumb(name: context.state.cover, size: 22, circle: true)
            } compactTrailing: {
                Image(systemName: "waveform").foregroundStyle(lime)
            } minimal: {
                CoverThumb(name: context.state.cover, size: 22, circle: true)
            }
            .keylineTint(lime)
        }
    }
}

struct ProgressBlock: View {
    let state: YimengActivityAttributes.ContentState
    var body: some View {
        HStack(spacing: 8) {
            Text(timerInterval: state.startDate...state.endDate, countsDown: false).font(.system(size: 11)).monospacedDigit().frame(width: 36)
            ProgressView(timerInterval: state.startDate...state.endDate, countsDown: false) { EmptyView() } currentValueLabel: { EmptyView() }
                .tint(.white)
            Text(Self.fmt(state.duration)).font(.system(size: 11)).monospacedDigit()
        }
    }
    static func fmt(_ t: TimeInterval) -> String { let s = Int(t); return "\(s / 60):" + String(format: "%02d", s % 60) }
}
