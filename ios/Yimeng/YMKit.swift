import SwiftUI

// MARK: - YM settings kit — the exact 我的 tokens (Theme + dashed doodle card), shared by every settings-like screen.
// Never the system Form / grouped List look. Liquid glass is NOT used here (tab bar + mini player only).

extension Color {
    /// "#rrggbb" (Core cover colours) → Color.
    init?(hexString s: String?) {
        guard var h = s?.trimmingCharacters(in: .whitespaces), !h.isEmpty else { return nil }
        if h.hasPrefix("#") { h.removeFirst() }
        guard h.count == 6, let v = UInt32(h, radix: 16) else { return nil }
        self.init(hex: v)
    }
}

/// Page backdrop: app base colour with the now-playing cover's colour bleeding in from the top (same ambience as Home).
struct YMBackdrop: View {
    @Environment(\.colorScheme) var scheme
    @ObservedObject private var player = PlayerStore.shared
    var body: some View {
        ZStack(alignment: .top) {
            Theme.base(scheme)
            if let u = player.coverURL { RemoteBackdrop(urls: [u], height: 420).opacity(scheme == .dark ? 0.55 : 0.35) }
        }
        .ignoresSafeArea()
    }
}

/// Settings page: small centred title, quiet circle close/back button, scrolling doodle cards on the ambient backdrop.
struct YMPage<Content: View, Trailing: View>: View {
    let title: String
    var back = false
    @ViewBuilder var trailing: () -> Trailing
    @ViewBuilder var content: () -> Content
    @Environment(\.dismiss) private var dismiss
    @Environment(\.colorScheme) var scheme
    init(_ title: String, back: Bool = false, @ViewBuilder trailing: @escaping () -> Trailing, @ViewBuilder content: @escaping () -> Content) {
        self.title = title; self.back = back; self.trailing = trailing; self.content = content
    }
    var body: some View {
        VStack(spacing: 0) {
            ZStack {
                Text(title).font(.system(size: 17, weight: .semibold)).lineLimit(1).truncationMode(.middle).padding(.horizontal, 60)
                HStack {
                    Button { dismiss() } label: { CircleIconButton(icon: back ? "chevron.left" : "xmark", size: 36) }
                        .buttonStyle(.plain).accessibilityLabel(back ? "返回" : "关闭")
                    Spacer()
                    trailing()
                }
            }
            .padding(.horizontal, Theme.margin).padding(.top, 12).padding(.bottom, 6)
            ScrollView(showsIndicators: false) {
                VStack(alignment: .leading, spacing: 0) { content() }
                    .frame(maxWidth: 680).frame(maxWidth: .infinity)
                    .padding(.horizontal, Theme.margin).padding(.bottom, 40)
            }
        }
        .foregroundStyle(Theme.fg(scheme))
        .background(YMBackdrop())
        .toolbar(.hidden, for: .navigationBar)
    }
}
extension YMPage where Trailing == EmptyView {
    init(_ title: String, back: Bool = false, @ViewBuilder content: @escaping () -> Content) {
        self.init(title, back: back, trailing: { EmptyView() }, content: content)
    }
}

/// The 我的 doodle card: fill + dashed hairline, 20 pt corners.
struct YMCard: ViewModifier {
    @Environment(\.colorScheme) var scheme
    var padding: CGFloat = 16
    func body(content: Content) -> some View {
        content.padding(.horizontal, padding).padding(.vertical, 4)
            .background(RoundedRectangle(cornerRadius: 20, style: .continuous).fill(Theme.fill(scheme))
                .overlay(RoundedRectangle(cornerRadius: 20, style: .continuous)
                    .strokeBorder(Theme.fg(scheme).opacity(0.08), style: StrokeStyle(lineWidth: 1, dash: [6, 4]))))
    }
}
extension View { func ymCard(padding: CGFloat = 16) -> some View { modifier(YMCard(padding: padding)) } }

struct YMSection<Content: View>: View {
    var title: String? = nil
    var footer: String? = nil
    @ViewBuilder var content: () -> Content
    @Environment(\.colorScheme) var scheme
    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            if let title { Text(title).font(.system(size: 13, weight: .semibold)).foregroundStyle(Theme.secondary(scheme)).padding(.leading, 4) }
            VStack(spacing: 0) { content() }.ymCard()
            if let footer { Text(footer).font(.system(size: 12)).foregroundStyle(Theme.secondary(scheme)).padding(.horizontal, 4).fixedSize(horizontal: false, vertical: true) }
        }
        .padding(.top, 20)
    }
}

struct YMDivider: View {
    @Environment(\.colorScheme) var scheme
    var body: some View { Rectangle().fill(Theme.divider(scheme)).frame(height: 0.5) }
}

/// Settings-row leading mark — doodle glyph when we know the SF name, else the SF Symbol as a last resort.
struct YMLeadingIcon: View {
    let name: String
    var body: some View {
        Group {
            if let g = YMGlyph.from(systemName: name) { YMIcon(glyph: g, size: 18, weight: 1.6) }
            else { Image(systemName: name).font(.system(size: 16, weight: .medium)) }
        }.frame(width: 28)
    }
}

struct YMRow<Accessory: View>: View {
    var icon: String? = nil
    let title: String
    var detail: String = ""
    var chevron = false
    var detailColor: Color? = nil
    @ViewBuilder var accessory: () -> Accessory
    @Environment(\.colorScheme) var scheme
    var body: some View {
        HStack(spacing: 14) {
            if let icon { YMLeadingIcon(name: icon) }
            Text(title).font(.system(size: 16)).lineLimit(1).truncationMode(.middle).layoutPriority(1)
            Spacer(minLength: 8)
            if !detail.isEmpty { Text(detail).font(.system(size: 13)).foregroundStyle(detailColor ?? Theme.secondary(scheme)).lineLimit(1).truncationMode(.middle) }
            accessory()
            if chevron { YMIcon(glyph: .chevron, size: 11, weight: 1.6).foregroundStyle(Theme.secondary(scheme)) }
        }
        .frame(minHeight: 50).contentShape(Rectangle())
    }
}
extension YMRow where Accessory == EmptyView {
    init(icon: String? = nil, _ title: String, _ detail: String = "", chevron: Bool = false, detailColor: Color? = nil) {
        self.icon = icon; self.title = title; self.detail = detail; self.chevron = chevron; self.detailColor = detailColor; self.accessory = { EmptyView() }
    }
}

/// Doodle switch: dashed pencil track when off; lime fill with a crayon squiggle when on; hand-drawn knob.
struct YMToggleStyle: ToggleStyle {
    @Environment(\.colorScheme) var scheme
    @Environment(\.isEnabled) var enabled
    func makeBody(configuration c: Configuration) -> some View {
        HStack(spacing: 12) {
            c.label
            Spacer(minLength: 8)
            ZStack(alignment: c.isOn ? .trailing : .leading) {
                Capsule().fill(c.isOn ? Theme.accent(scheme) : Theme.fill(scheme))
                    .overlay(Capsule().strokeBorder(Theme.fg(scheme).opacity(c.isOn ? 0 : 0.25), style: StrokeStyle(lineWidth: 1.2, dash: [3, 3])))
                if c.isOn {
                    WaveShape().stroke(Color.black.opacity(0.35), style: StrokeStyle(lineWidth: 1.4, lineCap: .round))
                        .frame(width: 16, height: 5).padding(.leading, 8).frame(maxWidth: .infinity, alignment: .leading)
                }
                Circle().fill(c.isOn ? Color.black.opacity(scheme == .dark ? 0.85 : 0.75) : Theme.fg(scheme).opacity(0.85))
                    .overlay(Circle().stroke(Theme.fg(scheme).opacity(0.25), lineWidth: 1).offset(x: 0.6, y: -0.5))
                    .frame(width: 22, height: 22).padding(3)
            }
            .frame(width: 50, height: 28)
            .opacity(enabled ? 1 : 0.4)
            .animation(.snappy(duration: 0.22), value: c.isOn)
        }
        .contentShape(Rectangle())
        .onTapGesture { c.isOn.toggle(); Haptics.tick() }
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isToggle)
        .accessibilityValue(c.isOn ? "1" : "0")
        .accessibilityAction { c.isOn.toggle() }
    }
}

struct YMToggleRow: View {
    var icon: String? = nil
    let title: String
    @Binding var isOn: Bool
    init(icon: String? = nil, _ title: String, isOn: Binding<Bool>) { self.icon = icon; self.title = title; self._isOn = isOn }
    var body: some View {
        Toggle(isOn: $isOn) {
            HStack(spacing: 14) { if let icon { YMLeadingIcon(name: icon) }; Text(title).font(.system(size: 16)) }
        }
        .toggleStyle(YMToggleStyle())
        .frame(minHeight: 50)
        .accessibilityIdentifier(title)
        .accessibilityLabel(title)
    }
}

/// Value picker: quiet capsule menu (same as 我的 › 串流音质).
struct YMPickerRow<V: Hashable>: View {
    var icon: String? = nil
    let title: String
    @Binding var selection: V
    let options: [(V, String)]
    @Environment(\.colorScheme) var scheme
    init(icon: String? = nil, _ title: String, selection: Binding<V>, options: [(V, String)]) {
        self.icon = icon; self.title = title; self._selection = selection; self.options = options
    }
    var body: some View {
        HStack(spacing: 14) {
            if let icon { YMLeadingIcon(name: icon) }
            Text(title).font(.system(size: 16))
            Spacer()
            Menu {
                ForEach(options, id: \.0) { o in Button(o.1) { selection = o.0 } }
            } label: {
                HStack(spacing: 4) {
                    Text(options.first { $0.0 == selection }?.1 ?? "")
                    YMIcon(glyph: .order, size: 10, weight: 1.4)
                }
                .font(.system(size: 14, weight: .medium)).foregroundStyle(Theme.fg(scheme))
                .padding(.horizontal, 12).frame(height: 30).background(Capsule().fill(Theme.fill(scheme)))
            }
        }.frame(minHeight: 50)
    }
}

/// Segmented choice as quiet capsules (lime-outlined when selected).
struct YMChips<V: Hashable>: View {
    @Binding var selection: V
    let options: [(V, String)]
    @Environment(\.colorScheme) var scheme
    var body: some View {
        HStack(spacing: 8) {
            ForEach(options, id: \.0) { o in
                let on = o.0 == selection
                Button { selection = o.0; Haptics.tick() } label: {
                    Text(o.1).font(.system(size: 14, weight: on ? .semibold : .medium))
                        .foregroundStyle(on ? (scheme == .dark ? Color.black : .white) : Theme.fg(scheme))
                        .padding(.horizontal, 14).frame(height: 32)
                        .background(Capsule().fill(on ? Theme.fg(scheme) : Theme.fill(scheme)))
                }.buttonStyle(.plain)
            }
            Spacer(minLength: 0)
        }.padding(.vertical, 10)
    }
}

struct YMSliderRow: View {
    var icon: String? = nil
    let title: String
    let value: String
    @Binding var v: Double
    let range: ClosedRange<Double>
    var step: Double = 1
    @Environment(\.colorScheme) var scheme
    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 14) {
                if let icon { YMLeadingIcon(name: icon) }
                Text(title).font(.system(size: 16)); Spacer()
                Text(value).font(.system(size: 13).monospacedDigit()).foregroundStyle(Theme.secondary(scheme))
            }
            Slider(value: $v, in: range, step: step).tint(Theme.accent(scheme))
        }.padding(.vertical, 12)
    }
}

/// Quiet solid action buttons. Primary = solid fg capsule (PrimaryButtonStyle); quiet = fill; destructive = fill + soft red.
struct YMDestructiveButtonStyle: ButtonStyle {
    @Environment(\.colorScheme) var scheme
    func makeBody(configuration: Configuration) -> some View {
        configuration.label.font(.system(size: 16, weight: .semibold))
            .foregroundStyle(Color(hex: 0xff6b6b))
            .frame(maxWidth: .infinity, minHeight: 48)
            .background(Capsule().fill(Theme.fill(scheme)))
            .opacity(configuration.isPressed ? 0.7 : 1)
    }
}
/// Small inline quiet pill (e.g. 清除, 全部已读).
struct YMPillButtonStyle: ButtonStyle {
    @Environment(\.colorScheme) var scheme
    var destructive = false
    func makeBody(configuration: Configuration) -> some View {
        configuration.label.font(.system(size: 14, weight: .semibold))
            .foregroundStyle(destructive ? Color(hex: 0xff6b6b) : Theme.fg(scheme))
            .padding(.horizontal, 14).frame(height: 32).background(Capsule().fill(Theme.fill(scheme)))
            .opacity(configuration.isPressed ? 0.7 : 1)
    }
}

/// Text field row inside a card.
struct YMFieldRow: View {
    let title: String
    let prompt: String
    @Binding var text: String
    var secure = false
    @Environment(\.colorScheme) var scheme
    var body: some View {
        HStack(spacing: 14) {
            Text(title).font(.system(size: 16)).fixedSize()
            Group {
                if secure { SecureField(prompt, text: $text) } else { TextField(prompt, text: $text) }
            }
            .multilineTextAlignment(.trailing).font(.system(size: 15))
            .textInputAutocapitalization(.never).autocorrectionDisabled()
        }.frame(minHeight: 50)
    }
}

/// Doodle usage ring: dashed pencil track + double crayon arc in lime.
struct YMDoodleRing: View {
    let fraction: Double
    var size: CGFloat = 132
    var lineWidth: CGFloat = 10
    @Environment(\.colorScheme) var scheme
    var body: some View {
        let f = max(0.004, min(1, fraction))
        ZStack {
            Circle().stroke(Theme.fg(scheme).opacity(0.18), style: StrokeStyle(lineWidth: 1.5, dash: [4, 6]))
            Circle().trim(from: 0, to: f).stroke(Theme.accent(scheme), style: StrokeStyle(lineWidth: lineWidth, lineCap: .round))
                .rotationEffect(.degrees(-90))
            Circle().trim(from: 0, to: f).stroke(Theme.accent(scheme).opacity(0.45), style: StrokeStyle(lineWidth: 1.6, lineCap: .round))
                .scaleEffect(1.12).rotationEffect(.degrees(-87))
            Path { p in p.addPath(DoodlePaths.sparkle(center: CGPoint(x: size * 0.86, y: size * 0.12), r: 7)) }
                .stroke(Theme.accent(scheme), lineWidth: 1.4)
        }
        .frame(width: size, height: size)
        .animation(.snappy, value: fraction)
    }
}
