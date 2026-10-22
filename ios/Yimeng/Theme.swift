import SwiftUI

enum Theme {
    static let lime = Color(red: 0.784, green: 0.961, blue: 0.259)       // #C8F542
    static let limeLight = Color(red: 0.31, green: 0.42, blue: 0.0)       // #4F6B00
    /// Side margin: 20 on iPhone; wider on iPad (set by the iPad content column, see YMLayout).
    static var margin: CGFloat { YMLayout.margin }
    static func accent(_ s: ColorScheme) -> Color { s == .dark ? lime : limeLight }
    static func base(_ s: ColorScheme) -> Color { s == .dark ? Color(red: 0.039, green: 0.039, blue: 0.047) : .white }
    static func fg(_ s: ColorScheme) -> Color { s == .dark ? .white : Color(white: 0.07) }
    static func secondary(_ s: ColorScheme) -> Color { s == .dark ? Color(white: 1, opacity: 0.6) : Color(white: 0, opacity: 0.5) }
    static func fill(_ s: ColorScheme) -> Color { s == .dark ? Color(white: 1, opacity: 0.08) : Color(white: 0, opacity: 0.06) }
    static func divider(_ s: ColorScheme) -> Color { s == .dark ? Color(white: 1, opacity: 0.06) : Color(white: 0, opacity: 0.08) }
}

/// Primary action: white filled in dark, near-black filled in light (no lime, no glass).
struct PrimaryButtonStyle: ButtonStyle {
    @Environment(\.colorScheme) var scheme
    func makeBody(configuration: Configuration) -> some View {
        configuration.label.font(.system(size: 16, weight: .semibold))
            .foregroundStyle(scheme == .dark ? Color.black : .white)
            .frame(maxWidth: .infinity, minHeight: 48)
            .background(Capsule().fill(scheme == .dark ? Color.white : Color(white: 0.07)))
            .opacity(configuration.isPressed ? 0.8 : 1)
    }
}
struct SecondaryButtonStyle: ButtonStyle {
    @Environment(\.colorScheme) var scheme
    func makeBody(configuration: Configuration) -> some View {
        configuration.label.font(.system(size: 16, weight: .semibold))
            .foregroundStyle(Theme.fg(scheme))
            .frame(maxWidth: .infinity, minHeight: 48)
            .background(Capsule().fill(Theme.fill(scheme)))
            .opacity(configuration.isPressed ? 0.7 : 1)
    }
}
struct CircleIconButton: View {
    let icon: String; var size: CGFloat = 44; var dark = false
    @Environment(\.colorScheme) var scheme
    var body: some View {
        Image(systemName: icon).font(.system(size: 17, weight: .semibold))
            .foregroundStyle(dark ? .white : Theme.fg(scheme))
            .frame(width: size, height: size)
            .background(Circle().fill(dark ? Color.black.opacity(0.28) : Theme.fill(scheme)))
    }
}
struct PlayCircle: View {
    var size: CGFloat = 48
    var body: some View {
        Image(systemName: "play.fill").font(.system(size: size * 0.4)).foregroundStyle(.black)
            .frame(width: size, height: size).background(Circle().fill(.white))
    }
}
