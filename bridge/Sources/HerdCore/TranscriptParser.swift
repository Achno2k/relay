import Foundation

public enum TranscriptFormat: String, Sendable {
    case claude, pi, codex
}

/// Turns agent JSONL into `[Message]`, one line at a time.
///
/// Pure: feed it lines, read `messages`. `consume` returns the message that was
/// created or grew, so a tailer can emit `message.upserted` for it.
public struct TranscriptParser: Sendable {
    public let format: TranscriptFormat
    public let scrubber: PathScrubber
    /// Recognises the bridge's own `Attached files:` marker in user messages.
    public let uploads: UploadStore
    public private(set) var messages: [Message] = []
    /// Timestamp of the line being parsed, to match sent records.
    private var currentDate: Date?

    static let droppedUserPrefixes = ["<command-", "<local-command", "<system-reminder"]

    public init(format: TranscriptFormat, cwd: String?, uploads: UploadStore = UploadStore()) {
        self.format = format
        self.scrubber = PathScrubber(cwd: cwd)
        self.uploads = uploads
    }

    public static func parse(_ data: Data, format: TranscriptFormat, cwd: String?, uploads: UploadStore = UploadStore()) -> [Message] {
        var p = TranscriptParser(format: format, cwd: cwd, uploads: uploads)
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
        case .codex: return consumeCodex(obj)
        }
    }

    // MARK: Claude

    private mutating func consumeClaude(_ o: [String: Any]) -> Message? {
        guard let type = o["type"] as? String, type == "user" || type == "assistant" else { return nil }
        if (o["isSidechain"] as? Bool) == true || (o["isMeta"] as? Bool) == true { return nil }
        // `/compact` injects the whole summary as a user message.
        if (o["isCompactSummary"] as? Bool) == true { return nil }
        guard let message = o["message"] as? [String: Any] else { return nil }
        let id = (o["uuid"] as? String) ?? UUID().uuidString
        let createdAt = Timestamps.normalize(o["timestamp"]) ?? Timestamps.format(.now)
        currentDate = (o["timestamp"] as? String).flatMap(Timestamps.parse)

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

    // MARK: codex

    /// codex rollouts: only `event_msg` / `item_completed` items (what codex's own UI shows).
    /// The raw `response_item`s repeat them with injected context and JS tool wrappers.
    private mutating func consumeCodex(_ o: [String: Any]) -> Message? {
        guard o["type"] as? String == "event_msg",
              let p = o["payload"] as? [String: Any], p["type"] as? String == "item_completed",
              let item = p["item"] as? [String: Any], let type = item["type"] as? String
        else { return nil }
        let id = (item["id"] as? String) ?? UUID().uuidString
        let createdAt = Timestamps.normalize(o["timestamp"]) ?? Timestamps.format(.now)
        currentDate = (o["timestamp"] as? String).flatMap(Timestamps.parse)

        func texts(_ key: String = "content") -> [String] {
            (item[key] as? [[String: Any]] ?? []).compactMap { $0["text"] as? String }
        }
        switch type {
        case "UserMessage":
            return appendUser(id: id, createdAt: createdAt, texts: [texts().joined(separator: "\n")])
        case "AgentMessage":
            let t = texts().joined(separator: "\n")
            return t.isEmpty ? nil : appendAssistant(id: id, createdAt: createdAt, blocks: [.text(scrubber.scrub(t))])
        case "Reasoning":
            let t = (item["summary_text"] as? [String] ?? []).joined(separator: "\n\n")
            return t.isEmpty ? nil : appendAssistant(id: id, createdAt: createdAt, blocks: [.thinking(scrubber.scrub(t))])
        default:
            guard let blocks = codexTool(type, item, id: id), !blocks.isEmpty else { return nil }
            return appendAssistant(id: id, createdAt: createdAt, blocks: blocks)
        }
    }

    /// A finished codex tool item as a toolCall plus its toolResult.
    private func codexTool(_ type: String, _ item: [String: Any], id: String) -> [Block]? {
        func strip(_ s: String) -> String { s.hasPrefix("file://") ? String(s.dropFirst(7)) : s }
        let failed = (item["status"] as? String).map { $0 == "failed" || $0 == "declined" } ?? false
        func pair(_ name: String, _ summary: String, _ input: Any, _ output: String, error: Bool) -> [Block] {
            [.toolCall(id: id, name: name, summary: ToolSummary.truncate(scrubber.scrub(summary), 120),
                       input: ToolSummary.inputString(input, scrubber: scrubber)),
             .toolResult(toolCallId: id, isError: error, preview: ToolSummary.preview(output, scrubber: scrubber))]
        }
        switch type {
        case "CommandExecution":
            let argv = item["command"] as? [String] ?? []
            // `["/bin/zsh", "-lc", "<script>"]` → the script.
            let cmd = argv.count >= 3 && ["-lc", "-c"].contains(argv[1]) ? argv[2] : argv.joined(separator: " ")
            var input: [String: Any] = ["command": cmd]
            if let cwd = item["cwd"] as? String { input["cwd"] = strip(cwd) }
            let exit = item["exit_code"] as? Int
            let output = (item["aggregated_output"] as? String) ?? [item["stdout"], item["stderr"]].compactMap { $0 as? String }.joined(separator: "\n")
            return pair("Shell", "Ran \(ToolSummary.firstLine(cmd))", input, output, error: failed || (exit ?? 0) != 0)
        case "FileChange":
            let changes = item["changes"] as? [String: Any] ?? [:]
            let paths = changes.keys.sorted()
            let summary = paths.count == 1 ? "Edited \(scrubber.scrub(paths[0]))" : "Edited \(paths.count) files"
            let diff = paths.compactMap { p -> String? in
                guard let c = changes[p] as? [String: Any] else { return nil }
                return "\(p)\n" + ((c["unified_diff"] as? String) ?? (c["content"] as? String) ?? (c["type"] as? String ?? ""))
            }.joined(separator: "\n")
            return pair("Edit", summary, ["files": paths], diff, error: failed)
        case "McpToolCall":
            let name = [item["server"] as? String, item["tool"] as? String].compactMap { $0 }.joined(separator: ".")
            let result = item["result"] as? [String: Any]
            let text = (result?["content"] as? [[String: Any]] ?? []).compactMap { $0["text"] as? String }.joined(separator: "\n")
            return pair(name.isEmpty ? "MCP" : name, "Called \(name)", item["arguments"] ?? [:], text,
                        error: failed || (result?["isError"] as? Bool ?? false))
        case "Extension":
            let query = (item["query"] as? String) ?? ((item["action"] as? [String: Any])?["queries"] as? [String])?.first ?? ""
            let results = (item["results"] as? [[String: Any]] ?? []).compactMap { $0["domain"] as? String }
            let kind = item["kind"] as? String ?? "extension"
            return pair(kind == "web.search" ? "WebSearch" : kind, kind == "web.search" ? "Searched the web for \(query)" : kind,
                        ["query": query], results.joined(separator: "\n"), error: failed)
        case "ImageView":
            let path = strip(item["path"] as? String ?? "")
            return pair("ViewImage", "Viewed \(path)", ["path": path], "", error: false)
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
        var blocks: [Block] = []
        for t in kept {
            // What the bridge sent, even though Claude Code rewrote the text.
            if UploadStore.looksAttached(t), let sent = uploads.lookupSent(t, at: currentDate) {
                blocks += sent.attachments.map { .attachment(id: $0.id, name: $0.name, kind: $0.kind) }
                if !sent.text.isEmpty { blocks.append(.text(scrubber.scrub(sent.text))) }
                continue
            }
            let t = PastedContent.strip(t)
            // Before scrubbing: the marker's absolute paths identify our uploads.
            if let parsed = uploads.parseMarker(t) {
                blocks += parsed.attachments.map { .attachment(id: $0.id, name: $0.name, kind: $0.kind) }
                if !parsed.text.isEmpty { blocks.append(.text(scrubber.scrub(parsed.text))) }
            } else {
                blocks.append(.text(scrubber.scrub(t)))
            }
        }
        let m = Message(id: id, role: .user, createdAt: createdAt, blocks: blocks)
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
