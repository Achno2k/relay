import Foundation

// Wire models. See docs/api.md; keep this file in step with the contract.

public struct Workspace: Codable, Hashable, Identifiable, Sendable {
    public let id: String
    public var name: String
    public var agentCount: Int

    public init(id: String, name: String, agentCount: Int) {
        self.id = id
        self.name = name
        self.agentCount = agentCount
    }
}

public enum AgentStatus: String, Codable, Hashable, Sendable {
    case idle, working, blocked, done, unknown

    public init(from decoder: any Decoder) throws {
        let raw = try decoder.singleValueContainer().decode(String.self)
        self = AgentStatus(rawValue: raw) ?? .unknown
    }
}

/// Whether the bridge has a real transcript for an agent.
/// `pending`: it will once the first message is sent (a brand-new agent). `unsupported`: this kind has none,
/// so messages are a read of the screen.
public enum TranscriptState: String, Codable, Hashable, Sendable {
    case ready, pending, unsupported

    public init(from decoder: any Decoder) throws {
        self = TranscriptState(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .ready
    }
}

public struct Agent: Codable, Hashable, Identifiable, Sendable {
    public let id: String
    public var name: String?
    public var kind: String
    public var title: String
    public var workspaceId: String
    public var workspaceName: String
    public var cwdName: String
    public var status: AgentStatus
    public var hasTranscript: Bool
    public var updatedAt: Date
    /// Claude only; nil when unknown. Full model id from the transcript, e.g. "claude-opus-5-5".
    public var model: String?
    /// Display name for `model`, e.g. "Opus 5.5"; the bridge owns the mapping.
    public var modelLabel: String?
    /// `default | acceptEdits | plan | auto | bypassPermissions`.
    public var permissionMode: String?
    public var effort: String?
    /// Changes when the agent starts a new session (e.g. after /clear); the chat must be refetched.
    public var sessionId: String?
    /// Absent on older bridges; see `transcript`.
    public var transcriptState: TranscriptState?

    public init(
        id: String, name: String?, kind: String, title: String,
        workspaceId: String, workspaceName: String, cwdName: String,
        status: AgentStatus, hasTranscript: Bool, updatedAt: Date,
        model: String? = nil, modelLabel: String? = nil, permissionMode: String? = nil,
        effort: String? = nil, sessionId: String? = nil, transcriptState: TranscriptState? = nil
    ) {
        self.id = id
        self.name = name
        self.kind = kind
        self.title = title
        self.workspaceId = workspaceId
        self.workspaceName = workspaceName
        self.cwdName = cwdName
        self.status = status
        self.hasTranscript = hasTranscript
        self.updatedAt = updatedAt
        self.model = model
        self.modelLabel = modelLabel
        self.permissionMode = permissionMode
        self.effort = effort
        self.sessionId = sessionId
        self.transcriptState = transcriptState
    }

    /// Kinds the bridge can read a transcript for (api.md).
    public static let transcriptKinds: Set<String> = ["claude", "pi", "codex"]

    /// `transcriptState`, or what api.md says to assume when an older bridge doesn't send it.
    public var transcript: TranscriptState {
        transcriptState ?? (hasTranscript ? .ready : Self.transcriptKinds.contains(kind) ? .pending : .unsupported)
    }

    /// Title for lists and the chat header. herdr falls back to the kind when a pane has no title.
    public var displayTitle: String {
        let trimmed = title.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty || trimmed == kind { return name ?? kind.capitalized }
        return trimmed
    }

    /// Short name for "Message <name>".
    public var displayName: String { name ?? kind.capitalized }
}

public struct Message: Codable, Hashable, Identifiable, Sendable {
    public enum Role: String, Codable, Hashable, Sendable {
        case user, assistant
    }

    public let id: String
    public var role: Role
    public var createdAt: Date
    public var blocks: [Block]

    public init(id: String, role: Role, createdAt: Date, blocks: [Block]) {
        self.id = id
        self.role = role
        self.createdAt = createdAt
        self.blocks = blocks
    }

    /// Concatenated text blocks, used to match optimistic prompts against the transcript.
    public var plainText: String {
        blocks.compactMap { if case .text(let t) = $0 { t } else { nil } }.joined(separator: "\n")
    }

    public var attachments: [AttachmentRef] {
        blocks.compactMap { if case .attachment(let a) = $0 { a } else { nil } }
    }
}

public struct ToolCall: Codable, Hashable, Sendable {
    public let id: String
    public var name: String
    public var summary: String
    public var input: String?

    public init(id: String, name: String, summary: String, input: String? = nil) {
        self.id = id
        self.name = name
        self.summary = summary
        self.input = input
    }
}

public struct ToolResult: Codable, Hashable, Sendable {
    public var toolCallId: String
    public var isError: Bool
    public var preview: String?

    public init(toolCallId: String, isError: Bool, preview: String? = nil) {
        self.toolCallId = toolCallId
        self.isError = isError
        self.preview = preview
    }
}

/// Tagged on `"type"`. Unknown types decode to `.unknown` so a newer bridge can't break the app.
public enum Block: Codable, Hashable, Sendable {
    case text(String)
    case thinking(String)
    case toolCall(ToolCall)
    case toolResult(ToolResult)
    /// A file sent with a user prompt; fetch it with `GET /agents/:id/attachments/:attachmentId`.
    case attachment(AttachmentRef)
    case unknown(type: String)

    private enum CodingKeys: String, CodingKey {
        case type, text
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        let type = try c.decode(String.self, forKey: .type)
        switch type {
        case "text": self = .text(try c.decode(String.self, forKey: .text))
        case "thinking": self = .thinking(try c.decode(String.self, forKey: .text))
        case "toolCall": self = .toolCall(try ToolCall(from: decoder))
        case "toolResult": self = .toolResult(try ToolResult(from: decoder))
        case "attachment": self = .attachment(try AttachmentRef(from: decoder))
        default: self = .unknown(type: type)
        }
    }

    public func encode(to encoder: any Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        switch self {
        case .text(let t):
            try c.encode("text", forKey: .type)
            try c.encode(t, forKey: .text)
        case .thinking(let t):
            try c.encode("thinking", forKey: .type)
            try c.encode(t, forKey: .text)
        case .toolCall(let call):
            try c.encode("toolCall", forKey: .type)
            try call.encode(to: encoder)
        case .toolResult(let result):
            try c.encode("toolResult", forKey: .type)
            try result.encode(to: encoder)
        case .attachment(let ref):
            try c.encode("attachment", forKey: .type)
            try ref.encode(to: encoder)
        case .unknown(let type):
            try c.encode(type, forKey: .type)
        }
    }
}

public enum AttachmentKind: String, Codable, Hashable, Sendable {
    case image, pdf, file

    public init(from decoder: any Decoder) throws {
        self = AttachmentKind(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .file
    }
}

/// An attachment as it appears in history (no size, never a path).
public struct AttachmentRef: Codable, Hashable, Identifiable, Sendable {
    public var id: String
    public var name: String
    public var kind: AttachmentKind

    public init(id: String, name: String, kind: AttachmentKind) {
        self.id = id
        self.name = name
        self.kind = kind
    }
}

/// `201` from `POST /agents/:id/attachments`.
public struct Attachment: Codable, Hashable, Identifiable, Sendable {
    public var id: String
    public var name: String
    public var kind: AttachmentKind
    public var size: Int

    public init(id: String, name: String, kind: AttachmentKind, size: Int) {
        self.id = id
        self.name = name
        self.kind = kind
        self.size = size
    }

    public var ref: AttachmentRef { AttachmentRef(id: id, name: name, kind: kind) }
}

/// `GET /machine`: the Mac a bridge runs on.
public struct Machine: Codable, Hashable, Identifiable, Sendable {
    public enum Kind: String, Codable, Hashable, Sendable {
        case laptop, desktop

        public init(from decoder: any Decoder) throws {
            self = Kind(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .desktop
        }
    }

    public var id: String
    public var name: String
    public var kind: Kind
    public var model: String?
    public var os: String?

    public init(id: String, name: String, kind: Kind, model: String? = nil, os: String? = nil) {
        self.id = id
        self.name = name
        self.kind = kind
        self.model = model
        self.os = os
    }
}

public struct MessagePage: Codable, Hashable, Sendable {
    public var messages: [Message]
    public var hasMore: Bool

    public init(messages: [Message], hasMore: Bool) {
        self.messages = messages
        self.hasMore = hasMore
    }
}

public struct ApprovalOption: Codable, Hashable, Sendable {
    public var label: String
    public var keys: [String]
    /// Choosing it opens a text input in the agent's menu (Claude's "Type something." row).
    public var freeText: Bool?

    public init(label: String, keys: [String], freeText: Bool? = nil) {
        self.label = label
        self.keys = keys
        self.freeText = freeText
    }

    /// Bridges that predate `freeText` still send Claude's label.
    public var isFreeText: Bool {
        freeText ?? (label.trimmingCharacters(in: .whitespaces) == "Type something.")
    }
}

/// Progress through a multi-question prompt, e.g. question 2 of 3, "Focus".
public struct ApprovalStep: Codable, Hashable, Sendable {
    /// 1-based.
    public var index: Int
    public var count: Int
    public var title: String?

    public init(index: Int, count: Int, title: String? = nil) {
        self.index = index
        self.count = count
        self.title = title
    }
}

public struct Approval: Codable, Hashable, Sendable {
    public var agentId: String
    public var question: String
    public var options: [ApprovalOption]
    public var step: ApprovalStep?

    public init(agentId: String, question: String, options: [ApprovalOption], step: ApprovalStep? = nil) {
        self.agentId = agentId
        self.question = question
        self.options = options
        self.step = step
    }
}

/// One choice in `GET /controls`.
public struct ControlOption: Codable, Hashable, Identifiable, Sendable {
    public var id: String
    public var label: String

    public init(id: String, label: String) {
        self.id = id
        self.label = label
    }
}

/// `GET /controls`: what the app may offer. The list lives in the bridge.
public struct ControlsCatalog: Codable, Hashable, Sendable {
    public var models: [ControlOption]
    public var modes: [ControlOption]
    public var efforts: [ControlOption]

    public init(models: [ControlOption], modes: [ControlOption], efforts: [ControlOption]) {
        self.models = models
        self.modes = modes
        self.efforts = efforts
    }
}

/// `GET /agents/:id/controls`: what this agent's kind can change, with its own model and effort lists.
public struct AgentControlsInfo: Codable, Hashable, Sendable {
    public struct Supports: Codable, Hashable, Sendable {
        public var model: Bool
        public var effort: Bool
        public var mode: Bool
        public var compact: Bool
        public var clear: Bool

        public init(model: Bool, effort: Bool, mode: Bool, compact: Bool, clear: Bool) {
            self.model = model
            self.effort = effort
            self.mode = mode
            self.compact = compact
            self.clear = clear
        }

        public var any: Bool { model || effort || mode || compact || clear }
    }

    public var models: [ControlOption]
    public var efforts: [ControlOption]
    public var modes: [ControlOption]
    public var supports: Supports
    /// From `GET /controls?kind=`: what a new agent starts with if nothing is picked.
    public var defaultModel: String?
    public var defaultEffort: String?
    /// pi and codex: efforts differ per model. Claude's models all share `efforts`.
    public var effortsByModel: [String: [ControlOption]]?

    public init(
        models: [ControlOption], efforts: [ControlOption], modes: [ControlOption], supports: Supports,
        defaultModel: String? = nil, defaultEffort: String? = nil, effortsByModel: [String: [ControlOption]]? = nil
    ) {
        self.models = models
        self.efforts = efforts
        self.modes = modes
        self.supports = supports
        self.defaultModel = defaultModel
        self.defaultEffort = defaultEffort
        self.effortsByModel = effortsByModel
    }

    /// Effort choices for a model: its own list when the kind has per-model efforts, else the shared one.
    public func efforts(for model: String?) -> [ControlOption] {
        if let model, let list = effortsByModel?[model] { return list }
        return efforts
    }

    /// What an older bridge (no per-agent route) offers: Claude's global list, everything supported.
    public init(claudeCatalog catalog: ControlsCatalog) {
        self.init(
            models: catalog.models, efforts: catalog.efforts, modes: catalog.modes,
            supports: Supports(model: true, effort: true, mode: true, compact: true, clear: true)
        )
    }

    private enum CodingKeys: String, CodingKey {
        case models, efforts, modes, supports, defaultModel, defaultEffort, effortsByModel
    }

    /// Lists a kind doesn't have may be missing or null.
    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        models = try c.decodeIfPresent([ControlOption].self, forKey: .models) ?? []
        efforts = try c.decodeIfPresent([ControlOption].self, forKey: .efforts) ?? []
        modes = try c.decodeIfPresent([ControlOption].self, forKey: .modes) ?? []
        supports = try c.decode(Supports.self, forKey: .supports)
        defaultModel = try c.decodeIfPresent(String.self, forKey: .defaultModel)
        defaultEffort = try c.decodeIfPresent(String.self, forKey: .defaultEffort)
        effortsByModel = try c.decodeIfPresent([String: [ControlOption]].self, forKey: .effortsByModel)
    }
}

/// Body of `POST /agents/:id/control`; exactly one key per call.
public enum ControlRequest: Hashable, Sendable, Encodable {
    /// The bridge confirms model/mode/effort within ~10 s, `/clear` within ~30 s and `/compact` within ~90 s.
    public var timeout: TimeInterval {
        switch self {
        case .command(.compact): 120
        case .command(.clear): 60
        default: 20
        }
    }

    case model(String)
    case permissionMode(String)
    case effort(String)
    case command(Command)

    public enum Command: String, Hashable, Sendable {
        case compact, clear
    }

    private enum CodingKeys: String, CodingKey {
        case model, permissionMode, effort, command
    }

    public func encode(to encoder: any Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        switch self {
        case .model(let v): try c.encode(v, forKey: .model)
        case .permissionMode(let v): try c.encode(v, forKey: .permissionMode)
        case .effort(let v): try c.encode(v, forKey: .effort)
        case .command(let v): try c.encode(v.rawValue, forKey: .command)
        }
    }
}

public struct CreateAgentRequest: Codable, Hashable, Sendable {
    public var workspaceId: String
    public var kind: String
    public var name: String?
    /// Ids from `GET /controls?kind=`; nil means the agent's saved default.
    public var model: String?
    public var effort: String?
    /// Still accepted by the bridge; the app sends the first message with `POST /prompt` instead.
    public var prompt: String?

    public init(
        workspaceId: String, kind: String, name: String? = nil,
        model: String? = nil, effort: String? = nil, prompt: String? = nil
    ) {
        self.workspaceId = workspaceId
        self.kind = kind
        self.name = name
        self.model = model
        self.effort = effort
        self.prompt = prompt
    }
}

/// See api.md "Usage".
public struct UsageWindow: Codable, Hashable, Identifiable, Sendable {
    public var id: String
    public var label: String
    public var usedPercent: Double?
    public var windowMinutes: Int?
    public var resetsAt: Date?

    public init(id: String, label: String, usedPercent: Double?, windowMinutes: Int? = nil, resetsAt: Date? = nil) {
        self.id = id
        self.label = label
        self.usedPercent = usedPercent
        self.windowMinutes = windowMinutes
        self.resetsAt = resetsAt
    }
}

public struct UsageProvider: Codable, Hashable, Identifiable, Sendable {
    public var id: String
    public var label: String
    public var plan: String?
    public var windows: [UsageWindow]
    public var updatedAt: Date
    public var source: String
    public var stale: Bool
    public var unavailableReason: String?
    /// Which herdr-driven harnesses (`"claude"`, `"codex"`, `"pi"`) are authenticated against this
    /// subscription on this Mac. See api.md "Usage".
    public var usedBy: [String]

    public init(
        id: String, label: String, plan: String? = nil, windows: [UsageWindow], updatedAt: Date,
        source: String, stale: Bool, unavailableReason: String? = nil, usedBy: [String] = []
    ) {
        self.id = id
        self.label = label
        self.plan = plan
        self.windows = windows
        self.updatedAt = updatedAt
        self.source = source
        self.stale = stale
        self.unavailableReason = unavailableReason
        self.usedBy = usedBy
    }

    private enum CodingKeys: String, CodingKey {
        case id, label, plan, windows, updatedAt, source, stale, unavailableReason, usedBy
    }

    /// Manual `init(from:)` so a bridge that predates `usedBy` still decodes (`decodeIfPresent`,
    /// defaulting to `[]`); `encode(to:)` stays compiler-synthesized.
    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        label = try c.decode(String.self, forKey: .label)
        plan = try c.decodeIfPresent(String.self, forKey: .plan)
        windows = try c.decode([UsageWindow].self, forKey: .windows)
        updatedAt = try c.decode(Date.self, forKey: .updatedAt)
        source = try c.decode(String.self, forKey: .source)
        stale = try c.decode(Bool.self, forKey: .stale)
        unavailableReason = try c.decodeIfPresent(String.self, forKey: .unavailableReason)
        usedBy = try c.decodeIfPresent([String].self, forKey: .usedBy) ?? []
    }
}

/// `GET /usage`.
public struct UsageSnapshot: Codable, Hashable, Sendable {
    public var providers: [UsageProvider]

    public init(providers: [UsageProvider]) {
        self.providers = providers
    }
}

/// The tool call an agent is running right now, read off its screen (`reply.live.tool`). `summary` is
/// scrubbed the same way as a transcript `ToolCall.summary`, so the two can be matched.
public struct LiveTool: Codable, Hashable, Sendable {
    public var name: String
    public var summary: String
    /// "running" today; kept as a string so a new state doesn't break decoding.
    public var state: String

    public init(name: String, summary: String, state: String = "running") {
        self.name = name
        self.summary = summary
        self.state = state
    }

    private enum CodingKeys: String, CodingKey { case name, summary, state }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        name = try c.decode(String.self, forKey: .name)
        summary = try c.decodeIfPresent(String.self, forKey: .summary) ?? ""
        state = try c.decodeIfPresent(String.self, forKey: .state) ?? "running"
    }
}

/// One WebSocket frame.
public enum ServerEvent: Decodable, Hashable, Sendable {
    case hello
    case agentUpdated(Agent)
    case agentCreated(Agent)
    case agentClosed(agentId: String)
    case messageUpserted(agentId: String, message: Message)
    /// In-progress assistant text preview while an agent is working; `text: nil` clears it. `tool` is
    /// the tool call running on screen before the transcript has it; `nil` clears it. See api.md "Live reply".
    case replyLive(agentId: String, text: String?, seq: Int, tool: LiveTool? = nil)
    /// One provider's usage snapshot changed. See api.md "Usage".
    case usageUpdated(UsageProvider)
    case unknown(type: String)

    private enum CodingKeys: String, CodingKey {
        case type, agent, agentId, message, text, seq, provider, tool
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        let type = try c.decode(String.self, forKey: .type)
        switch type {
        case "hello": self = .hello
        case "agent.updated": self = .agentUpdated(try c.decode(Agent.self, forKey: .agent))
        case "agent.created": self = .agentCreated(try c.decode(Agent.self, forKey: .agent))
        case "agent.closed": self = .agentClosed(agentId: try c.decode(String.self, forKey: .agentId))
        case "message.upserted":
            self = .messageUpserted(
                agentId: try c.decode(String.self, forKey: .agentId),
                message: try c.decode(Message.self, forKey: .message)
            )
        case "reply.live":
            self = .replyLive(
                agentId: try c.decode(String.self, forKey: .agentId),
                text: try c.decodeIfPresent(String.self, forKey: .text),
                seq: try c.decode(Int.self, forKey: .seq),
                // An older bridge has no `tool`; a malformed one is ignored rather than dropping the frame.
                tool: (try? c.decodeIfPresent(LiveTool.self, forKey: .tool)) ?? nil
            )
        case "usage.updated":
            self = .usageUpdated(try c.decode(UsageProvider.self, forKey: .provider))
        default: self = .unknown(type: type)
        }
    }
}

/// What a backend's event stream carries: socket state plus decoded frames.
public enum ConnectionEvent: Hashable, Sendable {
    case connected
    case disconnected
    case event(ServerEvent)
}

struct APIErrorBody: Decodable {
    struct Detail: Decodable {
        var code: String
        var message: String
    }
    var error: Detail
}
