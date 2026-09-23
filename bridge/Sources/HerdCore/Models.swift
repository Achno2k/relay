import Foundation

public enum Herd {
    public static let version = "0.1.0"
}

public struct Workspace: Codable, Sendable, Equatable {
    public var id: String
    public var name: String
    public var agentCount: Int
}

public enum AgentStatus: String, Codable, Sendable {
    case idle, working, blocked, done, unknown
}

public struct Agent: Codable, Sendable, Equatable {
    public var id: String
    public var name: String?
    public var kind: String
    public var title: String
    public var workspaceId: String
    public var workspaceName: String
    public var cwdName: String
    public var status: AgentStatus
    public var hasTranscript: Bool
    public var updatedAt: String

    // Encode `name` as an explicit null so the app sees a stable shape.
    public func encode(to encoder: any Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(id, forKey: .id)
        try c.encode(name, forKey: .name)
        try c.encode(kind, forKey: .kind)
        try c.encode(title, forKey: .title)
        try c.encode(workspaceId, forKey: .workspaceId)
        try c.encode(workspaceName, forKey: .workspaceName)
        try c.encode(cwdName, forKey: .cwdName)
        try c.encode(status, forKey: .status)
        try c.encode(hasTranscript, forKey: .hasTranscript)
        try c.encode(updatedAt, forKey: .updatedAt)
    }
}

public enum Role: String, Codable, Sendable {
    case user, assistant
}

public enum Block: Codable, Sendable, Equatable {
    case text(String)
    case thinking(String)
    case toolCall(id: String, name: String, summary: String, input: String)
    case toolResult(toolCallId: String, isError: Bool, preview: String)

    private enum CodingKeys: String, CodingKey {
        case type, text, id, name, summary, input, toolCallId, isError, preview
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        switch try c.decode(String.self, forKey: .type) {
        case "text": self = .text(try c.decode(String.self, forKey: .text))
        case "thinking": self = .thinking(try c.decode(String.self, forKey: .text))
        case "toolCall":
            self = .toolCall(
                id: try c.decode(String.self, forKey: .id),
                name: try c.decode(String.self, forKey: .name),
                summary: try c.decode(String.self, forKey: .summary),
                input: try c.decode(String.self, forKey: .input))
        case "toolResult":
            self = .toolResult(
                toolCallId: try c.decode(String.self, forKey: .toolCallId),
                isError: try c.decode(Bool.self, forKey: .isError),
                preview: try c.decode(String.self, forKey: .preview))
        case let other:
            throw DecodingError.dataCorruptedError(forKey: .type, in: c, debugDescription: "unknown block type \(other)")
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
        case .toolCall(let id, let name, let summary, let input):
            try c.encode("toolCall", forKey: .type)
            try c.encode(id, forKey: .id)
            try c.encode(name, forKey: .name)
            try c.encode(summary, forKey: .summary)
            try c.encode(input, forKey: .input)
        case .toolResult(let toolCallId, let isError, let preview):
            try c.encode("toolResult", forKey: .type)
            try c.encode(toolCallId, forKey: .toolCallId)
            try c.encode(isError, forKey: .isError)
            try c.encode(preview, forKey: .preview)
        }
    }
}

public struct Message: Codable, Sendable, Equatable {
    public var id: String
    public var role: Role
    public var createdAt: String
    public var blocks: [Block]

    public init(id: String, role: Role, createdAt: String, blocks: [Block]) {
        self.id = id
        self.role = role
        self.createdAt = createdAt
        self.blocks = blocks
    }
}

public struct MessagePage: Codable, Sendable, Equatable {
    public var messages: [Message]
    public var hasMore: Bool
}

public struct ApprovalOption: Codable, Sendable, Equatable {
    public var label: String
    public var keys: [String]
    /// True on Claude's "Type something." row: send `keys`, then the answer via `POST /agents/:id/text`.
    public var freeText: Bool?

    public init(label: String, keys: [String], freeText: Bool? = nil) {
        self.label = label
        self.keys = keys
        self.freeText = freeText
    }
}

/// Progress through a multi-question prompt. `index` is 1-based; `count` includes the Submit tab.
public struct ApprovalStep: Codable, Sendable, Equatable {
    public var index: Int
    public var count: Int
    public var title: String?

    public init(index: Int, count: Int, title: String?) {
        self.index = index
        self.count = count
        self.title = title
    }
}

public struct Approval: Codable, Sendable, Equatable {
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

public enum ServerEvent: Sendable, Equatable {
    case hello
    case agentUpdated(Agent)
    case agentCreated(Agent)
    case agentClosed(String)
    case messageUpserted(agentId: String, message: Message)
}

extension ServerEvent: Encodable {
    private enum CodingKeys: String, CodingKey { case type, agent, agentId, message }

    public func encode(to encoder: any Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        switch self {
        case .hello:
            try c.encode("hello", forKey: .type)
        case .agentUpdated(let a):
            try c.encode("agent.updated", forKey: .type)
            try c.encode(a, forKey: .agent)
        case .agentCreated(let a):
            try c.encode("agent.created", forKey: .type)
            try c.encode(a, forKey: .agent)
        case .agentClosed(let id):
            try c.encode("agent.closed", forKey: .type)
            try c.encode(id, forKey: .agentId)
        case .messageUpserted(let id, let m):
            try c.encode("message.upserted", forKey: .type)
            try c.encode(id, forKey: .agentId)
            try c.encode(m, forKey: .message)
        }
    }
}

public enum Timestamps {
    /// ISO 8601 with an explicit offset, e.g. `2026-09-23T13:04:01+00:00`.
    public static func format(_ date: Date) -> String {
        var cal = Calendar(identifier: .gregorian)
        cal.timeZone = TimeZone(identifier: "UTC")!
        let c = cal.dateComponents([.year, .month, .day, .hour, .minute, .second], from: date)
        func p(_ v: Int?, _ w: Int = 2) -> String {
            let s = String(v ?? 0)
            return String(repeating: "0", count: max(0, w - s.count)) + s
        }
        return "\(p(c.year, 4))-\(p(c.month))-\(p(c.day))T\(p(c.hour)):\(p(c.minute)):\(p(c.second))+00:00"
    }

    /// Normalises transcript timestamps (`...Z`, fractional seconds, or epoch millis) to `format`.
    public static func normalize(_ raw: Any?) -> String? {
        if let s = raw as? String {
            if let d = try? Date(s, strategy: .iso8601.year().month().day().time(includingFractionalSeconds: true).timeZone(separator: .omitted)) {
                return format(d)
            }
            if let d = try? Date(s, strategy: .iso8601) { return format(d) }
            return nil
        }
        if let n = raw as? NSNumber {
            return format(Date(timeIntervalSince1970: n.doubleValue / 1000))
        }
        return nil
    }
}
