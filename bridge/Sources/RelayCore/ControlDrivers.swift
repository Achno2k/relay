import Foundation

public struct ControlSupport: Codable, Sendable, Equatable {
    public var model: Bool
    public var effort: Bool
    public var mode: Bool
    public var compact: Bool
    public var clear: Bool
}

/// `GET /agents/:id/controls`: what this agent's own UI can change, with its own lists.
public struct AgentControls: Codable, Sendable, Equatable {
    public var models: [ControlChoice]
    public var efforts: [ControlChoice]
    public var modes: [ControlChoice]
    public var supports: ControlSupport
}

public extension ControlChoice {
    init(_ id: String, _ label: String) {
        self.init(id: id, label: label)
    }
}

/// One per agent kind: reads the current model/effort/mode and drives that agent's UI to change them.
protocol ControlDriver: Sendable {
    /// Current values from the screen (footer) and session file. Gaps are filled by the caller.
    func read(_ a: HerdrAgent, ref: TranscriptRef?, screen: String?, service: AgentService) async -> ControlState
    func label(forModel id: String, service: AgentService) -> String?
    func capabilities(_ a: HerdrAgent, current: ControlState, service: AgentService) async -> AgentControls
    /// Applies an already-validated request and returns once it's confirmed.
    func apply(_ request: ControlRequest, to a: HerdrAgent, current: ControlState, service: AgentService) async throws
}

enum Effort {
    static let labels: [String: String] = [
        "none": "None", "off": "Off", "minimal": "Minimal", "low": "Low", "medium": "Medium",
        "high": "High", "xhigh": "Extra high", "max": "Max", "ultra": "Ultra",
    ]

    static func choices(_ ids: [String]) -> [ControlChoice] {
        ids.map { ControlChoice($0, labels[$0] ?? $0.capitalized) }
    }
}

// MARK: Claude

struct ClaudeDriver: ControlDriver {
    func read(_ a: HerdrAgent, ref: TranscriptRef?, screen: String?, service: AgentService) async -> ControlState {
        var s = ref.map(service.transcriptControls) ?? ControlState()
        if let screen {
            if let mode = ClaudeControls.footerMode(screen) { s.permissionMode = mode }
            s = s.merged(over: ClaudeControls.banner(screen))
        }
        return s
    }

    func label(forModel id: String, service: AgentService) -> String? { ClaudeControls.label(forModel: id) }

    func capabilities(_ a: HerdrAgent, current: ControlState, service: AgentService) async -> AgentControls {
        let c = ClaudeControls.catalog
        return AgentControls(models: c.models, efforts: c.efforts, modes: c.modes,
                             supports: .init(model: true, effort: true, mode: true, compact: true, clear: true))
    }

    func apply(_ request: ControlRequest, to a: HerdrAgent, current: ControlState, service: AgentService) async throws {
        try await service.applyClaude(request, to: a)
    }
}

// MARK: Shared screen helpers

/// A numbered picker as pi and codex draw it: `› 2. GPT-5.6-Terra   Older balanced model…`.
struct Picker: Equatable {
    var title: String?
    var labels: [String]
    var cursor: Int?

    private static let row = try! NSRegularExpression(pattern: #"^\s*([›❯>▶→]\s*)?(\d{1,2})\.\s+(\S.*?)\s*$"#)

    /// The last run of numbered rows 1…N on screen, or nil.
    static func parse(_ screen: String) -> Picker? {
        let lines = screen.components(separatedBy: "\n")
        var rows: [(line: Int, n: Int, label: String, cursor: Bool)] = []
        for (i, l) in lines.enumerated() {
            let ns = l as NSString
            guard let m = row.firstMatch(in: l, range: NSRange(location: 0, length: ns.length)),
                  let n = Int(ns.substring(with: m.range(at: 2))) else { continue }
            rows.append((i, n, cleanLabel(ns.substring(with: m.range(at: 3))), m.range(at: 1).location != NSNotFound))
        }
        guard let last = rows.last else { return nil }
        var run = [last]
        for r in rows.dropLast().reversed() {
            guard r.n == run.last!.n - 1 else { break }
            run.append(r)
        }
        guard run.last?.n == 1 else { return nil }
        run.reverse()
        var title: String?
        var i = run[0].line - 1
        while i >= 0 {
            let t = lines[i].trimmingCharacters(in: .whitespaces)
            if !t.isEmpty { title = t; break }
            i -= 1
        }
        return Picker(title: title, labels: run.map(\.label), cursor: run.firstIndex(where: \.cursor))
    }

    /// `GPT-5.6-Terra         Older balanced…` → `GPT-5.6-Terra`; drops `(default)`/`(current)`.
    static func cleanLabel(_ raw: String) -> String {
        var s = raw.components(separatedBy: "  ").first ?? raw
        for tag in ["(default)", "(current)"] { s = s.replacingOccurrences(of: tag, with: "") }
        return s.trimmingCharacters(in: .whitespaces)
    }
}

extension AgentService {
    /// Submits a slash command to a non-Claude agent (no input clearing needed: they start empty).
    func submit(_ a: HerdrAgent, _ command: String) async throws {
        try await herdr.prompt(a.paneId, text: command)
    }

    func screen(_ a: HerdrAgent) async throws -> String {
        try await herdr.read(a.paneId, source: .detection).text
    }

    /// Last non-empty lines of the screen, where fresh confirmations appear.
    static func tail(_ screen: String, _ n: Int = 8) -> [String] {
        Array(screen.components(separatedBy: "\n").map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }.suffix(n))
    }

    /// Waits for a picker whose title contains `title`, moves its cursor to the row `pick` matches
    /// one key at a time (re-reading the screen after each press), then presses `key`.
    func drivePicker(_ a: HerdrAgent, title: String, pick: @Sendable (String) -> Bool, key: String, what: String) async throws {
        var picker: Picker?
        let clock = ContinuousClock()
        let deadline = clock.now + .seconds(5)
        while picker == nil {
            guard clock.now < deadline else {
                throw APIError(.gatewayTimeout, "control_timeout", "the \(title) picker never opened")
            }
            try await Task.sleep(for: .milliseconds(150))
            picker = Picker.parse(try await screen(a)).flatMap { Self.titleMatches($0, title) ? $0 : nil }
        }
        for _ in 0..<24 {
            guard let p = picker else { break }
            guard let target = p.labels.firstIndex(where: pick) else {
                try await herdr.sendKeys(a.paneId, keys: ["esc"])
                throw APIError(.badRequest, "unsupported", "\(what) isn't in the agent's \(title) picker (\(p.labels.joined(separator: ", ")))")
            }
            guard let cursor = p.cursor else { throw APIError(.gatewayTimeout, "control_timeout", "can't see the cursor in the \(title) picker") }
            if cursor == target {
                try await herdr.sendKeys(a.paneId, keys: [key])
                return
            }
            try await herdr.sendKeys(a.paneId, keys: [target > cursor ? "down" : "up"])
            // Re-read until the cursor moves.
            for _ in 0..<10 {
                try await Task.sleep(for: .milliseconds(80))
                let next = Picker.parse(try await screen(a))
                if let next, next.cursor != cursor { picker = next; break }
            }
        }
        try? await herdr.sendKeys(a.paneId, keys: ["esc"])
        throw APIError(.gatewayTimeout, "control_timeout", "couldn't reach \(what) in the \(title) picker")
    }

    /// `""` matches any picker (Swift's `contains("")` is false, so say it explicitly).
    static func titleMatches(_ p: Picker, _ title: String) -> Bool {
        title.isEmpty || p.title?.contains(title) == true
    }

    static func count(_ text: String, in screen: String) -> Int {
        screen.components(separatedBy: text).count - 1
    }

    /// Waits until `done` holds on the screen. Only new output counts: an error line (`■ …Error…`)
    /// fails the control only if there are more of them than in `before`.
    func waitForScreen(_ a: HerdrAgent, before: String, timeout: Duration = AgentService.controlTimeout, what: String,
                       _ done: @escaping @Sendable (String) -> Bool) async throws {
        let errorsBefore = Self.count("■ Error", in: before)
        try await waitFor(timeout, what: what) {
            let s = try await self.screen(a)
            if Self.count("■ Error", in: s) > errorsBefore,
               let err = s.components(separatedBy: "\n").last(where: { $0.contains("■ Error") }) {
                throw APIError(.badGateway, "control_failed", err.trimmingCharacters(in: CharacterSet(charactersIn: "■ ")))
            }
            return done(s)
        }
    }
}
