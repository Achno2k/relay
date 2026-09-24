import Foundation

/// Validates `POST /agents/:id/keys` entries against herdr's key name shape (api.md: "Key names
/// are herdr's (`esc`, `enter`, `up`, `down`, `ctrl+u`, digits, …)") before they reach the socket,
/// so a malformed client can't type garbage into a live pane.
public enum KeyNames {
    static let named: Set<String> = Set([
        "esc", "escape", "enter", "return", "tab", "backtab", "space",
        "up", "down", "left", "right",
        "backspace", "delete", "insert", "home", "end", "pageup", "pagedown",
    ]).union((1...24).map { "f\($0)" })

    static let modifiers: Set<String> = ["ctrl", "shift", "alt", "cmd", "option", "meta"]

    /// One key token: a named key, a single printable character (digits, letters, punctuation),
    /// or `modifier+key` (`ctrl+u`, `shift+tab`).
    public static func isValid(_ key: String) -> Bool {
        guard !key.isEmpty, key.utf8.count <= 32 else { return false }
        let lower = key.lowercased()
        if named.contains(lower) { return true }
        if key.count == 1, let scalar = key.unicodeScalars.first, scalar.isASCII, scalar.value >= 0x20, scalar.value < 0x7F {
            return true
        }
        // A numbered menu row (`ApprovalParser`'s `\d{1,2}` options).
        if key.count <= 2, key.allSatisfy(\.isNumber) { return true }
        let parts = lower.split(separator: "+", omittingEmptySubsequences: false)
        guard parts.count == 2, modifiers.contains(String(parts[0])) else { return false }
        let rest = String(parts[1])
        guard !rest.isEmpty else { return false }
        return named.contains(rest) || (rest.count == 1 && rest.unicodeScalars.allSatisfy { $0.isASCII && $0.value >= 0x20 && $0.value < 0x7F })
    }

    public static func firstInvalid(_ keys: [String]) -> String? {
        keys.first { !isValid($0) }
    }
}
