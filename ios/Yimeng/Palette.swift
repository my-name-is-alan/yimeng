#if canImport(UIKit)
import UIKit
#endif
import CoreImage
import SwiftUI

/// Dominant-colour extraction: downsample to 24×24, k-means (k=5, deterministic luminance-quantile seeds),
/// score = count × (0.35 + saturation). Mirrors backdrop-algorithm.md.
struct Palette: Equatable {
    let dominant: (r: Double, g: Double, b: Double)
    static func == (a: Palette, b: Palette) -> Bool { a.dominant == b.dominant }
    var color: Color { Color(red: dominant.r / 255, green: dominant.g / 255, blue: dominant.b / 255) }
    /// Header foreground for a given base (mix 70/30 then WCAG luminance threshold).
    func headerIsLight(dark: Bool) -> Bool {
        let base: Double = dark ? 10 : 255
        let m = [dominant.r, dominant.g, dominant.b].map { $0 * 0.7 + base * 0.3 }
        let l = Palette.luminance(m[0], m[1], m[2])
        return dark ? l <= 0.5 : l < 0.36
    }
    static func luminance(_ r: Double, _ g: Double, _ b: Double) -> Double {
        func f(_ v: Double) -> Double { let v = v / 255; return v <= 0.03928 ? v / 12.92 : pow((v + 0.055) / 1.055, 2.4) }
        return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b)
    }

    private static var cache: [String: Palette] = [:]
    static func of(_ name: String) -> Palette {
        if let p = cache[name] { return p }
        let p = extract(UIImage(named: name)) ; cache[name] = p; return p
    }
    static func extract(_ image: UIImage?) -> Palette {
        guard let cg = image?.cgImage else { return Palette(dominant: (40, 30, 60)) }
        let n = 24; var buf = [UInt8](repeating: 0, count: n * n * 4)
        let ctx = CGContext(data: &buf, width: n, height: n, bitsPerComponent: 8, bytesPerRow: n * 4,
                            space: CGColorSpaceCreateDeviceRGB(), bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!
        ctx.interpolationQuality = .medium
        ctx.draw(cg, in: CGRect(x: 0, y: 0, width: n, height: n))
        var px: [[Double]] = []
        for i in stride(from: 0, to: buf.count, by: 4) { px.append([Double(buf[i]), Double(buf[i + 1]), Double(buf[i + 2])]) }
        let k = 5
        let sorted = px.sorted { luminance($0[0], $0[1], $0[2]) < luminance($1[0], $1[1], $1[2]) }
        var c = (0..<k).map { sorted[Int((Double($0) + 0.5) / Double(k) * Double(sorted.count))] }
        var cnt = [Int](repeating: 0, count: k)
        for _ in 0..<12 {
            var sum = [[Double]](repeating: [0, 0, 0], count: k); cnt = [Int](repeating: 0, count: k)
            for p in px {
                var best = 0; var bd = Double.greatestFiniteMagnitude
                for j in 0..<k { let d = pow(p[0] - c[j][0], 2) + pow(p[1] - c[j][1], 2) + pow(p[2] - c[j][2], 2); if d < bd { bd = d; best = j } }
                cnt[best] += 1; for ch in 0..<3 { sum[best][ch] += p[ch] }
            }
            for j in 0..<k where cnt[j] > 0 { c[j] = sum[j].map { $0 / Double(cnt[j]) } }
        }
        var bestJ = 0; var bestS = -1.0
        for j in 0..<k {
            let mx = c[j].max()!, mn = c[j].min()!; let sat = mx > 0 ? (mx - mn) / mx : 0
            let s = Double(cnt[j]) * (0.35 + sat); if s > bestS { bestS = s; bestJ = j }
        }
        return Palette(dominant: (c[bestJ][0], c[bestJ][1], c[bestJ][2]))
    }
}

/// Pre-rendered, cached diffused layer (downsample → blur → saturate) so nothing is blurred per frame.
enum BackdropCache {
    private static var cache: [String: UIImage] = [:]
    private static let ctx = CIContext(options: [.useSoftwareRenderer: false])
    static func image(_ name: String) -> UIImage {
        if let i = cache[name] { return i }
        guard let src = UIImage(named: name), let cg = src.cgImage else { return UIImage() }
        let ci = CIImage(cgImage: cg)
        let scale = 96 / max(ci.extent.width, ci.extent.height)
        let small = ci.transformed(by: .init(scaleX: scale, y: scale))
        let sat = small.applyingFilter("CIColorControls", parameters: [kCIInputSaturationKey: 1.8])
        let blur = sat.clampedToExtent().applyingFilter("CIGaussianBlur", parameters: [kCIInputRadiusKey: 10]).cropped(to: small.extent)
        let out = ctx.createCGImage(blur, from: small.extent).map { UIImage(cgImage: $0) } ?? src
        cache[name] = out; return out
    }
    static func warm(_ names: [String]) { DispatchQueue.global(qos: .userInitiated).async { for n in names { _ = Palette.of(n); _ = image(n) } } }
}

/// Diffused backdrop: dominant colour + cached blurred cover (scaled 2.6×), faded to base. Weights follow continuous progress (mod count).
struct DiffusedBackdrop: View {
    let images: [String]
    var progress: Double = 0
    var height: CGFloat = 640
    @Environment(\.colorScheme) var scheme
    var body: some View {
        GeometryReader { geo in
            ZStack {
                ForEach(Array(images.enumerated()), id: \.offset) { i, name in
                    let w = Self.weight(i, progress, images.count)
                    if w > 0.001 {
                        ZStack {
                            Palette.of(name).color
                            Image(uiImage: BackdropCache.image(name)).resizable().interpolation(.medium).scaledToFill()
                                .frame(width: geo.size.width, height: geo.size.width)
                                .scaleEffect(2.6)
                                .position(x: geo.size.width / 2, y: height * 0.4)
                        }
                        .opacity(w)
                    }
                }
                LinearGradient(stops: [.init(color: Theme.base(scheme).opacity(0), location: 0),
                                       .init(color: Theme.base(scheme).opacity(0.13), location: 0.25),
                                       .init(color: Theme.base(scheme).opacity(0.67), location: 0.62),
                                       .init(color: Theme.base(scheme), location: 1)], startPoint: .top, endPoint: .bottom)
            }
            .frame(width: geo.size.width, height: height).clipped()
            .drawingGroup()
        }
        .frame(height: height).allowsHitTesting(false)
    }
    /// Circular weight so the last→first wrap blends seamlessly.
    static func weight(_ i: Int, _ p: Double, _ n: Int) -> Double {
        guard n > 1 else { return 1 }
        let m = (p.truncatingRemainder(dividingBy: Double(n)) + Double(n)).truncatingRemainder(dividingBy: Double(n))
        var d = abs(m - Double(i)); d = min(d, Double(n) - d)
        return max(0, 1 - d)
    }
}

extension Color {
    static func header(_ images: [String], progress: Double, dark: Bool) -> Color {
        var acc = 0.0, tw = 0.0
        for (i, n) in images.enumerated() { let w = DiffusedBackdrop.weight(i, progress, images.count); acc += (Palette.of(n).headerIsLight(dark: dark) ? 1 : 0.07) * w; tw += w }
        return Color(white: tw > 0 ? acc / tw : 1)
    }
}
