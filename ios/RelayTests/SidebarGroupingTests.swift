import Foundation
import RelayKit
import Testing
@testable import Relay

@Suite("Sidebar grouping (design B)")
struct SidebarGroupingTests {
    private let now = Date()

    private func agent(_ id: String, _ ws: String, _ status: AgentStatus, minutesAgo: Double) -> Agent {
        Agent(
            id: id, name: nil, kind: "claude", title: "Chat \(id)",
            workspaceId: ws, workspaceName: "project-\(ws)", cwdName: "project-\(ws)",
            status: status, hasTranscript: true, updatedAt: now.addingTimeInterval(-minutesAgo * 60)
        )
    }

    private func model(_ agents: [Agent], workspaces: [String], archived: Set<String> = [], unseen: Set<String> = []) -> SidebarModel {
        var state = RelayState()
        state.agents = agents
        state.workspaces = workspaces.map { Workspace(id: $0, name: "project-\($0)", agentCount: 0) }
        return SidebarModel(state: state, archived: archived, isUnseen: { unseen.contains($0.id) })
    }

    @Test func nowHoldsEveryRunningChatNewestFirst() {
        let agents = (1...7).map { agent("a\($0)", $0 % 2 == 0 ? "w1" : "w2", .working, minutesAgo: Double($0)) }
            + [agent("idle", "w1", .idle, minutesAgo: 0)]
        let g = model(agents, workspaces: ["w1", "w2"]).grouped()
        #expect(g.now.map(\.id) == ["a1", "a2", "a3", "a4", "a5", "a6", "a7"])
        #expect(g.now.count > SidebarModel.nowCap, "more than the cap, so the card offers Show all")
        #expect(g.sections.allSatisfy { !$0.rows.contains { $0.status == .working } }, "running chats are only in Now")
    }

    @Test func completedFoldsOnlySeenDoneChats() {
        let agents = [
            agent("idle", "w1", .idle, minutesAgo: 1),
            agent("review", "w1", .done, minutesAgo: 30),
            agent("seen-old", "w1", .done, minutesAgo: 90),
            agent("seen-new", "w1", .done, minutesAgo: 5),
            agent("blocked", "w1", .blocked, minutesAgo: 60),
        ]
        let s = model(agents, workspaces: ["w1"], unseen: ["review"]).grouped().sections[0]
        #expect(s.rows.map(\.id) == ["blocked", "review", "idle"], "needs input, then ready for review, then newest")
        #expect(s.completed.map(\.id) == ["seen-new", "seen-old"])
        #expect(!s.isAllInNow)
    }

    @Test func projectWithOnlyRunningChatsCollapsesIntoNow() {
        let agents = [
            agent("r1", "w1", .working, minutesAgo: 1),
            agent("r2", "w1", .working, minutesAgo: 2),
            agent("x", "w2", .working, minutesAgo: 3),
            agent("y", "w2", .idle, minutesAgo: 3),
        ]
        let sections = model(agents, workspaces: ["w1", "w2", "w3"]).grouped().sections
        #expect(sections.map(\.id) == ["w1", "w2", "w3"], "workspace order, empty ones kept for new chat")
        #expect(sections[0].isAllInNow && sections[0].running.map(\.id) == ["r1", "r2"])
        #expect(!sections[1].isAllInNow, "an idle chat keeps the full card")
        #expect(sections[2].isEmpty && !sections[2].isAllInNow)
    }

    @Test func archivedChatsStayOutOfNowAndCards() {
        let agents = [
            agent("run", "w1", .working, minutesAgo: 1),
            agent("done", "w1", .done, minutesAgo: 2),
            agent("idle", "w1", .idle, minutesAgo: 3),
        ]
        let g = model(agents, workspaces: ["w1"], archived: ["run", "done"]).grouped()
        #expect(g.now.isEmpty)
        #expect(g.sections[0].rows.map(\.id) == ["idle"])
        #expect(g.sections[0].completed.isEmpty)
    }

    @Test func agentsInUnknownWorkspacesStillGetASection() {
        let g = model([agent("a", "w9", .idle, minutesAgo: 1)], workspaces: ["w1"]).grouped()
        #expect(g.sections.map(\.id) == ["w1", "w9"])
        #expect(g.sections[1].name == "project-w9")
    }

    @Test func expansionSetToggles() {
        var raw = ""
        raw = ExpansionSet(raw: raw).toggled("w2")
        raw = ExpansionSet(raw: raw).toggled("w1")
        #expect(raw == "w1,w2")
        #expect(ExpansionSet(raw: raw).contains("w1") && !ExpansionSet(raw: raw).contains("w"))
        #expect(ExpansionSet(raw: raw).toggled("w1") == "w2")
    }
}
