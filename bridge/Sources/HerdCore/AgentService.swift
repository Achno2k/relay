import Foundation
import Synchronization

/// Everything the REST routes need, re-derived from herdr on every call.
public final class AgentService: Sendable {
    public let herdr: HerdrClient
    public let locator: TranscriptLocator
    private let seen = Mutex<[String: (seq: UInt64, changed: Date?, first: Date)]>([:])
    private let cache = Mutex<[URL: CachedTranscript]>([:])

    private struct CachedTranscript {
        var size: UInt64
        var mtime: Date
        var messages: [Message]
        var used: Date
    }

    public init(herdr: HerdrClient, locator: TranscriptLocator) {
        self.herdr = herdr
        self.locator = locator
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
        return try await agents.map { snapshot($0, workspaceName: names[$0.workspaceId]) }
    }

    public func agents() async throws -> [Agent] {
        try await snapshots().map(\.agent)
    }

    public func snapshot(id: String) async throws -> Snapshot {
        async let raw = herdr.agent(id)
        async let workspaces = herdr.workspaces()
        let a = try await raw
        let name = try await workspaces.first { $0.workspaceId == a.workspaceId }?.label
        return snapshot(a, workspaceName: name)
    }

    public func agent(id: String) async throws -> Agent {
        try await snapshot(id: id).agent
    }

    func snapshot(_ a: HerdrAgent, workspaceName: String?) -> Snapshot {
        let ref = locator.locate(a)
        let mtime = ref.flatMap { try? $0.url.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate }
        let agent = Agent(
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
        return Snapshot(agent: agent, raw: a, transcript: ref)
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
        let detection = try await herdr.read(id, source: .detection)
        if let ap = ApprovalParser.parse(detection.text, agentId: a.paneId, scrubber: scrubber) { return ap }
        let visible = try await herdr.read(id, source: .visible)
        if let ap = ApprovalParser.parse(visible.text, agentId: a.paneId, scrubber: scrubber) { return ap }
        return ApprovalParser.fallback(detection.text, agentId: a.paneId, scrubber: scrubber)
    }

    // MARK: Actions

    public func prompt(id: String, text: String) async throws {
        try await herdr.prompt(id, text: text)
    }

    public func sendKeys(id: String, keys: [String]) async throws {
        try await herdr.sendKeys(id, keys: keys)
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
        return try await agent(id: pane.paneId)
    }
}
