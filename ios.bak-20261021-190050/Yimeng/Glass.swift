import SwiftUI

/// Glass chrome drawn on top of refracted content: very light tint + light blur + specular rim. No thick border.
struct GlassCapsule: View {
    @Environment(\.colorScheme) var scheme
    var body: some View {
        if #available(iOS 26.0, *) {
            // iOS 26+: system Liquid Glass (real lensing of the live backdrop, incl. UIKit-backed scroll views).
            Color.clear.glassEffect(.clear, in: Capsule())
                .overlay(Capsule().fill(scheme == .dark ? Color.black.opacity(0.10) : Color.white.opacity(0.2)).allowsHitTesting(false))
        } else {
            custom
        }
    }
    /// iOS 17–25 fallback (and the reference for Android AGSL): light blur + tint + Metal specular rim.
    var custom: some View {
        GeometryReader { g in
            ZStack {
                Capsule().fill(.ultraThinMaterial).opacity(0.35)
                Capsule().fill(scheme == .dark ? Color.black.opacity(0.14) : Color.white.opacity(0.38))
                Capsule().fill(RadialGradient(colors: [.white.opacity(scheme == .dark ? 0.14 : 0.3), .clear], center: UnitPoint(x: 0.3, y: 0), startRadius: 0, endRadius: g.size.width * 0.6))
                Rectangle().fill(.white)
                    .colorEffect(ShaderLibrary.glassRim(.float2(g.size), .float(scheme == .dark ? 0.95 : 0.8)))
                    .allowsHitTesting(false)
            }
        }
        .compositingGroup()
        .shadow(color: .black.opacity(scheme == .dark ? 0.3 : 0.12), radius: 14, y: 6)
    }
}

/// Collects the frames of glass chrome (in the shell's coordinate space) so the content layer can refract under them.
struct GlassFrameKey: PreferenceKey {
    static var defaultValue: [CGRect] = []
    static func reduce(value: inout [CGRect], nextValue: () -> [CGRect]) { value += nextValue() }
}
extension View {
    func glassFrame() -> some View {
        background(GeometryReader { g in Color.clear.preference(key: GlassFrameKey.self, value: [g.frame(in: .named("shell"))]) })
    }
    func glassLens(_ rects: [CGRect], bezel: CGFloat = 14, strength: CGFloat = 16) -> some View {
        func v(_ i: Int) -> Shader.Argument {
            i < rects.count ? .float4(rects[i].minX, rects[i].minY, rects[i].width, rects[i].height) : .float4(0, 0, 0, 0)
        }
        return layerEffect(ShaderLibrary.glassLens(v(0), v(1), v(2), .float(bezel), .float(strength)), maxSampleOffset: CGSize(width: strength * 1.2, height: strength * 1.2))
    }
}
