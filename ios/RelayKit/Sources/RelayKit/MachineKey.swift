import Foundation

/// App-wide keys for anything whose id is only unique on one bridge (agents, workspaces): `"<machineId>/<rawId>"`.
/// Machine ids never contain `/` (api.md "Multiple machines"), so the first `/` splits a key.
/// Views and stores only ever see keys; bridges only ever see raw ids (`NamespacedBackend` converts).
public enum MachineKey {
    public static let separator: Character = "/"

    public static func make(_ machineId: String, _ raw: String) -> String {
        "\(machineId)\(separator)\(raw)"
    }

    /// nil for a bare id (no machine part).
    public static func machineId(_ key: String) -> String? {
        guard let i = key.firstIndex(of: separator) else { return nil }
        return String(key[..<i])
    }

    /// The id the bridge knows. A bare id comes back unchanged.
    public static func raw(_ key: String) -> String {
        guard let i = key.firstIndex(of: separator) else { return key }
        return String(key[key.index(after: i)...])
    }

    /// Whether `key` belongs to `machineId`.
    public static func belongs(_ key: String, to machineId: String) -> Bool {
        Self.machineId(key) == machineId
    }
}

// MARK: - Re-keying wire models

public extension Workspace {
    func keyed(_ machineId: String) -> Workspace {
        Workspace(id: MachineKey.make(machineId, id), name: name, agentCount: agentCount)
    }
}

public extension Agent {
    func keyed(_ machineId: String) -> Agent {
        Agent(
            id: MachineKey.make(machineId, id), name: name, kind: kind, title: title,
            workspaceId: MachineKey.make(machineId, workspaceId), workspaceName: workspaceName, cwdName: cwdName,
            status: status, hasTranscript: hasTranscript, updatedAt: updatedAt,
            model: model, modelLabel: modelLabel, permissionMode: permissionMode,
            effort: effort, sessionId: sessionId, transcriptState: transcriptState
        )
    }
}

public extension Approval {
    func keyed(_ machineId: String) -> Approval {
        var copy = self
        copy.agentId = MachineKey.make(machineId, agentId)
        return copy
    }
}

public extension ServerEvent {
    func keyed(_ machineId: String) -> ServerEvent {
        let key = { MachineKey.make(machineId, $0) }
        switch self {
        case .hello, .unknown, .usageUpdated: return self
        case .agentUpdated(let a): return .agentUpdated(a.keyed(machineId))
        case .agentCreated(let a): return .agentCreated(a.keyed(machineId))
        case .agentClosed(let id): return .agentClosed(agentId: key(id))
        case .messageUpserted(let id, let m): return .messageUpserted(agentId: key(id), message: m)
        case .replyLive(let id, let text, let seq, let tool): return .replyLive(agentId: key(id), text: text, seq: seq, tool: tool)
        }
    }
}

// MARK: - NamespacedBackend

/// One machine's backend as the app sees it: every id going out is stripped to the raw id, every id coming
/// back is keyed with `machineId`. Message and attachment ids stay raw; they always live under an agent key.
public struct NamespacedBackend: Backend {
    public let machineId: String
    public let inner: any Backend

    public init(machineId: String, inner: any Backend) {
        self.machineId = machineId
        self.inner = inner
    }

    private func raw(_ key: String) -> String { MachineKey.raw(key) }

    public func workspaces() async throws -> [Workspace] { try await inner.workspaces().map { $0.keyed(machineId) } }
    public func agents() async throws -> [Agent] { try await inner.agents().map { $0.keyed(machineId) } }
    public func messages(agentId: String, before: String?, limit: Int) async throws -> MessagePage {
        try await inner.messages(agentId: raw(agentId), before: before, limit: limit)
    }
    public func prompt(agentId: String, text: String, attachments: [String]) async throws {
        try await inner.prompt(agentId: raw(agentId), text: text, attachments: attachments)
    }
    public func uploadAttachment(
        agentId: String, data: Data, filename: String, contentType: String,
        progress: @escaping @Sendable (Double) -> Void
    ) async throws -> Attachment {
        try await inner.uploadAttachment(agentId: raw(agentId), data: data, filename: filename, contentType: contentType, progress: progress)
    }
    public func attachmentData(agentId: String, attachmentId: String) async throws -> Data {
        try await inner.attachmentData(agentId: raw(agentId), attachmentId: attachmentId)
    }
    public func file(agentId: String, path: String) async throws -> AgentFile {
        try await inner.file(agentId: raw(agentId), path: path)
    }
    public func machine() async throws -> Machine { try await inner.machine() }
    public func health() async throws -> Health { try await inner.health() }
    public func sendKeys(agentId: String, keys: [String]) async throws { try await inner.sendKeys(agentId: raw(agentId), keys: keys) }
    public func sendText(agentId: String, text: String, submit: Bool) async throws {
        try await inner.sendText(agentId: raw(agentId), text: text, submit: submit)
    }
    public func approval(agentId: String) async throws -> Approval? {
        try await inner.approval(agentId: raw(agentId))?.keyed(machineId)
    }
    public func createAgent(_ request: CreateAgentRequest) async throws -> Agent {
        var request = request
        request.workspaceId = raw(request.workspaceId)
        return try await inner.createAgent(request).keyed(machineId)
    }
    public func controls() async throws -> ControlsCatalog { try await inner.controls() }
    public func kindControls(kind: String) async throws -> AgentControlsInfo { try await inner.kindControls(kind: kind) }
    public func agentControls(agentId: String) async throws -> AgentControlsInfo { try await inner.agentControls(agentId: raw(agentId)) }
    public func control(agentId: String, _ request: ControlRequest) async throws -> Agent {
        try await inner.control(agentId: raw(agentId), request).keyed(machineId)
    }
    public func events() -> AsyncStream<ConnectionEvent> {
        let source = inner.events()
        let machineId = machineId
        return AsyncStream { continuation in
            let task = Task {
                for await event in source {
                    if case .event(let e) = event {
                        continuation.yield(.event(e.keyed(machineId)))
                    } else {
                        continuation.yield(event)
                    }
                }
                continuation.finish()
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }
    public func usage() async throws -> UsageSnapshot { try await inner.usage() }
    public func refreshUsage() async throws { try await inner.refreshUsage() }
}
