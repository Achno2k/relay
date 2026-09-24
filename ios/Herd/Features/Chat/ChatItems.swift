import Foundation
import HerdKit

struct ToolStep: Identifiable, Hashable {
    var id: String
    var name: String
    var summary: String
    var isError: Bool
    var preview: String?
    var finished: Bool
}

/// What the chat list renders. Consecutive tool calls/results collapse into one `.tools` row.
enum ChatItem: Identifiable, Hashable {
    case user(id: String, text: String, pending: Bool, attachments: [AttachmentRef] = [])
    case text(id: String, markdown: String)
    case thinking(id: String, text: String)
    case tools(id: String, steps: [ToolStep], messageIds: [String])
    /// Claude's "[Request interrupted by user]" line after a stop.
    case stopped(id: String)

    var id: String {
        switch self {
        case .user(let id, _, _, _), .text(let id, _), .thinking(let id, _), .tools(let id, _, _), .stopped(let id): id
        }
    }

    var isAssistantText: Bool {
        if case .text = self { true } else { false }
    }

    static func build(from messages: [Message]) -> [ChatItem] {
        var items: [ChatItem] = []
        var steps: [ToolStep] = []
        var stepMessages: [String] = []
        var groupId: String?

        func flush() {
            if let groupId, !steps.isEmpty {
                items.append(.tools(id: groupId, steps: steps, messageIds: stepMessages))
            }
            steps = []
            stepMessages = []
            groupId = nil
        }

        for message in messages {
            if message.role == .user {
                flush()
                if message.isInterruptionMarker {
                    items.append(.stopped(id: message.id))
                    continue
                }
                let text = message.plainText
                let attachments = message.attachments
                if !text.isEmpty || !attachments.isEmpty {
                    items.append(.user(id: message.id, text: text, pending: false, attachments: attachments))
                }
                continue
            }
            for (i, block) in message.blocks.enumerated() {
                let blockId = "\(message.id)#\(i)"
                switch block {
                case .text(let t):
                    guard !t.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { continue }
                    flush()
                    items.append(.text(id: blockId, markdown: t))
                case .thinking(let t):
                    guard !t.isEmpty else { continue }
                    flush()
                    items.append(.thinking(id: blockId, text: t))
                case .toolCall(let call):
                    if groupId == nil { groupId = blockId }
                    if !stepMessages.contains(message.id) { stepMessages.append(message.id) }
                    steps.append(ToolStep(id: call.id, name: call.name, summary: call.summary, isError: false, preview: nil, finished: false))
                case .toolResult(let result):
                    if let j = steps.lastIndex(where: { $0.id == result.toolCallId }) {
                        steps[j].isError = result.isError
                        steps[j].preview = result.preview
                        steps[j].finished = true
                    }
                case .attachment, .unknown:
                    continue
                }
            }
        }
        flush()
        return items
    }
}

extension Message {
    /// Claude Code writes these as user lines when a turn is stopped. The bridge passes them through
    /// as-is; the app shows a "Stopped" marker instead of a bubble.
    var isInterruptionMarker: Bool {
        guard role == .user else { return false }
        let text = plainText.trimmingCharacters(in: .whitespacesAndNewlines)
        return text == "[Request interrupted by user]" || text == "[Request interrupted by user for tool use]"
    }
}

enum ToolIcon {
    static func symbol(for name: String) -> String {
        switch name {
        case "Edit", "Write", "MultiEdit", "NotebookEdit", "apply_patch": "pencil"
        case "Grep", "Glob", "search": "magnifyingglass"
        case "Bash", "shell", "exec", "BashOutput": "terminal"
        case "Read", "read", "LS": "doc.text"
        case "WebFetch", "WebSearch": "globe"
        case "Task", "Agent": "person.2"
        case "TodoWrite", "update_plan": "checklist"
        default: "wrench.adjustable"
        }
    }
}
