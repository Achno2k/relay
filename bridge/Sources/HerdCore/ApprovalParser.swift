import Foundation

/// Reads a blocked agent's screen (herdr `agent.read` detection/visible text) into an `Approval`.
///
/// Two menu shapes:
/// - Numbered: the last run of `❯ 1. Yes`, `2. ...`, `3. No, ... (esc)`. Option N maps to keys `["N"]`;
///   a trailing "No" rendered with an `(esc)` hint maps to `["esc"]`. Claude's "Type something." row is
///   `freeText` and is reached with arrow keys, because pressing its number also types the digit.
/// - Cursor: unnumbered lines around a `❯` line (e.g. Claude's folder-trust prompt). Keys are arrow
///   moves relative to the `❯` line, then Enter.
public enum ApprovalParser {
    private static let option = try! NSRegularExpression(pattern: #"^([❯›>▶]\s*)?(\d{1,2})[.)]\s+(\S.*?)\s*$"#)
    private static let cursorLine = try! NSRegularExpression(pattern: #"^[❯›▶]\s+(\S.*?)\s*$"#)
    private static let hint = try! NSRegularExpression(pattern: #"\s*\((esc|tab|shift\+tab|ctrl\+[a-z])\)\s*$"#, options: [.caseInsensitive])
    private static let frame = CharacterSet(charactersIn: "│┃║|╭╮╰╯┌┐└┘").union(.whitespaces)
    private static let rule = CharacterSet(charactersIn: "─━═-–—╌┄ ")
    static let freeTextLabel = "Type something."

    public static func parse(
        _ screen: String,
        agentId: String,
        scrubber: PathScrubber = PathScrubber(cwd: nil),
        cwdName: String? = nil
    ) -> Approval? {
        let lines = screen.components(separatedBy: "\n").map { $0.trimmingCharacters(in: frame) }
        return numbered(lines, agentId: agentId, scrubber: scrubber)
            ?? cursorMenu(lines, agentId: agentId, scrubber: scrubber, cwdName: cwdName)
    }

    // MARK: Numbered menus

    static func numbered(_ lines: [String], agentId: String, scrubber: PathScrubber) -> Approval? {
        var parsed: [(line: Int, n: Int, label: String, cursor: Bool)] = []
        for (i, line) in lines.enumerated() {
            let ns = line as NSString
            guard let m = option.firstMatch(in: line, range: NSRange(location: 0, length: ns.length)),
                  let n = Int(ns.substring(with: m.range(at: 2)))
            else { continue }
            parsed.append((i, n, ns.substring(with: m.range(at: 3)), m.range(at: 1).location != NSNotFound))
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
        let cursor = run.firstIndex(where: \.cursor)

        var options: [ApprovalOption] = []
        for (idx, o) in run.enumerated() {
            let (label, escHint) = stripHint(o.label)
            if label == freeTextLabel, let cursor {
                // Selecting it by number would also type the digit into the field.
                let moves = arrows(from: cursor, to: idx)
                options.append(ApprovalOption(label: label, keys: moves.isEmpty ? ["up", "down"] : moves, freeText: true))
                continue
            }
            let isTrailingNo = idx == run.count - 1 && label.lowercased().hasPrefix("no")
            let keys = isTrailingNo && escHint ? ["esc"] : [String(o.n)]
            options.append(ApprovalOption(label: scrubber.scrub(label), keys: keys, freeText: label == freeTextLabel ? true : nil))
        }

        return Approval(agentId: agentId, question: scrubber.scrub(question(above: run[0].line, in: lines)), options: options)
    }

    // MARK: Cursor menus

    static func cursorMenu(_ lines: [String], agentId: String, scrubber: PathScrubber, cwdName: String?) -> Approval? {
        // The last `❯ label` line; a bare `❯` is an empty input box, not a menu.
        guard let c = lines.indices.last(where: { i in
            let ns = lines[i] as NSString
            return cursorLine.firstMatch(in: lines[i], range: NSRange(location: 0, length: ns.length)) != nil
        }) else { return nil }

        func isItem(_ l: String) -> Bool {
            !l.isEmpty && !l.unicodeScalars.allSatisfy(rule.contains) && !isFooter(l)
        }
        var start = c, end = c
        while start > 0 && isItem(lines[start - 1]) { start -= 1 }
        while end < lines.count - 1 && isItem(lines[end + 1]) { end += 1 }
        guard end > start else { return nil }
        // Text between two rules is the input box (a multi-line prompt), not a menu.
        func isRule(_ i: Int) -> Bool { lines.indices.contains(i) && !lines[i].isEmpty && lines[i].unicodeScalars.allSatisfy(rule.contains) }
        if isRule(start - 1) && isRule(end + 1) { return nil }

        let items = (start...end).map { i -> String in
            i == c ? String(lines[i].drop(while: { "❯›▶".contains($0) }).drop(while: \.isWhitespace)) : lines[i]
        }
        let cursor = c - start
        let options = items.enumerated().map { idx, raw in
            ApprovalOption(label: scrubber.scrub(stripHint(raw).label), keys: arrows(from: cursor, to: idx) + ["enter"])
        }

        let screen = lines.joined(separator: "\n").lowercased()
        let q: String
        if screen.contains("trust this folder") || screen.contains("quick safety check") {
            q = cwdName.map { "Trust this folder? \($0)" } ?? "Trust this folder?"
        } else {
            q = question(above: start, in: lines)
        }
        return Approval(agentId: agentId, question: scrubber.scrub(q), options: options)
    }

    // MARK: Multi-question tabs

    /// Progress from a tab bar like `←  ☒ Delivery  ☐ Focus  ✔ Submit  →`.
    /// The active tab is the highlighted one in `ansi` (a visible read with colours); without it,
    /// the first unanswered tab. `index` is 1-based and `count` includes the Submit tab.
    public static func step(_ screen: String, ansi: String? = nil) -> ApprovalStep? {
        guard let bar = screen.components(separatedBy: "\n")
            .map({ $0.trimmingCharacters(in: .whitespaces) })
            .last(where: isTabBar)
        else { return nil }
        let tabs = tabItems(bar)
        guard tabs.count >= 2 else { return nil }

        var active: Int?
        if let ansi, let title = highlightedTab(ansi) {
            active = tabs.firstIndex { $0.title == title }
        }
        if active == nil { active = tabs.firstIndex { $0.mark == "☐" } }
        let i = active ?? tabs.count - 1
        return ApprovalStep(index: i + 1, count: tabs.count, title: tabs[i].title)
    }

    static func isTabBar(_ l: String) -> Bool {
        l.hasPrefix("←") && l.hasSuffix("→") && l.contains(where: { "☐☒✔☑✓".contains($0) })
    }

    static func tabItems(_ bar: String) -> [(mark: Character, title: String)] {
        let inner = bar.dropFirst().dropLast()
        var out: [(Character, String)] = []
        var mark: Character?
        var title = ""
        for ch in inner {
            if "☐☒✔☑✓".contains(ch) {
                if let m = mark { out.append((m, title.trimmingCharacters(in: .whitespaces))) }
                mark = ch
                title = ""
            } else if mark != nil {
                title.append(ch)
            }
        }
        if let m = mark { out.append((m, title.trimmingCharacters(in: .whitespaces))) }
        return out.filter { !$0.1.isEmpty }
    }

    private static let highlighted = try! NSRegularExpression(pattern: #"\u001B\[[0-9;]*\b48;[0-9;]*m\s*[☐☒✔☑✓]\s+([^\u001B]+?)\s*\u001B"#)

    static func highlightedTab(_ ansi: String) -> String? {
        for line in ansi.components(separatedBy: "\n") where line.contains("←") && line.contains("→") {
            let ns = line as NSString
            if let m = highlighted.firstMatch(in: line, range: NSRange(location: 0, length: ns.length)) {
                return ns.substring(with: m.range(at: 1))
            }
        }
        return nil
    }

    // MARK: Helpers

    static func arrows(from: Int, to: Int) -> [String] {
        to >= from ? Array(repeating: "down", count: to - from) : Array(repeating: "up", count: from - to)
    }

    static func stripHint(_ raw: String) -> (label: String, esc: Bool) {
        let ns = raw as NSString
        guard let h = hint.firstMatch(in: raw, range: NSRange(location: 0, length: ns.length)) else { return (raw, false) }
        return (ns.substring(to: h.range.location), ns.substring(with: h.range(at: 1)).lowercased() == "esc")
    }

    static func isFooter(_ l: String) -> Bool {
        let lower = l.lowercased()
        return lower.contains("esc to cancel") || (lower.contains(" · ") && lower.contains("enter to"))
    }

    /// The nearest question above `index`, else the nearest meaningful line.
    static func question(above index: Int, in lines: [String]) -> String {
        var fallback: String?
        var i = index - 1
        var looked = 0
        while i >= 0 && looked < 8 {
            let l = lines[i]
            i -= 1
            if l.isEmpty || l.unicodeScalars.allSatisfy(rule.contains) || isFooter(l) || isTabBar(l) { continue }
            looked += 1
            if l.hasSuffix("?") { return l }
            if fallback == nil { fallback = l }
        }
        return fallback ?? "Waiting for your input"
    }

    /// Fallback when a blocked agent shows no menu at all: the last meaningful screen line.
    public static func fallback(_ screen: String, agentId: String, scrubber: PathScrubber = PathScrubber(cwd: nil)) -> Approval {
        let lines = screen.components(separatedBy: "\n").map { $0.trimmingCharacters(in: frame) }
        return Approval(agentId: agentId, question: scrubber.scrub(question(above: lines.count, in: lines)), options: [])
    }
}
