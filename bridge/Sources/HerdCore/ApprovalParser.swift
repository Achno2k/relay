import Foundation

/// Reads a blocked agent's screen (herdr `agent.read` detection/visible text) into an `Approval`.
///
/// Looks for the last run of numbered options `❯ 1. Yes`, `2. ...`, `3. No, ... (esc)` and the
/// question line above it. Option N maps to keys `["N"]`; a trailing "No" rendered with an
/// `(esc)` hint maps to `["esc"]`.
public enum ApprovalParser {
    private static let option = try! NSRegularExpression(pattern: #"^(?:[❯›>▶]\s*)?(\d{1,2})[.)]\s+(\S.*?)\s*$"#)
    private static let hint = try! NSRegularExpression(pattern: #"\s*\((esc|tab|shift\+tab|ctrl\+[a-z])\)\s*$"#, options: [.caseInsensitive])
    private static let frame = CharacterSet(charactersIn: "│┃║|╭╮╰╯┌┐└┘").union(.whitespaces)
    private static let rule = CharacterSet(charactersIn: "─━═-–—╌┄ ")

    public static func parse(_ screen: String, agentId: String, scrubber: PathScrubber = PathScrubber(cwd: nil)) -> Approval? {
        let lines = screen.components(separatedBy: "\n").map { $0.trimmingCharacters(in: frame) }

        var parsed: [(line: Int, n: Int, label: String)] = []
        for (i, line) in lines.enumerated() {
            let ns = line as NSString
            guard let m = option.firstMatch(in: line, range: NSRange(location: 0, length: ns.length)),
                  let n = Int(ns.substring(with: m.range(at: 1)))
            else { continue }
            parsed.append((i, n, ns.substring(with: m.range(at: 2))))
        }

        // Walk back from the last option: N, N-1, ..., 1.
        guard let last = parsed.last, last.n >= 1 else { return nil }
        var run = [last]
        for candidate in parsed.dropLast().reversed() {
            guard let expected = run.last.map({ $0.n - 1 }), expected >= 1 else { break }
            if candidate.n == expected { run.append(candidate) } else { break }
        }
        guard run.last?.n == 1 else { return nil }
        run.reverse()

        var options: [ApprovalOption] = []
        for (idx, o) in run.enumerated() {
            var label = o.label
            var escHint = false
            let ns = label as NSString
            if let h = hint.firstMatch(in: label, range: NSRange(location: 0, length: ns.length)) {
                escHint = ns.substring(with: h.range(at: 1)).lowercased() == "esc"
                label = ns.substring(to: h.range.location)
            }
            let isTrailingNo = idx == run.count - 1 && label.lowercased().hasPrefix("no")
            let keys = isTrailingNo && escHint ? ["esc"] : [String(o.n)]
            options.append(ApprovalOption(label: scrubber.scrub(label), keys: keys))
        }

        return Approval(agentId: agentId, question: scrubber.scrub(question(above: run[0].line, in: lines)), options: options)
    }

    /// The nearest question line above the first option, else the nearest meaningful line.
    static func question(above index: Int, in lines: [String]) -> String {
        var fallback: String?
        var i = index - 1
        var looked = 0
        while i >= 0 && looked < 8 {
            let l = lines[i]
            i -= 1
            if l.isEmpty || l.unicodeScalars.allSatisfy(rule.contains) { continue }
            looked += 1
            if l.hasSuffix("?") { return l }
            if fallback == nil { fallback = l }
        }
        return fallback ?? "Waiting for your input"
    }

    /// Fallback when a blocked agent shows no numbered options: the last meaningful screen line.
    public static func fallback(_ screen: String, agentId: String, scrubber: PathScrubber = PathScrubber(cwd: nil)) -> Approval {
        let lines = screen.components(separatedBy: "\n").map { $0.trimmingCharacters(in: frame) }
        return Approval(agentId: agentId, question: scrubber.scrub(question(above: lines.count, in: lines)), options: [])
    }
}
