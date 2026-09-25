import Foundation
import Observation
import RelayKit
import Testing
@testable import Relay

/// `reply.live.tool`: the "Running Bash…" row shows as soon as the screen has the call, and the
/// transcript's toolCall takes over the same row when it lands.
@Suite("Live tool")
struct LiveToolTests {
    private let bash = LiveTool(name: "Bash", summary: "Ran ping -c 8 127.0.0.1")
    private let t0 = Date(timeIntervalSince1970: 1_000)

    private func prompt(_ id: String, _ text: String) -> Message {
        Message(id: id, role: .user, createdAt: t0, blocks: [.text(text)])
    }

    private func assistant(_ id: String, _ blocks: [Block]) -> Message {
        Message(id: id, role: .assistant, createdAt: t0, blocks: blocks)
    }

    private func call(_ id: String, _ name: String, _ summary: String) -> Block {
        .toolCall(ToolCall(id: id, name: name, summary: summary))
    }

    private func result(_ id: String) -> Block {
        .toolResult(ToolResult(toolCallId: id, isError: false, preview: "ok"))
    }

    // MARK: - Lifecycle

    @Test func startLandClear() throws {
        var live = LiveReplies()
        var transcript = [prompt("u1", "ping it"), assistant("a1", [.text("Pinging.")])]
        live.apply(agentId: "w1:p1", text: nil, tool: bash, transcript: transcript)
        let run = try #require(live["w1:p1"]?.tool)
        #expect(run.matchedCallId == nil)

        // Repeated frames for the same tool keep the same run (and placeholder id).
        live.apply(agentId: "w1:p1", text: nil, tool: bash, transcript: transcript)
        #expect(live["w1:p1"]?.tool?.placeholderId == run.placeholderId)

        transcript.append(assistant("a2", [call("c1", "Bash", "Ran ping -c 8 127.0.0.1")]))
        live.match(agentId: "w1:p1", transcript: transcript)
        #expect(live["w1:p1"]?.tool?.matchedCallId == "c1")
        #expect(live.aliases["w1:p1"] == ["c1": run.placeholderId])

        // A frame for the same tool after it landed doesn't bring the placeholder back.
        live.apply(agentId: "w1:p1", text: nil, tool: bash, transcript: transcript)
        #expect(live["w1:p1"]?.tool?.matchedCallId == "c1")

        live.apply(agentId: "w1:p1", text: nil, tool: nil, transcript: transcript)
        #expect(live["w1:p1"] == nil)
        #expect(live.aliases["w1:p1"] == ["c1": run.placeholderId], "the landed row keeps the placeholder's id")
    }

    @Test func refinedSummaryKeepsTheSameRow() throws {
        var live = LiveReplies()
        var transcript = [prompt("u1", "go")]
        live.apply(agentId: "a", text: nil, tool: LiveTool(name: "Bash", summary: "Ran a command"), transcript: transcript)
        let placeholder = try #require(live["a"]?.tool?.placeholderId)
        live.apply(agentId: "a", text: nil, tool: bash, transcript: transcript)
        #expect(live["a"]?.tool?.placeholderId == placeholder)
        #expect(live["a"]?.tool?.tool.summary == bash.summary)

        transcript.append(assistant("m1", [call("c1", "Bash", bash.summary)]))
        live.match(agentId: "a", transcript: transcript)
        // The next Bash (no null in between) is a new call with its own row.
        live.apply(agentId: "a", text: nil, tool: LiveTool(name: "Bash", summary: "Ran ls"), transcript: transcript)
        #expect(live["a"]?.tool?.placeholderId != placeholder)
        #expect(live["a"]?.tool?.matchedCallId == nil)
    }

    @Test func fallsBackToLatestNewCallWithTheSameName() {
        var live = LiveReplies()
        var transcript = [prompt("u1", "go")]
        live.apply(agentId: "a", text: nil, tool: LiveTool(name: "Bash", summary: "Ran ping -c 8 127.0.0.…"), transcript: transcript)
        transcript.append(assistant("m1", [call("c1", "Read", "Read x"), call("c2", "Bash", "Ran ping -c 8 127.0.0.1")]))
        live.match(agentId: "a", transcript: transcript)
        #expect(live["a"]?.tool?.matchedCallId == "c2")
    }

    @Test func neverMatchesAnEarlierCall() {
        var live = LiveReplies()
        // Same command in the previous turn, and a finished Bash earlier in this one.
        let transcript = [
            prompt("u1", "first"), assistant("m1", [call("old", "Bash", bash.summary), result("old")]),
            prompt("u2", "again"), assistant("m2", [call("c1", "Bash", "Ran ls"), result("c1")]),
        ]
        live.apply(agentId: "a", text: nil, tool: bash, transcript: transcript)
        #expect(live["a"]?.tool?.matchedCallId == nil)
    }

    @Test func transcriptThatBeatTheScreenStillMatches() {
        var live = LiveReplies()
        // The call is already in the transcript, still running, when the first frame arrives.
        let transcript = [prompt("u1", "go"), assistant("m1", [call("c1", "Bash", bash.summary)])]
        live.apply(agentId: "a", text: nil, tool: bash, transcript: transcript)
        #expect(live["a"]?.tool?.matchedCallId == "c1")
    }

    @Test func secondIdenticalCallGetsItsOwnRow() {
        var live = LiveReplies()
        var transcript = [prompt("u1", "go")]
        live.apply(agentId: "a", text: nil, tool: bash, transcript: transcript)
        transcript.append(assistant("m1", [call("c1", "Bash", bash.summary)]))
        live.match(agentId: "a", transcript: transcript)
        live.apply(agentId: "a", text: nil, tool: nil, transcript: transcript)
        transcript[1].blocks.append(result("c1"))

        live.apply(agentId: "a", text: nil, tool: bash, transcript: transcript)
        #expect(live["a"]?.tool?.matchedCallId == nil, "the finished first call isn't this one")
        transcript.append(assistant("m2", [call("c2", "Bash", bash.summary)]))
        live.match(agentId: "a", transcript: transcript)
        #expect(live["a"]?.tool?.matchedCallId == "c2")
        #expect(Set(live.aliases["a"]!.values).count == 2)
    }

    @Test func agentsAreIndependent() {
        var live = LiveReplies()
        let transcript = [prompt("u1", "go")]
        live.apply(agentId: "a", text: nil, tool: bash, transcript: transcript)
        live.apply(agentId: "b", text: "Thinking about it", tool: nil, transcript: transcript)
        #expect(live["a"]?.tool != nil && live["a"]?.text == nil)
        #expect(live["b"]?.tool == nil && live["b"]?.text == "Thinking about it")
        // b's transcript growing doesn't match a's tool.
        live.match(agentId: "b", transcript: transcript + [assistant("m1", [call("c1", "Bash", bash.summary)])])
        #expect(live["a"]?.tool?.matchedCallId == nil)
    }

    // MARK: - Live text

    @Test func toolTextNeverShowsAsProse() {
        #expect(LiveReplies.prose("⏺ Bash(ls -la)\n  ⎿  total 8\n     drwxr-xr-x  3 me") == nil)
        #expect(LiveReplies.prose("• Ran ping -c 8 127.0.0.1\n  └ PING 127.0.0.1") == nil)
        #expect(LiveReplies.prose("• Running cargo test") == nil)
        #expect(LiveReplies.prose("Checking the tests.\n\n⏺ Bash(npm test)\n  ⎿  Running…\n\nAll good.") == "Checking the tests.\n\nAll good.")
        // Prose that merely mentions a call or starts with a bullet stays.
        #expect(LiveReplies.prose("I'll call Bash(ls) next.") == "I'll call Bash(ls) next.")
        #expect(LiveReplies.prose("• Ranking is stable") == "• Ranking is stable")
    }

    @Test func textNeverStepsBackToAShorterPrefix() {
        var live = LiveReplies()
        live.apply(agentId: "a", text: "The answer is long", tool: nil, transcript: [])
        live.apply(agentId: "a", text: "The answer", tool: nil, transcript: [])
        #expect(live["a"]?.text == "The answer is long")
        live.apply(agentId: "a", text: "Second paragraph", tool: nil, transcript: [])
        #expect(live["a"]?.text == "Second paragraph")
        live.apply(agentId: "a", text: nil, tool: nil, transcript: [])
        #expect(live["a"] == nil)
    }

    // MARK: - Chat rows

    private func toolRows(_ items: [ChatItem]) -> [(id: String, steps: [ToolStep])] {
        items.compactMap { if case .tools(let id, let steps, _) = $0 { (id, steps) } else { nil } }
    }

    @Test func placeholderSwapsInPlaceWhenTheCallLands() throws {
        var live = LiveReplies()
        var transcript = [prompt("u1", "ping it"), assistant("a1", [.text("Pinging.")])]
        let pending = ChatItem.user(id: "local-1", text: "and then?", pending: true)
        live.apply(agentId: "a", text: nil, tool: bash, transcript: transcript)

        let before = ChatItem.withLive(ChatItem.build(from: transcript) + [pending], live: live["a"], aliases: live.aliases["a"] ?? [:], working: true)
        let placeholder = try #require(live["a"]?.tool?.placeholderId)
        #expect(before.map(\.id) == ["u1", "a1#0", placeholder, "local-1"], "tool row goes before the pending prompt")

        transcript.append(assistant("a2", [call("c1", "Bash", bash.summary)]))
        live.match(agentId: "a", transcript: transcript)
        let after = ChatItem.withLive(ChatItem.build(from: transcript) + [pending], live: live["a"], aliases: live.aliases["a"] ?? [:], working: true)
        #expect(after.map(\.id) == before.map(\.id), "same rows, same ids: no jump, no duplicate")
        #expect(toolRows(after).count == 1)
        #expect(toolRows(after)[0].steps.map(\.name) == ["Bash"])

        // Still the same id once the bridge clears the tool and the result lands.
        live.apply(agentId: "a", text: nil, tool: nil, transcript: transcript)
        transcript[2].blocks.append(result("c1"))
        let done = ChatItem.withLive(ChatItem.build(from: transcript), live: live["a"], aliases: live.aliases["a"] ?? [:], working: false)
        #expect(toolRows(done).map(\.id) == [placeholder])
    }

    @Test func placeholderJoinsTheRunningToolGroup() throws {
        var live = LiveReplies()
        var transcript = [prompt("u1", "go"), assistant("a1", [call("c0", "Read", "Read a.swift"), result("c0")])]
        live.apply(agentId: "a", text: nil, tool: bash, transcript: transcript)
        let before = ChatItem.withLive(ChatItem.build(from: transcript), live: live["a"], aliases: live.aliases["a"] ?? [:], working: true)
        #expect(toolRows(before).count == 1)
        #expect(toolRows(before)[0].id == "a1#0")
        #expect(toolRows(before)[0].steps.map(\.name) == ["Read", "Bash"])

        transcript[1].blocks.append(call("c1", "Bash", bash.summary))
        live.match(agentId: "a", transcript: transcript)
        let after = ChatItem.withLive(ChatItem.build(from: transcript), live: live["a"], aliases: live.aliases["a"] ?? [:], working: true)
        #expect(toolRows(after).map(\.id) == ["a1#0"])
        #expect(toolRows(after)[0].steps.map(\.id) == toolRows(before)[0].steps.map(\.id))
    }

    @Test func liveTextComesBeforeTheToolRow() {
        var live = LiveReplies()
        let transcript = [prompt("u1", "go")]
        live.apply(agentId: "a", text: "Let me look.", tool: bash, transcript: transcript)
        let items = ChatItem.withLive(ChatItem.build(from: transcript), live: live["a"], aliases: [:], working: true)
        #expect(items.map(\.id) == ["u1", ChatItem.liveTextId, live["a"]!.tool!.placeholderId])
    }

    @Test func nothingLiveOnceTheAgentStops() {
        var live = LiveReplies()
        let transcript = [prompt("u1", "go")]
        live.apply(agentId: "a", text: "Partial", tool: bash, transcript: transcript)
        let items = ChatItem.withLive(ChatItem.build(from: transcript), live: live["a"], aliases: [:], working: false)
        #expect(items.map(\.id) == ["u1"])
    }
}

/// The same lifecycle through `AppStore`, as the socket drives it.
@Suite("Live tool store")
@MainActor
struct LiveToolStoreTests {
    private let bash = LiveTool(name: "Bash", summary: "Ran sleep 3")

    private func agent(_ id: String, _ status: AgentStatus) -> Agent {
        Agent(
            id: id, name: nil, kind: "claude", title: "chat", workspaceId: "w1", workspaceName: "shop-api",
            cwdName: "shop-api", status: status, hasTranscript: true, updatedAt: .now
        )
    }

    private func store() -> AppStore {
        let backend = RecordingBackend(agent: agent("w1:p1", .working), approval: Approval(agentId: "w1:p1", question: "", options: []))
        let store = AppStore(backend: backend, hostLabel: "test")
        store.apply(.agentCreated(agent("w1:p1", .working)))
        store.apply(.agentCreated(agent("w1:p2", .working)))
        return store
    }

    @Test func toolLandsThenClears() async {
        let store = store()
        await store.loadMessages("w1:p1")
        store.apply(.replyLive(agentId: "w1:p1", text: nil, seq: 1, tool: bash))
        #expect(store.live["w1:p1"]?.tool?.tool == bash)
        let call = ToolCall(id: "c1", name: "Bash", summary: "Ran sleep 3")
        store.apply(.messageUpserted(agentId: "w1:p1", message: Message(id: "m1", role: .assistant, createdAt: .now, blocks: [.toolCall(call)])))
        #expect(store.live["w1:p1"]?.tool?.matchedCallId == "c1")
        store.apply(.replyLive(agentId: "w1:p1", text: nil, seq: 2, tool: nil))
        #expect(store.live["w1:p1"] == nil)
    }

    @Test func stopMidToolClears() {
        let store = store()
        store.apply(.replyLive(agentId: "w1:p1", text: "Running it", seq: 1, tool: bash))
        store.apply(.agentUpdated(agent("w1:p1", .idle)))
        #expect(store.live["w1:p1"] == nil)
    }

    @Test func approvalMidToolKeepsIt() {
        let store = store()
        store.apply(.replyLive(agentId: "w1:p1", text: nil, seq: 1, tool: bash))
        store.apply(.agentUpdated(agent("w1:p1", .blocked)))
        #expect(store.live["w1:p1"]?.tool != nil)
    }

    @Test func switchingAgentsMidTool() {
        let store = store()
        store.selectedAgentId = "w1:p1"
        store.apply(.replyLive(agentId: "w1:p1", text: nil, seq: 1, tool: bash))
        store.selectedAgentId = "w1:p2"
        store.apply(.replyLive(agentId: "w1:p2", text: "Other agent", seq: 1, tool: nil))
        #expect(store.live["w1:p2"]?.tool == nil)
        #expect(store.live["w1:p1"]?.tool?.tool == bash, "the first agent's tool is still running when you come back")
        store.selectedAgentId = "w1:p1"
        #expect(store.live["w1:p1"]?.tool?.matchedCallId == nil)
    }

    @Test func unchangedFrameDoesNotRerender() {
        let store = store()
        store.apply(.replyLive(agentId: "w1:p1", text: "Same", seq: 1, tool: bash))
        let flag = Flag()
        withObservationTracking { _ = store.live } onChange: { flag.set() }
        store.apply(.replyLive(agentId: "w1:p1", text: "Same", seq: 2, tool: bash))
        // An older bridge's tool block as text adds nothing either.
        store.apply(.replyLive(agentId: "w1:p1", text: "Same\n⏺ Bash(sleep 3)\n  ⎿  Running…", seq: 3, tool: bash))
        #expect(!flag.value)
        store.apply(.replyLive(agentId: "w1:p1", text: "Same, then more", seq: 4, tool: bash))
        #expect(flag.value)
    }

    @Test func staleFrameIgnored() {
        let store = store()
        store.apply(.replyLive(agentId: "w1:p1", text: nil, seq: 2, tool: nil))
        store.apply(.replyLive(agentId: "w1:p1", text: nil, seq: 1, tool: bash))
        #expect(store.live["w1:p1"] == nil)
    }

    @Test func decodesTheToolObject() throws {
        func decode(_ json: String) throws -> ServerEvent {
            try RelayJSON.decoder().decode(ServerEvent.self, from: Data(json.utf8))
        }
        let with = try decode(#"{"type":"reply.live","agentId":"w1:p1","seq":3,"text":null,"tool":{"name":"Bash","summary":"Ran ls","state":"running"}}"#)
        #expect(with == .replyLive(agentId: "w1:p1", text: nil, seq: 3, tool: LiveTool(name: "Bash", summary: "Ran ls")))
        let null = try decode(#"{"type":"reply.live","agentId":"w1:p1","seq":4,"text":"Hi","tool":null}"#)
        #expect(null == .replyLive(agentId: "w1:p1", text: "Hi", seq: 4, tool: nil))
        let old = try decode(#"{"type":"reply.live","agentId":"w1:p1","seq":5,"text":"Hi"}"#)
        #expect(old == .replyLive(agentId: "w1:p1", text: "Hi", seq: 5, tool: nil))
        let odd = try decode(#"{"type":"reply.live","agentId":"w1:p1","seq":6,"text":"Hi","tool":{"summary":42}}"#)
        #expect(odd == .replyLive(agentId: "w1:p1", text: "Hi", seq: 6, tool: nil))
    }
}

private final class Flag: @unchecked Sendable {
    private(set) var value = false
    func set() { value = true }
}
