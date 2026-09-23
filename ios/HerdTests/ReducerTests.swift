import Foundation
import HerdKit
import Testing
@testable import Herd

@Suite("HerdState reducer")
struct ReducerTests {
    private func loaded() throws -> HerdState {
        var state = HerdState()
        state.agents = try FixtureFiles.decode([Agent].self, "agents.json")
        state.workspaces = try FixtureFiles.decode([Workspace].self, "workspaces.json")
        state.setPage(try FixtureFiles.decode(MessagePage.self, "messages.json"), agentId: "w1:p1")
        return state
    }

    @Test func messageUpsertedReplacesGrowingMessage() throws {
        var state = try loaded()
        let events = try FixtureFiles.events()
        state.apply(events[2])
        let messages = try #require(state.messages["w1:p1"])
        #expect(messages.count == 4)
        #expect(messages.last?.id == "m4")
        #expect(messages.last?.blocks.last == .text("Now re-reading on mtime change."))
    }

    @Test func messageUpsertedAppendsNewMessageInOrder() throws {
        var state = try loaded()
        let later = Message(id: "m5", role: .user, createdAt: .now, blocks: [.text("thanks")])
        let earlier = Message(id: "m0", role: .user, createdAt: .distantPast, blocks: [.text("first")])
        state.apply(.messageUpserted(agentId: "w1:p1", message: later))
        state.apply(.messageUpserted(agentId: "w1:p1", message: earlier))
        #expect(state.messages["w1:p1"]?.map(\.id) == ["m0", "m1", "m2", "m3", "m4", "m5"])
    }

    @Test func messageUpsertedIgnoresChatsNotLoaded() throws {
        var state = try loaded()
        let m = Message(id: "x", role: .user, createdAt: .now, blocks: [.text("hi")])
        state.apply(.messageUpserted(agentId: "w2:p1", message: m))
        #expect(state.messages["w2:p1"] == nil)
    }

    @Test func agentUpdatedReplacesAgent() throws {
        var state = try loaded()
        let events = try FixtureFiles.events()
        state.apply(events[1])
        #expect(state.agent("w1:p2")?.status == .working)
        #expect(state.agents.count == 4)
    }

    @Test func agentCreatedAndUpdatedInsertUnknownAgents() throws {
        var state = try loaded()
        var agent = try #require(state.agent("w1:p1"))
        agent = Agent(
            id: "w1:p9", name: nil, kind: "pi", title: "pi", workspaceId: "w1", workspaceName: "shop-api",
            cwdName: "shop-api", status: .idle, hasTranscript: true, updatedAt: agent.updatedAt
        )
        state.apply(.agentCreated(agent))
        #expect(state.agents.count == 5)
        state.apply(.agentUpdated(agent))
        #expect(state.agents.count == 5)
    }

    @Test func agentClosedRemovesAgentAndChat() throws {
        var state = try loaded()
        state.apply(.agentClosed(agentId: "w1:p1"))
        #expect(state.agent("w1:p1") == nil)
        #expect(state.messages["w1:p1"] == nil)
        let events = try FixtureFiles.events()
        state.apply(events[3])
        #expect(state.agent("w2:p3") == nil)
        #expect(state.agents.count == 2)
    }

    @Test func sectionsFloatBlockedAgentsToTop() throws {
        let state = try loaded()
        let sections = state.sections()
        #expect(sections.map(\.name) == ["shop-api", "website"])
        #expect(sections[0].agents.map(\.id) == ["w1:p2", "w1:p1"])
        #expect(sections[1].agents.map(\.id) == ["w2:p1", "w2:p3"])
        #expect(state.sections(matching: "landing").flatMap(\.agents).map(\.id) == ["w2:p1"])
    }

    @Test func prependPageSkipsDuplicates() throws {
        var state = try loaded()
        let page = try FixtureFiles.decode(MessagePage.self, "messages.json")
        let older = Message(id: "m-1", role: .user, createdAt: .distantPast, blocks: [.text("old")])
        state.prependPage(MessagePage(messages: [older, page.messages[0]], hasMore: false), agentId: "w1:p1")
        #expect(state.messages["w1:p1"]?.map(\.id) == ["m-1", "m1", "m2", "m3", "m4"])
    }
}

@Suite("Chat items")
struct ChatItemTests {
    @Test func toolBlocksCollapseIntoOneRow() throws {
        let page = try FixtureFiles.decode(MessagePage.self, "messages.json")
        let items = ChatItem.build(from: page.messages)
        #expect(items.count == 6)
        guard case .tools(_, let steps, _) = items[2] else { Issue.record("expected tools row"); return }
        #expect(steps.map(\.name) == ["Grep", "Edit", "Bash"])
        #expect(steps.map(\.isError) == [false, false, true])
        #expect(steps.allSatisfy { $0.finished })
        guard case .tools(_, let live, _) = items[5] else { Issue.record("expected trailing tools row"); return }
        #expect(live.map(\.finished) == [false])
    }

    @Test func markdownBlocks() {
        let blocks = MarkdownParser.parse("## Title\n\nSome **bold**\ntext\n\n- one\n- two\n\n1. first\n\n```swift\nlet x = 1\n```\n> quote")
        #expect(blocks == [
            .heading(level: 2, text: "Title"),
            .paragraph("Some **bold**\ntext"),
            .listItem(marker: "•", indent: 0, text: "one"),
            .listItem(marker: "•", indent: 0, text: "two"),
            .listItem(marker: "1.", indent: 0, text: "first"),
            .code(language: "swift", code: "let x = 1"),
            .quote("quote"),
        ])
    }
}
