import Foundation
import RelayKit

/// Plain value state plus the WebSocket reducer. No I/O here so it's easy to test.
struct RelayState: Equatable, Sendable {
    var agents: [Agent] = []
    var workspaces: [Workspace] = []
    /// Only agents whose chat has been fetched have an entry. Upserts for other agents are ignored;
    /// the chat is fetched in full when it's opened.
    var messages: [String: [Message]] = [:]
    var hasMore: [String: Bool] = [:]

    func agent(_ id: String?) -> Agent? {
        guard let id else { return nil }
        return agents.first { $0.id == id }
    }

    mutating func apply(_ event: ServerEvent) {
        switch event {
        case .hello, .unknown, .replyLive:
            break
        case .agentUpdated(let agent), .agentCreated(let agent):
            upsert(agent)
        case .agentClosed(let id):
            agents.removeAll { $0.id == id }
            messages[id] = nil
            hasMore[id] = nil
        case .messageUpserted(let agentId, let message):
            upsert(message, agentId: agentId)
        }
    }

    mutating func upsert(_ agent: Agent) {
        if let i = agents.firstIndex(where: { $0.id == agent.id }) {
            // Drop the cached chat so it's refetched when what it was read from changes:
            // - a new session (/clear) is a different transcript;
            // - a session appearing, or the transcript state changing, means the old fetch may have been a
            //   screen read taken while the bridge was still detecting a just-created agent.
            let old = agents[i]
            let sessionChanged = old.sessionId != agent.sessionId && agent.sessionId != nil
            if sessionChanged || old.transcript != agent.transcript {
                messages[agent.id] = nil
                hasMore[agent.id] = nil
            }
            agents[i] = agent
        } else {
            agents.append(agent)
        }
    }

    /// Replaces a message with the same id (the last assistant message grows while working),
    /// otherwise inserts it in `createdAt` order.
    ///
    /// Mutates through the dictionary's own subscript (`messages[agentId]!...`) rather than
    /// `var list = messages[agentId]!; ...; messages[agentId] = list`: the latter holds two live
    /// references to the array (the dictionary's and `list`'s) across the mutation, forcing a full
    /// copy-on-write of the whole transcript on every delta. On a long-running turn in a large
    /// transcript that ran on every streamed token; this keeps growth O(1) amortized instead of O(n).
    mutating func upsert(_ message: Message, agentId: String) {
        guard messages[agentId] != nil else { return }
        if let i = messages[agentId]!.firstIndex(where: { $0.id == message.id }) {
            messages[agentId]![i] = message
        } else {
            let i = messages[agentId]!.lastIndex { $0.createdAt <= message.createdAt }.map { $0 + 1 } ?? 0
            messages[agentId]!.insert(message, at: i)
        }
    }

    mutating func setPage(_ page: MessagePage, agentId: String) {
        messages[agentId] = page.messages
        hasMore[agentId] = page.hasMore
    }

    mutating func prependPage(_ page: MessagePage, agentId: String) {
        let existing = messages[agentId] ?? []
        let known = Set(existing.map(\.id))
        messages[agentId] = page.messages.filter { !known.contains($0.id) } + existing
        hasMore[agentId] = page.hasMore
    }

    // MARK: - Sidebar

    struct Section: Identifiable, Equatable {
        var id: String
        var name: String
        var agents: [Agent]
    }

    /// Agents grouped by workspace (in `/workspaces` order), blocked first, then most recent.
    func sections(matching query: String = "") -> [Section] {
        let q = query.trimmingCharacters(in: .whitespaces).lowercased()
        let visible = q.isEmpty ? agents : agents.filter {
            $0.displayTitle.lowercased().contains(q)
                || ($0.name ?? "").lowercased().contains(q)
                || $0.workspaceName.lowercased().contains(q)
                || $0.kind.lowercased().contains(q)
        }
        let grouped = Dictionary(grouping: visible, by: \.workspaceId)
        var order = workspaces.map(\.id)
        for a in visible where !order.contains(a.workspaceId) { order.append(a.workspaceId) }
        return order.compactMap { wid in
            guard let list = grouped[wid], let first = list.first else { return nil }
            let sorted = list.sorted { a, b in
                if (a.status == .blocked) != (b.status == .blocked) { return a.status == .blocked }
                return a.updatedAt > b.updatedAt
            }
            let name = workspaces.first { $0.id == wid }?.name ?? first.workspaceName
            return Section(id: wid, name: name, agents: sorted)
        }
    }
}
