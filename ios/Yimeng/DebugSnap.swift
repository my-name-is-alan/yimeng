#if DEBUG
import UIKit

/// `-snap <name> -snapDelay <s>`: render the key window to Documents/<name>.png (simctl io is unreliable headless).
enum DebugSnap {
    static func schedule() {
        let a = ProcessInfo.processInfo.arguments
        func arg(_ k: String) -> String? { a.firstIndex(of: k).flatMap { $0 + 1 < a.count ? a[$0 + 1] : nil } }
        guard let name = arg("-snap") else { return }
        let delay = Double(arg("-snapDelay") ?? "") ?? 12
        DispatchQueue.main.asyncAfter(deadline: .now() + delay) {
            guard let w = UIApplication.shared.connectedScenes.compactMap({ $0 as? UIWindowScene }).flatMap(\.windows).first(where: \.isKeyWindow) else { return }
            let img = UIGraphicsImageRenderer(bounds: w.bounds).image { _ in w.drawHierarchy(in: w.bounds, afterScreenUpdates: true) }
            let url = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask)[0].appendingPathComponent(name + ".png")
            try? img.pngData()?.write(to: url)
        }
    }
}
#endif
