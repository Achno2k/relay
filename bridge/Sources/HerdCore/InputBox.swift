import Foundation

/// Claude Code's prompt input: the lines between the last two horizontal rules, starting with `❯`.
public enum InputBox {
    private static let rule = CharacterSet(charactersIn: "─━═╌┄ ")

    /// The text in the input box, "" when empty, nil when the screen has no recognisable box.
    public static func content(_ screen: String) -> String? {
        let lines = screen.components(separatedBy: "\n")
        func isRule(_ l: String) -> Bool {
            let t = l.trimmingCharacters(in: .whitespaces)
            return t.count >= 10 && t.unicodeScalars.allSatisfy(rule.contains)
        }
        guard let bottom = lines.indices.last(where: { isRule(lines[$0]) }),
              let top = lines[..<bottom].indices.last(where: { isRule(lines[$0]) }),
              bottom - top >= 2
        else { return nil }
        let box = lines[(top + 1)..<bottom]
        guard let first = box.first, first.hasPrefix("❯") || first.hasPrefix(">") else { return nil }
        let text = ([String(first.dropFirst())] + box.dropFirst())
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .joined(separator: "\n")
        return text.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// Text on the last `❯` line of the screen: the input line, even while an autocomplete list
    /// covers the footer. Empty when the input is empty.
    public static func lastPromptLine(_ screen: String) -> String? {
        guard let line = screen.components(separatedBy: "\n").last(where: { $0.hasPrefix("❯") }) else { return nil }
        return line.dropFirst().trimmingCharacters(in: .whitespaces)
    }

    /// Enough `ctrl+u` presses to empty `text`: one per line plus one per line break.
    public static func clearKeys(for text: String) -> [String] {
        let lines = text.components(separatedBy: "\n").count
        return Array(repeating: "ctrl+u", count: min(lines * 2, 60))
    }
}
