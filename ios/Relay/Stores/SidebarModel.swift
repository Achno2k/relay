import Foundation
import RelayKit

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
    var state: RelayState
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

// MARK: - Grouped home (design B)

extension SidebarModel {
    /// One project card. Running chats live in Now, so they never appear here; done chats the person
    /// has seen fold into the "N completed" row.
    struct ProjectSection: Identifiable, Equatable {
        var id: String
        var name: String
        /// Needs input, ready for review, idle: needs input first, then ready for review, then newest.
        var rows: [Agent]
        /// Done and seen, newest first.
        var completed: [Agent]
        /// This project's chats in Now, newest first.
        var running: [Agent]

        /// Every chat is running, so the whole project is already in Now: one "name · N running" row.
        var isAllInNow: Bool { rows.isEmpty && completed.isEmpty && !running.isEmpty }
        var isEmpty: Bool { rows.isEmpty && completed.isEmpty && running.isEmpty }
    }

    struct Grouped: Equatable {
        /// Every running chat across projects, newest first. The card shows `nowCap` of them until "Show all".
        var now: [Agent]
        /// Every herdr workspace, in `/workspaces` order, including empty ones (for "new chat in project").
        var sections: [ProjectSection]
    }

    static let nowCap = 4

    func isCompleted(_ agent: Agent) -> Bool { agent.status == .done && !isUnseen(agent) }

    /// The home screen (filter All). Archived chats are left out, as everywhere but `.archived`.
    func grouped() -> Grouped {
        let visible = state.agents.filter { matches($0, .all) }
        let newestFirst: (Agent, Agent) -> Bool = { $0.updatedAt > $1.updatedAt }
        let now = visible.filter { $0.status == .working }.sorted(by: newestFirst)
        let byProject = Dictionary(grouping: visible, by: \.workspaceId)
        var order = state.workspaces.map { ($0.id, $0.name) }
        for a in visible where !order.contains(where: { $0.0 == a.workspaceId }) {
            order.append((a.workspaceId, a.workspaceName))
        }
        let sections = order.map { id, name in
            let agents = byProject[id] ?? []
            let rows = agents
                .filter { $0.status != .working && !isCompleted($0) }
                .sorted { a, b in
                    let (ra, rb) = (rank(a), rank(b))
                    return ra != rb ? ra < rb : a.updatedAt > b.updatedAt
                }
            return ProjectSection(
                id: id,
                name: name,
                rows: rows,
                completed: agents.filter(isCompleted).sorted(by: newestFirst),
                running: agents.filter { $0.status == .working }.sorted(by: newestFirst)
            )
        }
        return Grouped(now: now, sections: sections)
    }

    /// Order inside a project card: what needs you, then what's new to read, then the rest.
    private func rank(_ agent: Agent) -> Int {
        if agent.status == .blocked { return 0 }
        if agent.status == .done && isUnseen(agent) { return 1 }
        return 2
    }
}
