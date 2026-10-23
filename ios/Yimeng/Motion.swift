import SwiftUI

/// Framer-Motion-style entrance. Runs once per view identity (push / tab switch / sheet), not on scroll.
enum MotionFrom { case bottom, scale, fade, top }

struct MotionIn: ViewModifier {
    var delay: Double = 0
    var from: MotionFrom = .bottom
    @Environment(\.accessibilityReduceMotion) private var reduce
    @State private var shown = false
    private var shotMac: Bool { ProcessInfo.processInfo.arguments.contains("-shotMac") }
    func body(content: Content) -> some View {
        content
            .opacity(shown || shotMac ? 1 : 0)
            .offset(y: reduce || shown || shotMac ? 0 : (from == .bottom ? 16 : from == .top ? -16 : 0))
            .scaleEffect(reduce || shown || shotMac ? 1 : (from == .scale ? 0.92 : 1))
            .onAppear {
                if shotMac { shown = true; return }
                guard !shown else { return }
                let anim: Animation = reduce ? .easeOut(duration: 0.25) :
                    (from == .scale ? .spring(response: 0.55, dampingFraction: 0.68) : .spring(response: 0.5, dampingFraction: 0.8))
                withAnimation(anim.delay(reduce ? 0 : delay)) { shown = true }
            }
    }
}

/// Scroll-linked: rows/cards gently fade+scale as they enter/leave the viewport (iOS 17 .scrollTransition).
struct ScrollReveal: ViewModifier {
    var axis: Axis = .vertical
    @Environment(\.accessibilityReduceMotion) private var reduce
    func body(content: Content) -> some View {
        if reduce { content } else {
            content.scrollTransition(axis: axis) { c, phase in
                c.opacity(phase.isIdentity ? 1 : 0.55)
                 .scaleEffect(phase.isIdentity ? 1 : 0.96)
            }
        }
    }
}

/// Blur → sharp entrance for lyric lines.
struct BlurIn: ViewModifier {
    var delay: Double = 0
    @Environment(\.accessibilityReduceMotion) private var reduce
    @State private var shown = false
    func body(content: Content) -> some View {
        content.blur(radius: shown || reduce ? 0 : 10).opacity(shown ? 1 : 0)
            .onAppear { withAnimation(.smooth(duration: 0.6).delay(delay)) { shown = true } }
    }
}

extension View {
    func motionIn(_ delay: Double = 0, from: MotionFrom = .bottom) -> some View { modifier(MotionIn(delay: delay, from: from)) }
    func scrollReveal(_ axis: Axis = .vertical) -> some View { modifier(ScrollReveal(axis: axis)) }
    func blurIn(_ delay: Double = 0) -> some View { modifier(BlurIn(delay: delay)) }
}

/// Stagger helper: base + index × 0.05s, capped so long lists don't drag.
func stagger(_ i: Int, base: Double = 0.15, step: Double = 0.05) -> Double { base + Double(min(i, 8)) * step }
