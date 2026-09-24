import Foundation

/// Pulls the in-progress assistant text out of a pane's visible screen, per agent kind.
/// Input is already ANSI-stripped (herdr's `recent_unwrapped` source with `strip_ansi`).
/// Returns `nil` when there's nothing to show yet: a tool call is running, only a spinner is
/// visible, or the screen has no text block at all. Paths are not scrubbed here; callers do that.
public enum LiveReplyParser {
    public static func extract(screen: String, kind: String) -> String? {
        let lines = screen.components(separatedBy: "\n").map(stripInlineBanner)
        switch kind {
        case "claude": return claude(lines)
        case "codex": return codex(lines)
        case "pi": return pi(lines)
        default: return nil
        }
    }

    // MARK: Claude

    private static let claudeSpinners: Set<Character> = ["✢", "✻", "✽", "✳", "✶", "✺", "✵"]

    private static func claude(_ lines: [String]) -> String? {
        guard let markerIdx = lines.lastIndex(where: { isClaudeMarker($0) }) else {
            // The block's own `⏺` marker has scrolled off the read window (a long reply, or a
            // small `lines` budget): fall back to whatever paragraph is left at the tail.
            return claudeFallback(lines)
        }
        let markerLine = lines[markerIdx].trimmingCharacters(in: .whitespaces)
        let first = String(markerLine.dropFirst()).trimmingCharacters(in: .whitespaces)

        var block = [first]
        var i = markerIdx + 1
        while i < lines.count {
            let line = lines[i]
            if isClaudeStop(line) { break }
            block.append(line)
            i += 1
        }

        if isCallSignature(first) { return nil }
        if let nextContent = block.dropFirst().first(where: { !$0.trimmingCharacters(in: .whitespaces).isEmpty }),
           nextContent.trimmingCharacters(in: .whitespaces).hasPrefix("⎿") {
            return nil
        }

        return nonEmpty(joinBlock(block))
    }

    /// The last paragraph on screen, once chrome (footer, rules, prompts, spinners, tool results)
    /// is filtered out. Used when the current block's `⏺` marker isn't in the read window.
    private static func claudeFallback(_ lines: [String]) -> String? {
        let content = lines.filter { !isClaudeChrome($0) }
        guard let block = lastParagraph(content) else { return nil }
        return nonEmpty(joinBlock(block))
    }

    private static func isClaudeMarker(_ line: String) -> Bool {
        line.trimmingCharacters(in: .whitespaces).hasPrefix("⏺")
    }

    private static func isClaudeStop(_ line: String) -> Bool {
        let t = line.trimmingCharacters(in: .whitespaces)
        if let first = t.first, claudeSpinners.contains(first) { return true }
        if t.hasPrefix("❯") { return true }
        if isRule(t) { return true }
        return false
    }

    private static let claudeFooterMarkers = ["Update available!", "auto mode on"]

    private static func isClaudeChrome(_ line: String) -> Bool {
        let t = line.trimmingCharacters(in: .whitespaces)
        if t.isEmpty { return false }  // blank lines are paragraph separators, not chrome
        if isRule(t) { return true }
        if t.hasPrefix("❯") { return true }  // the prompt echo above, or the empty input box below
        if t.hasPrefix("⎿") { return true }  // a tool result, not reply text
        if let first = t.first, claudeSpinners.contains(first) { return true }
        if claudeFooterMarkers.contains(where: { t.contains($0) }) { return true }
        if isTokenStats(t) { return true }
        return false
    }

    // MARK: Codex

    private static func codex(_ lines: [String]) -> String? {
        guard let markerIdx = lines.lastIndex(where: { $0.trimmingCharacters(in: .whitespaces).hasPrefix("• ") })
        else { return nil }
        let markerLine = lines[markerIdx].trimmingCharacters(in: .whitespaces)
        let first = String(markerLine.dropFirst(2))

        if first.hasPrefix("Working") { return nil }
        if first.hasPrefix("Ran ") { return nil }
        if first.hasPrefix("Model changed") { return nil }

        var block = [first]
        var i = markerIdx + 1
        while i < lines.count {
            let line = lines[i]
            if isCodexStop(line) { break }
            block.append(line)
            i += 1
        }

        if let nextContent = block.dropFirst().first(where: { !$0.trimmingCharacters(in: .whitespaces).isEmpty }),
           nextContent.trimmingCharacters(in: .whitespaces).hasPrefix("└") {
            return nil
        }

        return nonEmpty(joinBlock(block))
    }

    private static func isCodexStop(_ line: String) -> Bool {
        let t = line.trimmingCharacters(in: .whitespaces)
        if t.hasPrefix("›") { return true }
        if t.hasPrefix("✗") || t.hasPrefix("✔") || t.hasPrefix("■") { return true }
        if isRule(t) { return true }
        if isTimestamp(t) { return true }
        return false
    }

    // MARK: pi

    private static func pi(_ lines: [String]) -> String? {
        if lines.contains(where: isPiSpinner) { return nil }
        let content = lines.filter { !isPiChrome($0) }
        guard let block = lastParagraph(content) else { return nil }
        return nonEmpty(joinBlock(block))
    }

    private static func isPiSpinner(_ line: String) -> Bool {
        let t = line.trimmingCharacters(in: .whitespaces)
        return t.contains("Working") && t.contains("─")
    }

    private static let piChromePrefixes = [
        "Update Available", "New version", "Changelog:", "Model:", "Warning:", "Thinking level:",
        "Manage extra usage",
    ]

    private static func isPiChrome(_ line: String) -> Bool {
        let t = line.trimmingCharacters(in: .whitespaces)
        if t.isEmpty { return false }  // blank lines are paragraph separators, not chrome
        if isRule(t) { return true }
        if piChromePrefixes.contains(where: { t.hasPrefix($0) }) { return true }
        if t.hasPrefix("$") && t.contains("(") { return true }  // cost/token footer chip
        if t.contains("(auto)") || t.contains("(sub)") { return true }
        if t.hasPrefix("~") { return true }  // cwd + git branch footer line
        return false
    }

    // MARK: Shared helpers

    /// A bare tool-call signature, e.g. `Write(answer.txt)` or `Bash(ping -c 45 127.0.0.1)`.
    private static func isCallSignature(_ text: String) -> Bool {
        guard let re = try? NSRegularExpression(pattern: #"^[A-Za-z][A-Za-z0-9_.]*\([^\n]*\)$"#) else { return false }
        let range = NSRange(text.startIndex..., in: text)
        return re.firstMatch(in: text, range: range) != nil
    }

    private static func isRule(_ line: String) -> Bool {
        !line.isEmpty && line.allSatisfy { $0 == "─" || $0 == "-" }
    }

    private static func isTimestamp(_ line: String) -> Bool {
        (try? NSRegularExpression(pattern: #"^\d{1,2}:\d{2}\s?(AM|PM)$"#))
            .map { $0.firstMatch(in: line, range: NSRange(line.startIndex..., in: line)) != nil } ?? false
    }

    /// Claude's footer token/usage line, e.g. `72.2k/1.0M · in 72.2k out 977 · 5h 10%(2h13m) · wk 11%(4d15h)`.
    private static func isTokenStats(_ line: String) -> Bool {
        (try? NSRegularExpression(pattern: #"^[\d.]+[kM]?/[\d.]+[kM]?\s*·"#))
            .map { $0.firstMatch(in: line, range: NSRange(line.startIndex..., in: line)) != nil } ?? false
    }

    /// The last run of non-blank lines, i.e. the last paragraph. It's fine to show only the tail
    /// of a scrolled-off reply.
    private static func lastParagraph(_ lines: [String]) -> [String]? {
        var end = lines.count
        while end > 0, lines[end - 1].trimmingCharacters(in: .whitespaces).isEmpty { end -= 1 }
        guard end > 0 else { return nil }
        var start = end
        while start > 0, !lines[start - 1].trimmingCharacters(in: .whitespaces).isEmpty { start -= 1 }
        return Array(lines[start..<end])
    }

    /// Rejoins wrapped lines into flowing paragraphs; a blank line is kept as a paragraph break.
    private static func joinBlock(_ lines: [String]) -> String {
        var paragraphs: [String] = []
        var current: [String] = []
        for raw in lines {
            let line = raw.trimmingCharacters(in: .whitespaces)
            if line.isEmpty {
                if !current.isEmpty {
                    paragraphs.append(current.joined(separator: " "))
                    current = []
                }
            } else {
                current.append(line)
            }
        }
        if !current.isEmpty { paragraphs.append(current.joined(separator: " ")) }
        return paragraphs.joined(separator: "\n\n")
    }

    private static func nonEmpty(_ s: String) -> String? { s.isEmpty ? nil : s }

    /// Claude Code draws its "Update available!" banner at a fixed row that can land on the same
    /// visual row as flowing reply text once the alternate screen scrolls under it, so a clean read
    /// can show it concatenated onto real content. Best effort: drop everything from the banner on;
    /// it can't undo characters the two writes already interleaved on screen.
    private static func stripInlineBanner(_ line: String) -> String {
        guard let range = line.range(of: "Update available!") else { return line }
        return String(line[line.startIndex..<range.lowerBound])
    }
}
