import Foundation
import HerdKit

/// The sidebar filter menu (Codex style).
enum SessionFilter: String, CaseIterable, Identifiable, Sendable {
    case all, needsInput, readyForReview, working, completed, archived

    var id: String { rawValue }

    var title: String {
        switch self {
        case .all: "All"
        case .needsInput: "Needs input"
        case .readyForReview: "Ready for review"
        case .working: "Working"
        case .completed: "Completed"
        case .archived: "Archived"
        }
    }

    var symbol: String {
        switch self {
        case .all: "checklist"
        case .needsInput: "hand.raised"
        case .readyForReview: "eye"
        case .working: "circle.dotted"
        case .completed: "checkmark.circle"
        case .archived: "archivebox"
        }
    }
}

/// Pure sidebar logic: which chats show where. Archived chats appear only under `.archived`.
struct SidebarModel {
    struct Project: Identifiable, Equatable {
        var id: String
        var name: String
        var agents: [Agent]
        var needsInput: Int
    }

    var state: HerdState
    var archived: Set<String>
    var isUnseen: (Agent) -> Bool

    func matches(_ agent: Agent, _ filter: SessionFilter) -> Bool {
        let isArchived = archived.contains(agent.id)
        if filter == .archived { return isArchived }
        if isArchived { return false }
        switch filter {
        case .all: return true
        case .needsInput: return agent.status == .blocked
        case .readyForReview: return agent.status == .done && isUnseen(agent)
        case .working: return agent.status == .working
        case .completed: return (agent.status == .done && !isUnseen(agent)) || agent.status == .idle
        case .archived: return isArchived
        }
    }

    /// One row per herdr workspace (even empty ones, so "new chat in folder" works), in `/workspaces` order.
    /// Chats inside: blocked first, then newest.
    func projects() -> [Project] {
        let visible = state.agents.filter { matches($0, .all) }
        let grouped = Dictionary(grouping: visible, by: \.workspaceId)
        var order = state.workspaces.map { ($0.id, $0.name) }
        for a in visible where !order.contains(where: { $0.0 == a.workspaceId }) {
            order.append((a.workspaceId, a.workspaceName))
        }
        return order.map { id, name in
            let agents = (grouped[id] ?? []).sorted { a, b in
                if (a.status == .blocked) != (b.status == .blocked) { return a.status == .blocked }
                return a.updatedAt > b.updatedAt
            }
            return Project(id: id, name: name, agents: agents, needsInput: agents.filter { $0.status == .blocked }.count)
        }
    }

    /// Flat "Sessions" list for a filter or a search, newest first.
    func sessions(_ filter: SessionFilter, query: String = "") -> [Agent] {
        let q = query.trimmingCharacters(in: .whitespaces).lowercased()
        return state.agents
            .filter { matches($0, filter) }
            .filter {
                q.isEmpty
                    || $0.displayTitle.lowercased().contains(q)
                    || ($0.name ?? "").lowercased().contains(q)
                    || $0.workspaceName.lowercased().contains(q)
            }
            .sorted { $0.updatedAt > $1.updatedAt }
    }

    var needsInputCount: Int { state.agents.filter { matches($0, .needsInput) }.count }
}
