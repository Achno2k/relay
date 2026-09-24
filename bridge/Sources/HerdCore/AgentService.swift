import Foundation
import Synchronization

/// Everything the REST routes need, re-derived from herdr on every call.
public final class AgentService: Sendable {
    public let herdr: HerdrClient
    public let locator: TranscriptLocator
    private let seen = Mutex<[String: (seq: UInt64, changed: Date?, first: Date)]>([:])
    private let cache = Mutex<[URL: CachedTranscript]>([:])
    private let controlCache = Mutex<[URL: (size: UInt64, mtime: Date, state: ControlState)]>([:])
    private let lastControls = Mutex<[String: ControlState]>([:])
    /// Values a control just confirmed on screen, held until the transcript catches up.
    private let confirmed = Mutex<[String: (state: ControlState, until: Date)]>([:])
    let settings: SettingsGuard
    /// Called after a control changed something, so the monitor can push `agent.updated` right away.
    let onChange: @Sendable () -> Void

    private struct CachedTranscript {
        var size: UInt64
        var mtime: Date
        var messages: [Message]
        var used: Date
    }

    public init(herdr: HerdrClient, locator: TranscriptLocator, settings: SettingsGuard = SettingsGuard(), onChange: @escaping @Sendable () -> Void = {}) {
        self.herdr = herdr
        self.locator = locator
        self.settings = settings
        self.onChange = onChange
    }

    // MARK: Agents

    public struct Snapshot: Sendable {
        public var agent: Agent
        public var raw: HerdrAgent
        public var transcript: TranscriptRef?
    }

    public func snapshots() async throws -> [Snapshot] {
        async let agents = herdr.agents()
        async let workspaces = herdr.workspaces()
        let names = Dictionary(try await workspaces.map { ($0.workspaceId, $0.label) }, uniquingKeysWith: { a, _ in a })
        let raw = try await agents
        return await withTaskGroup(of: (Int, Snapshot).self) { group in
            for (i, a) in raw.enumerated() {
                group.addTask { (i, await self.snapshot(a, workspaceName: names[a.workspaceId])) }
            }
            var out = [Snapshot?](repeating: nil, count: raw.count)
            for await (i, s) in group { out[i] = s }
            return out.compactMap { $0 }
        }
    }

    public func agents() async throws -> [Agent] {
        try await snapshots().map(\.agent)
    }

    public func snapshot(id: String) async throws -> Snapshot {
        async let raw = herdr.agent(id)
        async let workspaces = herdr.workspaces()
        let a = try await raw
        let name = try await workspaces.first { $0.workspaceId == a.workspaceId }?.label
        return await snapshot(a, workspaceName: name)
    }

    public func agent(id: String) async throws -> Agent {
        try await snapshot(id: id).agent
    }

    func snapshot(_ a: HerdrAgent, workspaceName: String?) async -> Snapshot {
        let ref = locator.locate(a)
        let mtime = ref.flatMap { try? $0.url.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate }
        let controls = await controls(a, ref: ref)
        var agent = Agent(
            id: a.paneId,
            name: a.name,
            kind: a.agent ?? a.agentSession?.agent ?? "unknown",
            title: title(a),
            workspaceId: a.workspaceId,
            workspaceName: workspaceName ?? a.workspaceId,
            cwdName: CwdName.of(a.cwd ?? a.foregroundCwd),
            status: a.agentStatus,
            hasTranscript: ref != nil,
            updatedAt: Timestamps.format(updatedAt(a, transcriptModified: mtime)))
        agent.model = controls?.model
        agent.modelLabel = controls?.model.flatMap(ClaudeControls.label(forModel:))
        agent.permissionMode = controls?.permissionMode
        agent.effort = controls?.effort
        agent.sessionId = a.agentSession.map { s in
            s.kind == "path" ? URL(fileURLWithPath: s.value).deletingPathExtension().lastPathComponent : s.value
        }
        return Snapshot(agent: agent, raw: a, transcript: ref)
    }

    static func isClaude(_ a: HerdrAgent) -> Bool {
        (a.agent ?? a.agentSession?.agent) == "claude"
    }

    /// Transcript tail for model/effort, the footer for the permission mode (the transcript's
    /// `permission-mode` lines lag). Gaps are filled from the last known values, e.g. right after
    /// `/clear` when the new transcript has no assistant message yet.
    func controls(_ a: HerdrAgent, ref: TranscriptRef?) async -> ControlState? {
        guard Self.isClaude(a) else { return nil }
        var s = ref.map(transcriptControls) ?? ControlState()
        if let screen = try? await herdr.read(a.paneId, source: .detection).text {
            if let mode = ClaudeControls.footerMode(screen) { s.permissionMode = mode }
            s = s.merged(over: ClaudeControls.banner(screen))
        }
        if let held = confirmed.withLock({ c -> ControlState? in
            guard let e = c[a.paneId] else { return nil }
            if e.until < Date() {
                c.removeValue(forKey: a.paneId)
                return nil
            }
            return e.state
        }) {
            s.model = held.model ?? s.model
            s.effort = held.effort ?? s.effort
        }
        return lastControls.withLock { last in
            let merged = s.merged(over: last[a.paneId])
            last[a.paneId] = merged
            return merged
        }
    }

    /// Claude sometimes writes `/model` and `/effort` output to the transcript seconds late.
    func holdConfirmed(_ paneId: String, _ state: ControlState) {
        confirmed.withLock { c in
            var merged = state
            if let e = c[paneId], e.until > Date() { merged = state.merged(over: e.state) }
            c[paneId] = (merged, Date().addingTimeInterval(15))
        }
    }

    func dropConfirmed(_ paneId: String) {
        confirmed.withLock { _ = $0.removeValue(forKey: paneId) }
    }

    static let controlTailBytes: UInt64 = 1 << 20

    func transcriptControls(_ ref: TranscriptRef) -> ControlState {
        guard let attrs = try? FileManager.default.attributesOfItem(atPath: ref.url.path) else { return ControlState() }
        let size = (attrs[.size] as? NSNumber)?.uint64Value ?? 0
        let mtime = (attrs[.modificationDate] as? Date) ?? .distantPast
        if let hit = controlCache.withLock({ $0[ref.url] }), hit.size == size, hit.mtime == mtime { return hit.state }
        guard let h = try? FileHandle(forReadingFrom: ref.url) else { return ControlState() }
        defer { try? h.close() }
        let start = size > Self.controlTailBytes ? size - Self.controlTailBytes : 0
        try? h.seek(toOffset: start)
        let data = (try? h.readToEnd()) ?? Data()
        let state = ClaudeControls.scan(data)
        controlCache.withLock { $0[ref.url] = (size, mtime, state) }
        return state
    }

    private func title(_ a: HerdrAgent) -> String {
        let scrub = PathScrubber(cwd: a.cwd)
        for t in [a.terminalTitleStripped, a.title] {
            if let t = t?.trimmingCharacters(in: .whitespaces), !t.isEmpty { return scrub.scrub(t) }
        }
        return a.name ?? a.agent ?? a.paneId
    }

    /// herdr gives a state sequence number, not a time. Use the transcript's mtime, or the time we
    /// saw the sequence change; before either is known, the time the bridge first saw the agent.
    private func updatedAt(_ a: HerdrAgent, transcriptModified: Date?) -> Date {
        let seq = a.stateChangeSeq ?? 0
        let (changed, first) = seen.withLock { seen -> (Date?, Date) in
            let now = Date()
            guard let s = seen[a.paneId] else {
                seen[a.paneId] = (seq, nil, now)
                return (nil, now)
            }
            if s.seq != seq {
                seen[a.paneId] = (seq, now, s.first)
                return (now, s.first)
            }
            return (s.changed, s.first)
        }
        switch (transcriptModified, changed) {
        case let (m?, c?): return max(m, c)
        case let (m?, nil): return m
        case let (nil, c?): return c
        case (nil, nil): return first
        }
    }

    public func workspaces() async throws -> [Workspace] {
        async let ws = herdr.workspaces()
        async let agents = herdr.agents()
        let counts = Dictionary(grouping: try await agents, by: \.workspaceId).mapValues(\.count)
        return try await ws.map { Workspace(id: $0.workspaceId, name: $0.label, agentCount: counts[$0.workspaceId] ?? 0) }
    }

    // MARK: Messages

    public func messages(id: String, before: String?, limit: Int) async throws -> MessagePage {
        let snap = try await snapshot(id: id)
        let all: [Message]
        if let ref = snap.transcript {
            all = try transcriptMessages(ref)
        } else {
            all = try await screenMessages(snap.raw)
        }
        return try Self.page(all, before: before, limit: limit)
    }

    static func page(_ all: [Message], before: String?, limit: Int) throws -> MessagePage {
        var end = all.count
        if let before {
            guard let i = all.firstIndex(where: { $0.id == before }) else {
                throw APIError.notFound("no message \(before)")
            }
            end = i
        }
        let start = max(0, end - limit)
        return MessagePage(messages: Array(all[start..<end]), hasMore: start > 0)
    }

    func transcriptMessages(_ ref: TranscriptRef) throws -> [Message] {
        let attrs = try FileManager.default.attributesOfItem(atPath: ref.url.path)
        let size = (attrs[.size] as? NSNumber)?.uint64Value ?? 0
        let mtime = (attrs[.modificationDate] as? Date) ?? .distantPast
        if let hit = cache.withLock({ c -> [Message]? in
            guard let e = c[ref.url], e.size == size, e.mtime == mtime else { return nil }
            c[ref.url]?.used = Date()
            return e.messages
        }) {
            return hit
        }
        let data = try Data(contentsOf: ref.url)
        let messages = TranscriptParser.parse(data, format: ref.format, cwd: ref.cwd)
        cache.withLock { c in
            c[ref.url] = CachedTranscript(size: size, mtime: mtime, messages: messages, used: Date())
            if c.count > 8, let oldest = c.min(by: { $0.value.used < $1.value.used })?.key {
                c.removeValue(forKey: oldest)
            }
        }
        return messages
    }

    /// No transcript: show the recent screen as one synthetic assistant message.
    func screenMessages(_ a: HerdrAgent) async throws -> [Message] {
        let read = try await herdr.read(a.paneId, source: .recent, lines: 200)
        let text = PathScrubber(cwd: a.cwd).scrub(read.text).trimmingCharacters(in: .newlines)
        guard !text.isEmpty else { return [] }
        return [Message(
            id: "screen:\(a.paneId)",
            role: .assistant,
            createdAt: Timestamps.format(updatedAt(a, transcriptModified: nil)),
            blocks: [.text("```\n\(text)\n```")])]
    }

    // MARK: Approval

    public func approval(id: String) async throws -> Approval? {
        let a = try await herdr.agent(id)
        guard a.agentStatus == .blocked else { return nil }
        let scrubber = PathScrubber(cwd: a.cwd)
        let cwdName = CwdName.of(a.cwd ?? a.foregroundCwd)
        let detection = try await herdr.read(a.paneId, source: .detection)
        var approval = ApprovalParser.parse(detection.text, agentId: a.paneId, scrubber: scrubber, cwdName: cwdName)
        var screen = detection.text
        if approval == nil {
            let visible = try await herdr.read(a.paneId, source: .visible)
            approval = ApprovalParser.parse(visible.text, agentId: a.paneId, scrubber: scrubber, cwdName: cwdName)
            screen = visible.text
        }
        guard var approval else { return ApprovalParser.fallback(detection.text, agentId: a.paneId, scrubber: scrubber) }
        if screen.contains("←") {
            // The active tab is only visible as a highlight, so read with colours.
            let ansi = try? await herdr.read(a.paneId, source: .visible, ansi: true)
            approval.step = ApprovalParser.step(screen, ansi: ansi?.text)
        }
        return approval
    }

    // MARK: Actions

    /// Prompts always go into an empty input box.
    public func prompt(id: String, text: String) async throws {
        try await clearInput(id, wait: .zero)
        try await herdr.prompt(id, text: text)
    }

    /// A stop (`["esc"]`) makes Claude put the interrupted prompt back in the input box; clear it.
    public func sendKeys(id: String, keys: [String]) async throws {
        try await herdr.sendKeys(id, keys: keys)
        if keys == ["esc"] || keys == ["escape"] {
            try await clearInput(id, wait: .milliseconds(1500))
        }
    }

    /// Types literally into whatever is focused (e.g. a free-text answer field). No clearing, no Esc.
    public func text(id: String, text: String, submit: Bool) async throws {
        let a = try await herdr.agent(id)
        try await herdr.sendText(paneId: a.paneId, text: text)
        if submit {
            // Let the TUI take the pasted text before Enter, or Enter can land first.
            try await Task.sleep(for: .milliseconds(150))
            try await herdr.sendKeys(a.paneId, keys: ["enter"])
        }
    }

    /// Empties Claude's input box with `ctrl+u` until the screen shows it empty.
    /// Polls up to `wait` for text to appear (the restored prompt shows up shortly after Esc).
    /// Never touches a blocked agent: keys there would answer a dialog.
    func clearInput(_ id: String, wait: Duration) async throws {
        let clock = ContinuousClock()
        let deadline = clock.now + wait
        var cleared = false
        for _ in 0..<12 {
            let a = try await herdr.agent(id)
            guard a.agentStatus != .blocked, (a.agent ?? "claude") == "claude" else { return }
            let screen = try await herdr.read(a.paneId, source: .detection).text
            guard let content = InputBox.content(screen) else { return }
            if content.isEmpty {
                if cleared || clock.now >= deadline { return }
                try await Task.sleep(for: .milliseconds(150))
                continue
            }
            try await herdr.sendKeys(a.paneId, keys: InputBox.clearKeys(for: content))
            cleared = true
            try await Task.sleep(for: .milliseconds(120))
        }
    }

    public static func isValidName(_ name: String) -> Bool {
        name.wholeMatch(of: /[a-z][a-z0-9_-]{0,31}/) != nil
    }

    /// New tab in the workspace (cwd = its first pane's cwd), `agent.start`, then the optional prompt.
    public func create(workspaceId: String, kind: String, name: String?, prompt: String?) async throws -> Agent {
        let panes = try await herdr.panes(workspaceId: workspaceId)
        guard let first = panes.first else { throw APIError.notFound("no workspace \(workspaceId)") }
        let cwd = first.cwd ?? first.foregroundCwd
        let agentName = name ?? "\(kind.lowercased().filter { $0.isLetter || $0.isNumber })-\(String(UUID().uuidString.lowercased().prefix(4)))"
        let pane = try await herdr.createTab(workspaceId: workspaceId, cwd: cwd, label: name)

        // The new shell may need a moment before herdr considers it available.
        var started: HerdrAgent?
        var lastError: Error?
        for attempt in 0..<6 {
            do {
                started = try await herdr.startAgent(name: agentName, kind: kind, paneId: pane.paneId)
                break
            } catch HerdrError.remote(let code, let message) where code == "agent_not_ready" {
                // Blocked during startup (e.g. a trust dialog): the agent exists, just skip the prompt.
                lastError = HerdrError.remote(code: code, message: message)
                break
            } catch let e as HerdrError {
                lastError = e
                if case .remote(let code, _) = e, code.hasPrefix("invalid") || code.contains("unknown") { break }
                try await Task.sleep(for: .milliseconds(300 * (attempt + 1)))
            }
        }
        if started == nil {
            // agent_not_ready still leaves a live agent behind; anything else is a failure.
            guard case .remote("agent_not_ready", _)? = lastError as? HerdrError else {
                throw lastError ?? HerdrError.io("agent.start failed")
            }
        } else if let prompt, !prompt.isEmpty {
            try await herdr.prompt(pane.paneId, text: prompt)
        }
        var created = try await agent(id: pane.paneId)
        // herdr may not have classified the new agent yet.
        if created.kind == "unknown" { created.kind = kind }
        return created
    }
}
