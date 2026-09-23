import CoreImage
import Foundation

/// Terminal QR codes: CoreImage's generator, drawn with half-block characters.
public enum QRCode {
    /// Module matrix (true = dark), without quiet zone.
    public static func modules(for text: String) -> [[Bool]]? {
        guard let filter = CIFilter(name: "CIQRCodeGenerator") else { return nil }
        filter.setValue(Data(text.utf8), forKey: "inputMessage")
        filter.setValue("M", forKey: "inputCorrectionLevel")
        guard let image = filter.outputImage else { return nil }
        let w = Int(image.extent.width), h = Int(image.extent.height)
        var pixels = [UInt8](repeating: 0, count: w * h * 4)
        let ctx = CIContext(options: [.useSoftwareRenderer: true])
        ctx.render(image, toBitmap: &pixels, rowBytes: w * 4, bounds: image.extent, format: .RGBA8, colorSpace: CGColorSpaceCreateDeviceRGB())
        var rows: [[Bool]] = []
        for y in 0..<h {
            rows.append((0..<w).map { x in pixels[(y * w + x) * 4] < 128 })
        }
        // Trim CoreImage's own margin so we control the quiet zone.
        while let first = rows.first, !first.contains(true) { rows.removeFirst() }
        while let last = rows.last, !last.contains(true) { rows.removeLast() }
        guard !rows.isEmpty else { return nil }
        let left = rows.compactMap { $0.firstIndex(of: true) }.min() ?? 0
        let right = rows.compactMap { $0.lastIndex(of: true) }.max() ?? (w - 1)
        return rows.map { Array($0[left...right]) }
    }

    /// Black-on-white half-block rendering with a 2-module quiet zone, readable on dark terminals.
    public static func terminal(_ text: String, quiet: Int = 2) -> String? {
        guard let m = modules(for: text) else { return nil }
        let size = m[0].count + quiet * 2
        let blank = [Bool](repeating: false, count: size)
        var grid = [[Bool]](repeating: blank, count: quiet)
        for row in m {
            grid.append([Bool](repeating: false, count: quiet) + row + [Bool](repeating: false, count: quiet))
        }
        grid += [[Bool]](repeating: blank, count: quiet)
        if grid.count % 2 == 1 { grid.append(blank) }

        var out = ""
        for y in stride(from: 0, to: grid.count, by: 2) {
            out += "\u{1B}[30;107m"
            for x in 0..<size {
                switch (grid[y][x], grid[y + 1][x]) {
                case (true, true): out += "█"
                case (true, false): out += "▀"
                case (false, true): out += "▄"
                case (false, false): out += " "
                }
            }
            out += "\u{1B}[0m\n"
        }
        return out
    }
}
