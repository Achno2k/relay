import Foundation

public enum Relay {
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
    public var model: String? = nil
    public var modelLabel: String? = nil
    public var permissionMode: String? = nil
    public var effort: String? = nil
    public var sessionId: String? = nil
    /// `ready`: a transcript was found. `pending`: this kind has a transcript but it doesn't exist yet
    /// (a new agent before its first message). `unsupported`: no transcript parser for this kind;
    /// messages are a screen read.
    public var transcriptState: TranscriptState = .unsupported

    enum CodingKeys: String, CodingKey {
        case id, name, kind, title, workspaceId, workspaceName, cwdName, status, hasTranscript, updatedAt
        case model, modelLabel, permissionMode, effort, sessionId, transcriptState
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        name = try c.decodeIfPresent(String.self, forKey: .name)
        kind = try c.decode(String.self, forKey: .kind)
        title = try c.decode(String.self, forKey: .title)
        workspaceId = try c.decode(String.self, forKey: .workspaceId)
        workspaceName = try c.decode(String.self, forKey: .workspaceName)
        cwdName = try c.decode(String.self, forKey: .cwdName)
        status = try c.decode(AgentStatus.self, forKey: .status)
        hasTranscript = try c.decode(Bool.self, forKey: .hasTranscript)
        updatedAt = try c.decode(String.self, forKey: .updatedAt)
        model = try c.decodeIfPresent(String.self, forKey: .model)
        modelLabel = try c.decodeIfPresent(String.self, forKey: .modelLabel)
        permissionMode = try c.decodeIfPresent(String.self, forKey: .permissionMode)
        effort = try c.decodeIfPresent(String.self, forKey: .effort)
        sessionId = try c.decodeIfPresent(String.self, forKey: .sessionId)
        // Older bridges don't send it: derive it the same way.
        transcriptState = try c.decodeIfPresent(TranscriptState.self, forKey: .transcriptState)
            ?? .of(kind: kind, hasTranscript: hasTranscript)
    }

    public init(id: String, name: String?, kind: String, title: String, workspaceId: String, workspaceName: String,
                cwdName: String, status: AgentStatus, hasTranscript: Bool, updatedAt: String) {
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
        try c.encode(model, forKey: .model)
        try c.encode(modelLabel, forKey: .modelLabel)
        try c.encode(permissionMode, forKey: .permissionMode)
        try c.encode(effort, forKey: .effort)
        try c.encode(sessionId, forKey: .sessionId)
        try c.encode(transcriptState, forKey: .transcriptState)
    }
}

public enum TranscriptState: String, Codable, Sendable {
    case ready, pending, unsupported

    /// Kinds whose transcripts the bridge can parse.
    public static let parsedKinds: Set<String> = ["claude", "pi", "codex"]

    public static func of(kind: String, hasTranscript: Bool) -> TranscriptState {
        hasTranscript ? .ready : parsedKinds.contains(kind) ? .pending : .unsupported
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
    case attachment(id: String, name: String, kind: AttachmentKind)

    private enum CodingKeys: String, CodingKey {
        case type, text, id, name, summary, input, toolCallId, isError, preview, kind
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
        case "attachment":
            self = .attachment(
                id: try c.decode(String.self, forKey: .id),
                name: try c.decode(String.self, forKey: .name),
                kind: try c.decode(AttachmentKind.self, forKey: .kind))
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
        case .attachment(let id, let name, let kind):
            try c.encode("attachment", forKey: .type)
            try c.encode(id, forKey: .id)
            try c.encode(name, forKey: .name)
            try c.encode(kind, forKey: .kind)
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
    /// In-progress assistant text preview; see api.md "Live reply". `text: nil` clears it.
    case replyLive(agentId: String, text: String?, seq: Int)
    /// One provider's usage snapshot changed; see api.md "Usage".
    case usageUpdated(UsageProvider)
}

extension ServerEvent: Encodable {
    private enum CodingKeys: String, CodingKey { case type, agent, agentId, message, text, seq, provider }

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
        case .replyLive(let id, let text, let seq):
            try c.encode("reply.live", forKey: .type)
            try c.encode(id, forKey: .agentId)
            try c.encode(text, forKey: .text)
            try c.encode(seq, forKey: .seq)
        case .usageUpdated(let p):
            try c.encode("usage.updated", forKey: .type)
            try c.encode(p, forKey: .provider)
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

    public static func parse(_ s: String) -> Date? {
        (try? Date(s, strategy: .iso8601.year().month().day().time(includingFractionalSeconds: true).timeZone(separator: .omitted)))
            ?? (try? Date(s, strategy: .iso8601))
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
