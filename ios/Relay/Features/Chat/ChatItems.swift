import Foundation
import RelayKit

struct ToolStep: Identifiable, Hashable {
    var id: String
    var name: String
    var summary: String
    var isError: Bool
    var preview: String?
    var finished: Bool
    /// The file it read or changed (cwd-relative), for the file viewer; never set on live placeholders.
    var path: String? = nil
    var edit: ToolEdit? = nil
    /// ExitPlanMode's plan, shown inline under the step.
    var plan: String? = nil
}

/// What the chat list renders. Consecutive tool calls/results collapse into one `.tools` row.
enum ChatItem: Identifiable, Hashable {
    case user(id: String, text: String, pending: Bool, attachments: [AttachmentRef] = [])
    case text(id: String, markdown: String)
    case thinking(id: String, text: String)
    case tools(id: String, steps: [ToolStep], messageIds: [String])
    /// Claude's "[Request interrupted by user]" line after a stop.
    case stopped(id: String)
    /// The in-progress reply from `reply.live`, until the transcript's text lands.
    case live(id: String, markdown: String)

    var id: String {
        switch self {
        case .user(let id, _, _, _), .text(let id, _), .thinking(let id, _), .tools(let id, _, _), .stopped(let id), .live(let id, _): id
        }
    }

    var isAssistantText: Bool {
        if case .text = self { true } else { false }
    }

    var isPending: Bool {
        if case .user(_, _, true, _) = self { true } else { false }
    }

    static let liveTextId = "live-text"

    /// Adds what's live for a working agent to the built transcript: the reply text as it's typed and
    /// the tool running on screen, both before any pending prompts (they belong to the turn in progress,
    /// and the transcript rows that replace them land there too).
    ///
    /// The running tool goes exactly where its transcript call will land: into the last tool group if
    /// the chat ends with one, otherwise as a new group. A landed call keeps the placeholder's id
    /// (`aliases`), so the row swaps in place.
    static func withLive(_ items: [ChatItem], live: LiveReply?, aliases: [String: String], working: Bool) -> [ChatItem] {
        var body = aliases.isEmpty ? items : items.map { $0.aliased(aliases) }
        let split = body.lastIndex { !$0.isPending }.map { $0 + 1 } ?? 0
        let pending = body[split...]
        body.removeSubrange(split...)
        guard working, let live else { return body + pending }

        let showText = live.text.map { !$0.isEmpty } == true && !(body.last?.isAssistantText ?? false)
        if showText, let text = live.text {
            body.append(.live(id: liveTextId, markdown: text))
        }
        if let run = live.tool, run.matchedCallId == nil {
            let step = ToolStep(
                id: run.placeholderId, name: run.tool.name, summary: run.tool.summary,
                isError: false, preview: nil, finished: false
            )
            if !showText, case .tools(let id, let steps, let messageIds) = body.last {
                body[body.count - 1] = .tools(id: id, steps: steps + [step], messageIds: messageIds)
            } else {
                body.append(.tools(id: run.placeholderId, steps: [step], messageIds: []))
            }
        }
        return body + pending
    }

    private func aliased(_ aliases: [String: String]) -> ChatItem {
        guard case .tools(let id, var steps, let messageIds) = self,
              steps.contains(where: { aliases[$0.id] != nil }) else { return self }
        let groupId = steps.first.flatMap { aliases[$0.id] } ?? id
        for i in steps.indices {
            if let alias = aliases[steps[i].id] { steps[i].id = alias }
        }
        return .tools(id: groupId, steps: steps, messageIds: messageIds)
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
                    steps.append(ToolStep(
                        id: call.id, name: call.name, summary: call.summary, isError: false, preview: nil, finished: false,
                        path: call.path, edit: call.edit, plan: call.plan
                    ))
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
        case "Bash", "shell", "Shell", "exec", "BashOutput": "terminal"
        case "ViewImage": "photo"
        case "Read", "read", "LS": "doc.text"
        case "WebFetch", "WebSearch": "globe"
        case "Task", "Agent": "person.2"
        case "TodoWrite", "update_plan": "checklist"
        default: "wrench.adjustable"
        }
    }
}
