import Foundation

/// `POST /agents/:id/control`: drives Claude Code's own UI (slash commands, Shift+Tab) and waits
/// until the change shows up in the transcript or on screen.
public enum ControlRequest: Sendable, Equatable {
    case model(String)
    case permissionMode(String)
    case effort(String)
    case compact
    case clear
}

extension AgentService {
    static let controlTimeout: Duration = .seconds(10)
    static let compactTimeout: Duration = .seconds(120)
    /// herdr can take 10+ s to report the new session id after `/clear`.
    static let clearTimeout: Duration = .seconds(30)

    public func control(id: String, _ request: ControlRequest) async throws -> Agent {
        let a = try await herdr.agent(id)
        guard Self.isClaude(a) else { throw APIError(.badRequest, "unsupported", "controls only work on Claude agents") }
        switch a.agentStatus {
        case .working: throw APIError(.conflict, "agent_busy", "agent is working")
        case .blocked: throw APIError(.conflict, "agent_blocked", "agent is waiting at a dialog")
        default: break
        }
        let catalog = ClaudeControls.catalog
        switch request {
        case .model(let alias):
            guard catalog.models.contains(where: { $0.id == alias }) else { throw APIError.badRequest("unknown model \(alias)") }
            let label = catalog.models.first { $0.id == alias }!.label
            try await slashSetting(a, command: "/model \(alias)", confirm: "Set model to \(label)")
            holdConfirmed(a.paneId, ControlState(model: ClaudeControls.id(forLabel: label)))
        case .effort(let level):
            guard catalog.efforts.contains(where: { $0.id == level }) else { throw APIError.badRequest("unknown effort \(level)") }
            try await slashSetting(a, command: "/effort \(level)", confirm: "Set effort level to \(level)")
            holdConfirmed(a.paneId, ControlState(effort: level))
        case .permissionMode(let mode):
            guard catalog.modes.contains(where: { $0.id == mode }) || mode == "dontAsk" else { throw APIError.badRequest("unknown mode \(mode)") }
            try await cycleMode(a, to: mode)
        case .compact:
            let ref = locator.locate(a)
            let start = ref.map { fileSize($0.url) }
            let compactedBefore = try await herdr.read(a.paneId, source: .detection).text.components(separatedBy: "Compacted").count
            try await slash(a, "/compact")
            try await waitFor(Self.compactTimeout, what: "/compact") {
                if let ref, let start {
                    let tail = self.read(ref.url, from: start)
                    return tail.contains("\"compact_boundary\"") || tail.contains("Compacted")
                }
                let s = try await self.herdr.read(a.paneId, source: .detection).text
                return s.components(separatedBy: "Compacted").count > compactedBefore
            }
        case .clear:
            let before = a.agentSession?.value
            try await slash(a, "/clear")
            dropConfirmed(a.paneId)
            try await waitFor(Self.clearTimeout, what: "/clear") {
                let now = try await self.herdr.agent(a.paneId)
                return now.agentSession?.value != nil && now.agentSession?.value != before
            }
        }
        onChange()
        return try await agent(id: a.paneId)
    }

    // MARK: Slash commands

    /// Submits a slash command into an empty input box with herdr's `agent.prompt` (text and Enter
    /// as one ordered write). If the command is still sitting on the last `❯` line afterwards (Claude
    /// can drop an Enter while re-rendering, seen live after `/model`), press Enter again.
    func slash(_ a: HerdrAgent, _ command: String) async throws {
        try await clearInput(a.paneId, wait: .zero)
        try await herdr.prompt(a.paneId, text: command)
        for attempt in 0..<4 {
            var pending = false
            for _ in 0..<6 {
                try await Task.sleep(for: .milliseconds(150))
                let screen = try await herdr.read(a.paneId, source: .detection).text
                pending = InputBox.lastPromptLine(screen) == command
                if !pending { return }
            }
            if attempt < 3 { try await herdr.sendKeys(a.paneId, keys: ["enter"]) }
        }
        throw APIError(.gatewayTimeout, "control_timeout", "\(command) stayed in the input box")
    }

    /// `/model x` and `/effort x`: confirmed by Claude's `Set … to <value>` output, then the saved
    /// default is restored.
    func slashSetting(_ a: HerdrAgent, command: String, confirm: String) async throws {
        let ref = locator.locate(a)
        let start = ref.map { fileSize($0.url) }
        let saved = settings.snapshot()
        defer {
            settings.restore(saved)
            // Claude may write the file again a moment later.
            let settings = self.settings
            Task {
                try? await Task.sleep(for: .seconds(1))
                settings.restore(saved)
            }
        }
        try await slash(a, command)
        // The transcript quotes the value in backticks (`Set model to `Sonnet 5``); the screen doesn't.
        @Sendable func mentions(_ text: String) -> Bool { text.replacingOccurrences(of: "`", with: "").contains(confirm) }
        try await waitFor(Self.controlTimeout, what: command) {
            if let ref, let start, mentions(self.read(ref.url, from: start)) { return true }
            let now = try await self.herdr.agent(a.paneId)
            let screen = try await self.herdr.read(a.paneId, source: .detection).text
            if now.agentStatus == .blocked {
                // A picker instead of a direct switch: leave it rather than guess.
                try await self.herdr.sendKeys(a.paneId, keys: ["esc"])
                throw APIError(.badRequest, "unsupported", "Claude opened a picker for \(command)")
            }
            // Stale output of the same value would only confirm what's already true.
            return mentions(screen)
        }
    }

    // MARK: Permission mode

    /// Shift+Tab until the footer shows `target`, at most one full cycle.
    func cycleMode(_ a: HerdrAgent, to target: String) async throws {
        func current() async throws -> String {
            let screen = try await herdr.read(a.paneId, source: .detection).text
            guard let m = ClaudeControls.footerMode(screen) else {
                throw APIError(.gatewayTimeout, "control_timeout", "can't see the permission mode in the footer")
            }
            return m
        }
        let start = try await current()
        if start == target { return }
        var seen = [start]
        for _ in 0..<8 {
            // One press per call: two presses in one send_keys only move one step.
            try await herdr.sendKeys(a.paneId, keys: ["shift+tab"])
            var mode = seen.last!
            for _ in 0..<10 {
                try await Task.sleep(for: .milliseconds(150))
                mode = try await current()
                if mode != seen.last { break }
            }
            if mode == target { return }
            if mode == start || seen.contains(mode) {
                throw APIError(.badRequest, "unsupported", "\(target) isn't in this agent's Shift+Tab cycle (\(seen.joined(separator: " → ")))")
            }
            seen.append(mode)
        }
        throw APIError(.gatewayTimeout, "control_timeout", "mode never reached \(target)")
    }

    // MARK: Helpers

    func waitFor(_ timeout: Duration, what: String, _ done: @Sendable () async throws -> Bool) async throws {
        let clock = ContinuousClock()
        let deadline = clock.now + timeout
        while clock.now < deadline {
            try await Task.sleep(for: .milliseconds(250))
            if try await done() { return }
        }
        throw APIError(.gatewayTimeout, "control_timeout", "\(what) sent, but no confirmation within \(timeout)")
    }

    func fileSize(_ url: URL) -> UInt64 {
        ((try? FileManager.default.attributesOfItem(atPath: url.path))?[.size] as? NSNumber)?.uint64Value ?? 0
    }

    func read(_ url: URL, from offset: UInt64) -> String {
        guard let h = try? FileHandle(forReadingFrom: url) else { return "" }
        defer { try? h.close() }
        try? h.seek(toOffset: offset)
        return String(decoding: (try? h.readToEnd()) ?? Data(), as: UTF8.self)
    }
}
