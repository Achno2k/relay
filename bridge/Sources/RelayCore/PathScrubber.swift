import Foundation

/// Keeps full filesystem paths off the wire.
/// Paths under `cwd` become cwd-relative; any other absolute path keeps only its last component.
public struct PathScrubber: Sendable {
    public let cwd: String?

    public init(cwd: String?) {
        if let cwd, cwd.count > 1 {
            self.cwd = cwd.hasSuffix("/") ? String(cwd.dropLast()) : cwd
        } else {
            self.cwd = nil
        }
    }

    // Characters that end a path component.
    private static let stop = #"\s/"'`<>|;,()\[\]{}\\"#
    // An absolute (or ~-relative) path with at least two components, not part of a URL or relative path.
    private static let absolute = try! NSRegularExpression(
        pattern: #"(?<![A-Za-z0-9_.\-~/:@])~?/(?:[^STOP]+/)+[^STOP]*"#.replacingOccurrences(of: "STOP", with: stop))

    public func scrub(_ text: String) -> String {
        var s = text
        if let cwd, s.contains(cwd) {
            let escaped = NSRegularExpression.escapedPattern(for: cwd)
            // `<cwd>/x` -> `x`, bare `<cwd>` -> `.`
            s = Self.replace(in: s, pattern: escaped + "/(?=[^" + Self.stop + "])", with: { _ in "" })
            s = Self.replace(in: s, pattern: escaped + #"(?![A-Za-z0-9_.\-])"#, with: { _ in "." })
        }
        return Self.replace(in: s, regex: Self.absolute) { match in
            Self.lastComponent(match)
        }
    }

    public static func lastComponent(_ path: String) -> String {
        let parts = path.split(separator: "/", omittingEmptySubsequences: true)
        guard let last = parts.last else { return path }
        return last == "~" ? "~" : String(last)
    }

    private static func replace(in s: String, pattern: String, with f: (String) -> String) -> String {
        guard let re = try? NSRegularExpression(pattern: pattern) else { return s }
        return replace(in: s, regex: re, with: f)
    }

    private static func replace(in s: String, regex: NSRegularExpression, with f: (String) -> String) -> String {
        let ns = s as NSString
        let matches = regex.matches(in: s, range: NSRange(location: 0, length: ns.length))
        guard !matches.isEmpty else { return s }
        var out = ""
        var cursor = 0
        for m in matches {
            out += ns.substring(with: NSRange(location: cursor, length: m.range.location - cursor))
            out += f(ns.substring(with: m.range))
            cursor = m.range.location + m.range.length
        }
        out += ns.substring(from: cursor)
        return out
    }
}

public enum CwdName {
    /// Last component of a cwd; never the full path.
    public static func of(_ cwd: String?) -> String {
        guard let cwd, !cwd.isEmpty else { return "" }
        return PathScrubber.lastComponent(cwd)
    }
}
