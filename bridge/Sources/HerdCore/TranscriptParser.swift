import Foundation

public enum TranscriptFormat: String, Sendable {
    case claude, pi
}

/// Turns agent JSONL into `[Message]`, one line at a time.
///
/// Pure: feed it lines, read `messages`. `consume` returns the message that was
/// created or grew, so a tailer can emit `message.upserted` for it.
public struct TranscriptParser: Sendable {
    public let format: TranscriptFormat
    public let scrubber: PathScrubber
    public private(set) var messages: [Message] = []

    static let droppedUserPrefixes = ["<command-", "<local-command", "<system-reminder"]

    public init(format: TranscriptFormat, cwd: String?) {
        self.format = format
        self.scrubber = PathScrubber(cwd: cwd)
    }

    public static func parse(_ data: Data, format: TranscriptFormat, cwd: String?) -> [Message] {
        var p = TranscriptParser(format: format, cwd: cwd)
        p.consume(data: data)
        return p.messages
    }

    /// Feeds every complete line in `data`. Returns the messages that changed, in order.
    @discardableResult
    public mutating func consume(data: Data) -> [Message] {
        var changed: [Message] = []
        for line in data.split(separator: UInt8(ascii: "\n")) {
            if let m = consume(line: Data(line)) {
                if let i = changed.firstIndex(where: { $0.id == m.id }) {
                    changed[i] = m
                } else {
                    changed.append(m)
                }
            }
        }
        return changed
    }

    /// Feeds one JSONL line. Returns the message it created or grew, if any.
    public mutating func consume(line: Data) -> Message? {
        guard let obj = (try? JSONSerialization.jsonObject(with: line)) as? [String: Any] else { return nil }
        switch format {
        case .claude: return consumeClaude(obj)
        case .pi: return consumePi(obj)
        }
    }

    // MARK: Claude

    private mutating func consumeClaude(_ o: [String: Any]) -> Message? {
        guard let type = o["type"] as? String, type == "user" || type == "assistant" else { return nil }
        if (o["isSidechain"] as? Bool) == true || (o["isMeta"] as? Bool) == true { return nil }
        guard let message = o["message"] as? [String: Any] else { return nil }
        let id = (o["uuid"] as? String) ?? UUID().uuidString
        let createdAt = Timestamps.normalize(o["timestamp"]) ?? Timestamps.format(.now)

        if type == "assistant" {
            guard let content = message["content"] as? [[String: Any]] else { return nil }
            let blocks = content.compactMap(claudeAssistantBlock)
            return appendAssistant(id: id, createdAt: createdAt, blocks: blocks)
        }

        if let text = message["content"] as? String {
            return appendUser(id: id, createdAt: createdAt, texts: [text])
        }
        guard let content = message["content"] as? [[String: Any]] else { return nil }
        var results: [Block] = []
        var texts: [String] = []
        for b in content {
            switch b["type"] as? String {
            case "tool_result":
                results.append(.toolResult(
                    toolCallId: (b["tool_use_id"] as? String) ?? "",
                    isError: (b["is_error"] as? Bool) ?? false,
                    preview: ToolSummary.preview(Self.resultText(b["content"]), scrubber: scrubber)))
            case "text":
                if let t = b["text"] as? String { texts.append(t) }
            default:
                break
            }
        }
        var changed: Message?
        if !results.isEmpty { changed = attachResults(results) }
        if let user = appendUser(id: id, createdAt: createdAt, texts: texts) { changed = user }
        return changed
    }

    private func claudeAssistantBlock(_ b: [String: Any]) -> Block? {
        switch b["type"] as? String {
        case "text":
            guard let t = b["text"] as? String, !t.isEmpty else { return nil }
            return .text(scrubber.scrub(t))
        case "thinking":
            guard let t = b["thinking"] as? String, !t.isEmpty else { return nil }
            return .thinking(scrubber.scrub(t))
        case "tool_use":
            let name = (b["name"] as? String) ?? "tool"
            let input = (b["input"] as? [String: Any]) ?? [:]
            return .toolCall(
                id: (b["id"] as? String) ?? "",
                name: name,
                summary: ToolSummary.summary(name: name, input: input, scrubber: scrubber),
                input: ToolSummary.inputString(input, scrubber: scrubber))
        default:
            return nil
        }
    }

    // MARK: pi

    private mutating func consumePi(_ o: [String: Any]) -> Message? {
        guard (o["type"] as? String) == "message", let message = o["message"] as? [String: Any] else { return nil }
        let id = (o["id"] as? String) ?? UUID().uuidString
        let createdAt = Timestamps.normalize(o["timestamp"]) ?? Timestamps.normalize(message["timestamp"]) ?? Timestamps.format(.now)
        let content = message["content"]
        switch message["role"] as? String {
        case "user":
            if let s = content as? String { return appendUser(id: id, createdAt: createdAt, texts: [s]) }
            let texts = (content as? [[String: Any]] ?? []).compactMap { b -> String? in
                (b["type"] as? String) == "text" ? b["text"] as? String : nil
            }
            return appendUser(id: id, createdAt: createdAt, texts: texts)
        case "assistant":
            let blocks = (content as? [[String: Any]] ?? []).compactMap { b -> Block? in
                switch b["type"] as? String {
                case "text":
                    guard let t = b["text"] as? String, !t.isEmpty else { return nil }
                    return .text(scrubber.scrub(t))
                case "thinking":
                    guard let t = b["thinking"] as? String, !t.isEmpty else { return nil }
                    return .thinking(scrubber.scrub(t))
                case "toolCall":
                    let name = (b["name"] as? String) ?? "tool"
                    let args = (b["arguments"] as? [String: Any]) ?? [:]
                    return .toolCall(
                        id: (b["id"] as? String) ?? "",
                        name: name,
                        summary: ToolSummary.summary(name: name, input: args, scrubber: scrubber),
                        input: ToolSummary.inputString(args, scrubber: scrubber))
                default:
                    return nil
                }
            }
            return appendAssistant(id: id, createdAt: createdAt, blocks: blocks)
        case "toolResult":
            return attachResults([.toolResult(
                toolCallId: (message["toolCallId"] as? String) ?? "",
                isError: (message["isError"] as? Bool) ?? false,
                preview: ToolSummary.preview(Self.resultText(content), scrubber: scrubber))])
        default:
            return nil
        }
    }

    // MARK: Building

    private mutating func appendAssistant(id: String, createdAt: String, blocks: [Block]) -> Message? {
        guard !blocks.isEmpty else { return nil }
        if var last = messages.last, last.role == .assistant {
            last.blocks += blocks
            messages[messages.count - 1] = last
            return last
        }
        let m = Message(id: id, role: .assistant, createdAt: createdAt, blocks: blocks)
        messages.append(m)
        return m
    }

    private mutating func appendUser(id: String, createdAt: String, texts: [String]) -> Message? {
        let kept = texts.filter { t in
            let trimmed = t.trimmingCharacters(in: .whitespacesAndNewlines)
            return !trimmed.isEmpty && !Self.droppedUserPrefixes.contains(where: trimmed.hasPrefix)
        }
        guard !kept.isEmpty else { return nil }
        let m = Message(id: id, role: .user, createdAt: createdAt, blocks: kept.map { .text(scrubber.scrub($0)) })
        messages.append(m)
        return m
    }

    /// Tool results belong to the assistant message that made the call; never a user bubble.
    private mutating func attachResults(_ results: [Block]) -> Message? {
        guard var last = messages.last, last.role == .assistant else { return nil }
        last.blocks += results
        messages[messages.count - 1] = last
        return last
    }

    static func resultText(_ content: Any?) -> String {
        if let s = content as? String { return s }
        if let arr = content as? [[String: Any]] {
            return arr.compactMap { b in
                switch b["type"] as? String {
                case "text": return b["text"] as? String
                case "image": return "[image]"
                default: return nil
                }
            }.joined(separator: "\n")
        }
        return ""
    }
}
