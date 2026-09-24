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

    public init(
        id: String, name: String?, kind: String, title: String,
        workspaceId: String, workspaceName: String, cwdName: String,
        status: AgentStatus, hasTranscript: Bool, updatedAt: Date,
        model: String? = nil, modelLabel: String? = nil, permissionMode: String? = nil,
        effort: String? = nil, sessionId: String? = nil
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

    public init(models: [ControlOption], efforts: [ControlOption], modes: [ControlOption], supports: Supports) {
        self.models = models
        self.efforts = efforts
        self.modes = modes
        self.supports = supports
    }

    /// What an older bridge (no per-agent route) offers: Claude's global list, everything supported.
    public init(claudeCatalog catalog: ControlsCatalog) {
        self.init(
            models: catalog.models, efforts: catalog.efforts, modes: catalog.modes,
            supports: Supports(model: true, effort: true, mode: true, compact: true, clear: true)
        )
    }

    private enum CodingKeys: String, CodingKey { case models, efforts, modes, supports }

    /// Lists a kind doesn't have may be missing or null.
    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        models = try c.decodeIfPresent([ControlOption].self, forKey: .models) ?? []
        efforts = try c.decodeIfPresent([ControlOption].self, forKey: .efforts) ?? []
        modes = try c.decodeIfPresent([ControlOption].self, forKey: .modes) ?? []
        supports = try c.decode(Supports.self, forKey: .supports)
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
    public var prompt: String?

    public init(workspaceId: String, kind: String, name: String? = nil, prompt: String? = nil) {
        self.workspaceId = workspaceId
        self.kind = kind
        self.name = name
        self.prompt = prompt
    }
}

/// One WebSocket frame.
public enum ServerEvent: Decodable, Hashable, Sendable {
    case hello
    case agentUpdated(Agent)
    case agentCreated(Agent)
    case agentClosed(agentId: String)
    case messageUpserted(agentId: String, message: Message)
    case unknown(type: String)

    private enum CodingKeys: String, CodingKey {
        case type, agent, agentId, message
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
