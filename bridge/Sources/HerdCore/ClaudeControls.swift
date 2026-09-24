import Foundation

/// One choice in `GET /controls`.
public struct ControlChoice: Codable, Sendable, Equatable {
    public var id: String
    public var label: String
}

public struct ControlsCatalog: Codable, Sendable, Equatable {
    public var models: [ControlChoice]
    public var modes: [ControlChoice]
    public var efforts: [ControlChoice]
}

/// Model, permission mode and effort of a Claude agent, as far as the transcript and screen tell.
public struct ControlState: Sendable, Equatable {
    public var model: String?
    public var effort: String?
    public var permissionMode: String?

    public init(model: String? = nil, effort: String? = nil, permissionMode: String? = nil) {
        self.model = model
        self.effort = effort
        self.permissionMode = permissionMode
    }

    /// Fills gaps in `self` from `older`.
    public func merged(over older: ControlState?) -> ControlState {
        ControlState(model: model ?? older?.model, effort: effort ?? older?.effort, permissionMode: permissionMode ?? older?.permissionMode)
    }
}

/// Everything the bridge knows about Claude Code's controls. Pure; the list the app offers lives here.
public enum ClaudeControls {
    public static let catalog = ControlsCatalog(
        models: [
            .init(id: "opus", label: "Opus 5.5"),
            .init(id: "sonnet", label: "Sonnet 5"),
            .init(id: "haiku", label: "Haiku 4.5"),
            .init(id: "fable", label: "Fable 5.1"),
        ],
        modes: [
            .init(id: "default", label: "Default"),
            .init(id: "acceptEdits", label: "Accept edits"),
            .init(id: "plan", label: "Plan"),
            .init(id: "auto", label: "Auto"),
            .init(id: "bypassPermissions", label: "Bypass permissions"),
        ],
        efforts: [
            .init(id: "low", label: "Low"),
            .init(id: "medium", label: "Medium"),
            .init(id: "high", label: "High"),
            .init(id: "xhigh", label: "Extra high"),
            .init(id: "max", label: "Max"),
        ])

    static let effortIds = Set(catalog.efforts.map(\.id))

    // MARK: Model names

    /// `claude-opus-5-5` -> `Opus 5.5`, `claude-haiku-4-5-20251001` -> `Haiku 4.5`.
    public static func label(forModel id: String) -> String? {
        var parts = id.lowercased().split(separator: "[").first.map { $0.split(separator: "-").map(String.init) } ?? []
        guard parts.first == "claude", parts.count >= 3 else { return nil }
        parts.removeFirst()
        if let last = parts.last, last.count == 8, last.allSatisfy(\.isNumber) { parts.removeLast() }
        let family = parts.removeFirst()
        guard !parts.isEmpty, parts.allSatisfy({ $0.allSatisfy(\.isNumber) }) else { return nil }
        return family.prefix(1).uppercased() + family.dropFirst() + " " + parts.joined(separator: ".")
    }

    /// `Sonnet 5` -> `claude-sonnet-5`, `Opus 5.5 (1M context)` -> `claude-opus-5-5`.
    public static func id(forLabel label: String) -> String? {
        let clean = label.split(separator: "(").first.map { $0.trimmingCharacters(in: .whitespaces) } ?? label
        let words = clean.split(separator: " ")
        guard words.count == 2 else { return nil }
        let version = words[1].split(separator: ".")
        guard version.allSatisfy({ $0.allSatisfy(\.isNumber) }) else { return nil }
        return "claude-" + words[0].lowercased() + "-" + version.joined(separator: "-")
    }

    // MARK: Transcript

    private static let setModel = try! NSRegularExpression(pattern: #"Set model to `?([^`\n]+?)`?(?: and |\s*$|\s*\(|</)"#)
    private static let setEffort = try! NSRegularExpression(pattern: #"Set effort level to `?([a-z]+)"#)

    /// Scans transcript lines (oldest first); later lines win.
    public static func scan(_ data: Data) -> ControlState {
        var s = ControlState()
        for line in data.split(separator: UInt8(ascii: "\n")) {
            guard let o = (try? JSONSerialization.jsonObject(with: Data(line))) as? [String: Any] else { continue }
            switch o["type"] as? String {
            case "assistant":
                if (o["isSidechain"] as? Bool) == true { continue }
                if let m = (o["message"] as? [String: Any])?["model"] as? String, m.hasPrefix("claude-") { s.model = m }
                if let e = o["effort"] as? String, effortIds.contains(e) { s.effort = e }
            case "permission-mode":
                if let m = o["permissionMode"] as? String { s.permissionMode = normalizeMode(m) }
            case "user":
                guard let text = (o["message"] as? [String: Any])?["content"] as? String, text.contains("<local-command-stdout>") else { continue }
                let ns = text as NSString
                let r = NSRange(location: 0, length: ns.length)
                if let m = setModel.firstMatch(in: text, range: r), let id = id(forLabel: ns.substring(with: m.range(at: 1))) {
                    s.model = id
                }
                if let m = setEffort.firstMatch(in: text, range: r) {
                    let e = ns.substring(with: m.range(at: 1))
                    if effortIds.contains(e) { s.effort = e }
                }
            default:
                continue
            }
        }
        return s
    }

    public static func normalizeMode(_ m: String) -> String {
        m == "manual" ? "default" : m
    }

    // MARK: Screen

    private static let footerModes: [(String, String)] = [
        ("plan mode on", "plan"),
        ("accept edits on", "acceptEdits"),
        ("auto mode on", "auto"),
        ("manual mode on", "default"),
        ("default mode on", "default"),
        ("bypass permissions on", "bypassPermissions"),
        ("don't ask mode on", "dontAsk"),
        ("dontask mode on", "dontAsk"),
    ]

    /// The permission mode from Claude's footer (`⏸ plan mode on (shift+tab to cycle)`).
    public static func footerMode(_ screen: String) -> String? {
        for line in screen.components(separatedBy: "\n").suffix(6).reversed() {
            let l = line.lowercased()
            for (needle, mode) in footerModes where l.contains(needle) { return mode }
        }
        return nil
    }

    private static let bannerLine = try! NSRegularExpression(pattern: #"\b(Opus|Sonnet|Haiku|Fable) (\d+(?:\.\d+)?)(?: \([^)]*\))? with (low|medium|high|xhigh|max) effort"#)

    /// Claude's session banner (`Sonnet 5 with medium effort · Claude Max`), shown at the top of a
    /// fresh session. Covers the gap after `/clear`, before the first assistant message.
    public static func banner(_ screen: String) -> ControlState? {
        let ns = screen as NSString
        guard let m = bannerLine.matches(in: screen, range: NSRange(location: 0, length: ns.length)).last else { return nil }
        let label = ns.substring(with: m.range(at: 1)) + " " + ns.substring(with: m.range(at: 2))
        return ControlState(model: id(forLabel: label), effort: ns.substring(with: m.range(at: 3)))
    }

    private static let effortLine = try! NSRegularExpression(pattern: #"\b(low|medium|high|xhigh|max) · /effort"#)

    /// The effort badge Claude shows above the input (`● high · /effort`), when visible.
    public static func screenEffort(_ screen: String) -> String? {
        let ns = screen as NSString
        let matches = effortLine.matches(in: screen, range: NSRange(location: 0, length: ns.length))
        return matches.last.map { ns.substring(with: $0.range(at: 1)) }
    }
}
