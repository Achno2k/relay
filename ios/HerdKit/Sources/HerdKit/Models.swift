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

    public init(
        id: String, name: String?, kind: String, title: String,
        workspaceId: String, workspaceName: String, cwdName: String,
        status: AgentStatus, hasTranscript: Bool, updatedAt: Date
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
        case .unknown(let type):
            try c.encode(type, forKey: .type)
        }
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
