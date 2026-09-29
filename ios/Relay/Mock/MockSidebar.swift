#if DEBUG
import Foundation
import RelayKit

/// Extra made-up projects and chats so the mock sidebar shows every state of design B:
/// 7 running (so Now has "Show all"), a project with completed chats, a project whose chats are all
/// running (the one-row "N running" card), and a blocked chat (from the fixtures).
enum MockSidebar {
    static let workspaces = [
        Workspace(id: "w3", name: "analytics", agentCount: 0),
        Workspace(id: "w4", name: "mobile", agentCount: 0),
    ]

    /// Done chats the mock marks as already read, so they fold into "N completed".
    static let completedIds = ["w2:p7", "w2:p8"]

    static func agents(now: Date = Date()) -> [Agent] {
        func agent(_ id: String, _ kind: String, _ title: String, _ ws: Workspace, _ status: AgentStatus, minutesAgo: Double) -> Agent {
            Agent(
                id: id, name: nil, kind: kind, title: title,
                workspaceId: ws.id, workspaceName: ws.name, cwdName: ws.name,
                status: status, hasTranscript: true, updatedAt: now.addingTimeInterval(-minutesAgo * 60)
            )
        }
        let website = Workspace(id: "w2", name: "website", agentCount: 0)
        let (analytics, mobile) = (workspaces[0], workspaces[1])
        return [
            agent("w2:p6", "claude", "Speed up image loading", website, .working, minutesAgo: 6),
            agent("w2:p7", "codex", "Update footer links", website, .done, minutesAgo: 180),
            agent("w2:p8", "claude", "Add a sitemap route", website, .done, minutesAgo: 20 * 60),
            agent("w3:p1", "claude", "Weekly report export", analytics, .working, minutesAgo: 2),
            agent("w3:p2", "codex", "Retry failed crawls", analytics, .working, minutesAgo: 14),
            agent("w4:p1", "claude", "Dark mode for settings", mobile, .working, minutesAgo: 3),
            agent("w4:p2", "codex", "Refresh push tokens", mobile, .working, minutesAgo: 11),
            agent("w4:p3", "pi", "Offline queue for drafts", mobile, .working, minutesAgo: 24),
            agent("w4:p4", "claude", "Release notes for 2.4", mobile, .idle, minutesAgo: 28),
        ]
    }

    /// Marks `completedIds` as seen in the (test-isolated under UI tests) defaults the store reads at start.
    /// A second later than `updatedAt`: the 1970/2001 epoch round trip can lose the last bit and read as unseen.
    static func markCompletedSeen(_ agents: [Agent], machineId: String) {
        var seen = AppDefaults.standard.dictionary(forKey: "seenAgents") as? [String: Double] ?? [:]
        for a in agents where completedIds.contains(a.id) {
            seen[MachineKey.make(machineId, a.id)] = a.updatedAt.timeIntervalSince1970 + 1
        }
        AppDefaults.standard.set(seen, forKey: "seenAgents")
    }
}
#endif
