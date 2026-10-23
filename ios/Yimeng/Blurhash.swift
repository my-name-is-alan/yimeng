import Foundation
import CoreGraphics
import ImageIO

/// Minimal BlurHash decoder (https://blurha.sh, MIT algorithm). 32×32 output, cached — it's a placeholder only.
enum Blurhash {
    private static let chars = Array("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz#$%*+,-.:;=?@[]^_{|}~")
    private static let map: [Character: Int] = Dictionary(uniqueKeysWithValues: chars.enumerated().map { ($1, $0) })
    private static let cache = NSCache<NSString, PlatformImage>()

    static func image(_ hash: String, size: Int = 32) -> PlatformImage? {
        if let c = cache.object(forKey: hash as NSString) { return c }
        guard let img = decode(hash, w: size, h: size) else { return nil }
        cache.setObject(img, forKey: hash as NSString)
        return img
    }
    private static func b83(_ s: Substring) -> Int? { var v = 0; for c in s { guard let d = map[c] else { return nil }; v = v * 83 + d }; return v }
    private static func s2l(_ v: Int) -> Float { let x = Float(v) / 255; return x <= 0.04045 ? x / 12.92 : powf((x + 0.055) / 1.055, 2.4) }
    private static func l2s(_ v: Float) -> UInt8 {
        let x = max(0, min(1, v)); let s = x <= 0.0031308 ? x * 12.92 : 1.055 * powf(x, 1 / 2.4) - 0.055
        return UInt8(max(0, min(255, s * 255 + 0.5)))
    }
    private static func signPow(_ v: Float, _ e: Float) -> Float { copysignf(powf(abs(v), e), v) }

    static func decode(_ hash: String, w: Int, h: Int) -> PlatformImage? {
        let s = Array(hash)
        guard s.count >= 6, let size = b83(Substring(String(s[0]))) else { return nil }
        let ny = size / 9 + 1, nx = size % 9 + 1
        guard s.count == 4 + 2 * nx * ny, let qmax = b83(Substring(String(s[1]))) else { return nil }
        let maxV = Float(qmax + 1) / 166
        var colors: [(Float, Float, Float)] = []
        for i in 0..<(nx * ny) {
            if i == 0 {
                guard let v = b83(Substring(String(s[2...5]))) else { return nil }
                colors.append((s2l(v >> 16), s2l((v >> 8) & 255), s2l(v & 255)))
            } else {
                let a = 4 + i * 2
                guard let v = b83(Substring(String(s[a...a + 1]))) else { return nil }
                let r = v / (19 * 19), g = (v / 19) % 19, b = v % 19
                colors.append((signPow((Float(r) - 9) / 9, 2) * maxV, signPow((Float(g) - 9) / 9, 2) * maxV, signPow((Float(b) - 9) / 9, 2) * maxV))
            }
        }
        var px = [UInt8](repeating: 255, count: w * h * 4)
        for y in 0..<h { for x in 0..<w {
            var r: Float = 0, g: Float = 0, b: Float = 0
            for j in 0..<ny { for i in 0..<nx {
                let basis = cosf(.pi * Float(x * i) / Float(w)) * cosf(.pi * Float(y * j) / Float(h))
                let c = colors[i + j * nx]; r += c.0 * basis; g += c.1 * basis; b += c.2 * basis
            } }
            let o = 4 * (x + y * w); px[o] = l2s(r); px[o + 1] = l2s(g); px[o + 2] = l2s(b)
        } }
        guard let prov = CGDataProvider(data: Data(px) as CFData),
              let cg = CGImage(width: w, height: h, bitsPerComponent: 8, bitsPerPixel: 32, bytesPerRow: w * 4, space: CGColorSpaceCreateDeviceRGB(),
                               bitmapInfo: CGBitmapInfo(rawValue: CGImageAlphaInfo.noneSkipLast.rawValue), provider: prov, decode: nil, shouldInterpolate: true, intent: .defaultIntent)
        else { return nil }
        return PlatformImage.from(cgImage: cg)
    }
}
