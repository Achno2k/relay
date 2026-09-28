import Foundation
import RelayKit
import Testing
import UniformTypeIdentifiers
@testable import Relay

/// A backend whose socket the test drives: every `events()` call opens a new stream, and `emit`
/// yields into the newest one. Counts calls so reconnects and resyncs can be asserted.
final class SocketScriptBackend: Backend, @unchecked Sendable {
    private let lock = NSLock()
    private var streams: [AsyncStream<ConnectionEvent>.Continuation] = []
    private var closed = 0
    private var agentCalls = 0
    private var _agents: [Agent] = []
    private var _agentsError: (any Error)?
    private var _keysError: (any Error)?
    private var _approval: Approval?

    var eventsCalls: Int { lock.withLock { streams.count } }
    var closedStreams: Int { lock.withLock { closed } }
    var agentsCalls: Int { lock.withLock { agentCalls } }
    func setAgents(_ agents: [Agent]) { lock.withLock { _agents = agents } }
    func setAgentsError(_ error: (any Error)?) { lock.withLock { _agentsError = error } }
    func setKeysError(_ error: (any Error)?) { lock.withLock { _keysError = error } }
    func setApproval(_ approval: Approval?) { lock.withLock { _approval = approval } }

    func emit(_ event: ConnectionEvent) {
        lock.withLock { streams.last }?.yield(event)
    }

    func events() -> AsyncStream<ConnectionEvent> {
        let (stream, continuation) = AsyncStream.makeStream(of: ConnectionEvent.self)
        continuation.onTermination = { [weak self] _ in
            guard let self else { return }
            self.lock.withLock { self.closed += 1 }
        }
        lock.withLock { streams.append(continuation) }
        return stream
    }

    func agents() async throws -> [Agent] {
        let (agents, error) = lock.withLock { agentCalls += 1; return (_agents, _agentsError) }
        if let error { throw error }
        return agents
    }
    func workspaces() async throws -> [Workspace] { [] }
    func messages(agentId: String, before: String?, limit: Int) async throws -> MessagePage { MessagePage(messages: [], hasMore: false) }
    func prompt(agentId: String, text: String, attachments: [String]) async throws {}
    func uploadAttachment(
        agentId: String, data: Data, filename: String, contentType: String,
        progress: @escaping @Sendable (Double) -> Void
    ) async throws -> RelayKit.Attachment {
        RelayKit.Attachment(id: "a", name: filename, kind: .file, size: data.count)
    }
    func attachmentData(agentId: String, attachmentId: String) async throws -> Data { Data() }
    func machine() async throws -> Machine { Machine(id: "m1", name: "Test Mac", kind: .laptop) }
    func sendKeys(agentId: String, keys: [String]) async throws {
        if let error = lock.withLock({ _keysError }) { throw error }
    }
    func sendText(agentId: String, text: String, submit: Bool) async throws {}
    func approval(agentId: String) async throws -> Approval? { lock.withLock { _approval } }
    func createAgent(_ request: CreateAgentRequest) async throws -> Agent { throw RelayError.badResponse }
    func controls() async throws -> ControlsCatalog { ControlsCatalog(models: [], modes: [], efforts: []) }
    func kindControls(kind: String) async throws -> AgentControlsInfo { throw RelayError.badResponse }
    func agentControls(agentId: String) async throws -> AgentControlsInfo { throw RelayError.badResponse }
    func control(agentId: String, _ request: ControlRequest) async throws -> Agent { throw RelayError.badResponse }
    func usage() async throws -> UsageSnapshot { UsageSnapshot(providers: []) }
    func refreshUsage() async throws {}
}

private func agent(status: AgentStatus = .idle) -> Agent {
    Agent(
        id: "w1:p1", name: nil, kind: "claude", title: "Test agent", workspaceId: "w1", workspaceName: "shop-api",
        cwdName: "shop-api", status: status, hasTranscript: true, updatedAt: .now
    )
}

/// Lets the store's tasks run: event handling hops through a stream and the main actor.
private func settle() async throws {
    try await Task.sleep(for: .milliseconds(150))
}

@MainActor
@Suite("Round 8: reconnects, re-pairing, approvals, attachments")
struct Round8Tests {
    // MARK: - R8-i1: a refused socket must end in the re-pair prompt

    @Test func refusedSocketChecksTheTokenOverREST() async throws {
        let backend = SocketScriptBackend()
        backend.setAgents([agent()])
        let store = AppStore(backend: backend, hostLabel: "test")
        store.start()
        try await settle()
        backend.emit(.connected)
        try await settle()
        #expect(!store.needsRePairing)

        // The token was rotated and the bridge restarted: the socket drops, then every upgrade is
        // answered with a bare 400 and REST says 401.
        backend.setAgentsError(RelayError.unauthorized)
        backend.emit(.disconnected)
        backend.emit(.rejected(status: 400))
        backend.emit(.disconnected)
        try await settle()

        #expect(store.needsRePairing, "a refused socket must lead to the re-pair banner, not endless Reconnecting…")
        #expect(store.connection == .reconnecting)
    }

    @Test func refusedSocketWith401NeedsNoRESTCall() async throws {
        let backend = SocketScriptBackend()
        backend.setAgents([agent()])
        let store = AppStore(backend: backend, hostLabel: "test")
        store.start()
        try await settle()
        let calls = backend.agentsCalls

        backend.emit(.rejected(status: 401))
        try await settle()

        #expect(store.needsRePairing)
        #expect(backend.agentsCalls == calls)
    }

    @Test func refusedSocketWithAGoodTokenDoesNotAskToRePair() async throws {
        let backend = SocketScriptBackend()
        backend.setAgents([agent()])
        let store = AppStore(backend: backend, hostLabel: "test")
        store.start()
        try await settle()

        backend.emit(.rejected(status: 503))
        backend.emit(.disconnected)
        try await settle()

        #expect(!store.needsRePairing)
        #expect(store.connection == .reconnecting)
    }

    @Test func handshakeRejectionReadsTheUpgradeStatus() throws {
        let url = URL(string: "http://127.0.0.1:7878/ws")!
        #expect(WSClient.rejection(HTTPURLResponse(url: url, statusCode: 400, httpVersion: nil, headerFields: nil)) == 400)
        #expect(WSClient.rejection(HTTPURLResponse(url: url, statusCode: 101, httpVersion: nil, headerFields: nil)) == nil)
        #expect(WSClient.rejection(nil) == nil, "no answer at all (offline) is not a refusal")
    }

    // MARK: - R8-i2: a failed approval answer brings the card back

    @Test func failedAnswerKeepsTheApprovalAnswerable() async throws {
        let backend = SocketScriptBackend()
        let blocked = agent(status: .blocked)
        let question = Approval(
            agentId: blocked.id, question: "Tea or coffee?",
            options: [ApprovalOption(label: "Tea", keys: ["1"]), ApprovalOption(label: "Coffee", keys: ["2"])]
        )
        backend.setAgents([blocked])
        backend.setApproval(question)
        let store = AppStore(backend: backend, hostLabel: "test")
        store.selectedAgentId = blocked.id
        await store.refresh()
        #expect(store.approval == question)

        backend.setKeysError(RelayError.unreachable(timedOut: false))
        store.answer(question.options[0])
        try await settle()

        #expect(store.approval == question, "the agent is still asking; the card must come back")
        #expect(!store.isApprovalSheetPresented, "don't throw the sheet back up on its own")
        #expect(store.errorMessage == RelayError.unreachable(timedOut: false).errorDescription)

        // And it isn't treated as "just answered" when it's fetched again.
        await store.refreshApproval()
        #expect(store.approval == question)
    }

    // MARK: - R8-i3: background closes the socket, foreground and the network coming back reopen it

    @Test func backgroundClosesTheSocketAndForegroundReopensIt() async throws {
        let backend = SocketScriptBackend()
        backend.setAgents([agent()])
        let store = AppStore(backend: backend, hostLabel: "test")
        store.start()
        try await settle()
        backend.emit(.connected)
        try await settle()
        #expect(backend.eventsCalls == 1)

        store.suspend()
        try await settle()
        #expect(backend.closedStreams == 1, "the socket is closed while the app is in the background")

        let before = backend.agentsCalls
        await store.resume()
        #expect(backend.eventsCalls == 2, "foreground opens a fresh socket")
        #expect(backend.agentsCalls == before + 1, "and resyncs at once")

        // Frames sent while it was closed are gone: the new socket's hello resyncs again.
        backend.emit(.connected)
        try await settle()
        #expect(backend.agentsCalls == before + 2)
        #expect(store.connection == .connected)
    }

    @Test func foregroundWhileConnectedOnlyResyncs() async throws {
        let backend = SocketScriptBackend()
        backend.setAgents([agent()])
        let store = AppStore(backend: backend, hostLabel: "test")
        store.start()
        try await settle()
        backend.emit(.connected)
        try await settle()

        await store.resume()  // e.g. back from Control Center: never went to the background
        #expect(backend.eventsCalls == 1)
        #expect(backend.closedStreams == 0)
    }

    @Test func foregroundWhileReconnectingSkipsTheBackoff() async throws {
        let backend = SocketScriptBackend()
        backend.setAgents([agent()])
        let store = AppStore(backend: backend, hostLabel: "test")
        store.start()
        try await settle()
        backend.emit(.disconnected)
        try await settle()

        await store.resume()
        #expect(backend.eventsCalls == 2, "a socket waiting out a 30 s backoff is retried now")
    }

    @Test func networkComingBackReconnectsAtOnce() async throws {
        let backend = SocketScriptBackend()
        backend.setAgents([agent()])
        let store = AppStore(backend: backend, hostLabel: "test")
        store.start()
        try await settle()
        backend.emit(.connected)
        try await settle()

        store.networkBecameAvailable()
        #expect(backend.eventsCalls == 1, "a healthy socket is left alone")

        backend.emit(.disconnected)  // airplane mode on
        try await settle()
        store.networkBecameAvailable()  // and off again
        #expect(backend.eventsCalls == 2)
        #expect(backend.closedStreams == 1, "the old stream, stuck in its backoff sleep, is dropped")

        backend.emit(.connected)
        try await settle()
        #expect(store.connection == .connected)
    }

    @Test func networkChangesWhileSuspendedDoNothing() async throws {
        let backend = SocketScriptBackend()
        backend.setAgents([agent()])
        let store = AppStore(backend: backend, hostLabel: "test")
        store.start()
        try await settle()
        backend.emit(.disconnected)
        try await settle()
        store.suspend()

        store.networkBecameAvailable()
        #expect(backend.eventsCalls == 1, "no socket while in the background")
    }

    // MARK: - R8-i5: a resync merges the latest page into the open chat

    private func msg(_ id: String, _ role: Message.Role = .assistant, at seconds: TimeInterval) -> Message {
        Message(id: id, role: role, createdAt: Date(timeIntervalSince1970: seconds), blocks: [.text(id)])
    }

    @Test func resyncKeepsOlderPagesTheUserScrolledTo() {
        var state = RelayState()
        state.setPage(MessagePage(messages: (3...5).map { msg("m\($0)", at: Double($0)) }, hasMore: true), agentId: "a")
        state.prependPage(MessagePage(messages: (1...2).map { msg("m\($0)", at: Double($0)) }, hasMore: false), agentId: "a")

        // Foreground: the latest page again, now with one new message and m5 grown.
        var grown = msg("m5", at: 5)
        grown.blocks = [.text("m5, longer")]
        state.mergeLatestPage(MessagePage(messages: [msg("m4", at: 4), grown, msg("m6", at: 6)], hasMore: true), agentId: "a")

        #expect(state.messages["a"]?.map(\.id) == ["m1", "m2", "m3", "m4", "m5", "m6"])
        #expect(state.messages["a"]?[4].blocks == [.text("m5, longer")], "the page's copy is the fresher one")
        #expect(state.hasMore["a"] == false, "the oldest page loaded is still the oldest there is")
    }

    @Test func resyncKeepsAMessageTheSocketDeliveredMidRequest() {
        var state = RelayState()
        state.setPage(MessagePage(messages: [msg("m1", at: 1), msg("m2", at: 2)], hasMore: false), agentId: "a")
        // While the page request was in flight, the user's prompt landed over the socket.
        state.upsert(msg("u3", .user, at: 3), agentId: "a")

        state.mergeLatestPage(MessagePage(messages: [msg("m1", at: 1), msg("m2", at: 2)], hasMore: false), agentId: "a")

        #expect(state.messages["a"]?.map(\.id) == ["m1", "m2", "u3"])
    }

    @Test func resyncAfterALongGapReplacesTheChat() {
        var state = RelayState()
        state.setPage(MessagePage(messages: [msg("m1", at: 1), msg("m2", at: 2)], hasMore: false), agentId: "a")

        // More than a page arrived while away: nothing overlaps, so the loaded history can't be joined up.
        state.mergeLatestPage(MessagePage(messages: [msg("m60", at: 60), msg("m61", at: 61)], hasMore: true), agentId: "a")

        #expect(state.messages["a"]?.map(\.id) == ["m60", "m61"])
        #expect(state.hasMore["a"] == true)
    }

    // MARK: - R8-i4: Files picks are checked before they're read

    @Test func oversizedFileIsRejectedWithoutReadingIt() throws {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("big-\(UUID().uuidString).zip")
        FileManager.default.createFile(atPath: url.path, contents: nil)
        defer { try? FileManager.default.removeItem(at: url) }
        let handle = try FileHandle(forWritingTo: url)
        try handle.truncate(atOffset: UInt64(AttachmentProcessing.maxBytes + 1))  // sparse: no bytes written
        try handle.close()

        #expect(throws: AttachmentProcessing.Failure.tooLarge(name: url.lastPathComponent)) {
            try AttachmentProcessing.read(url)
        }
    }

    @Test func smallFileIsReadWithItsType() throws {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("notes-\(UUID().uuidString).txt")
        try Data("hello".utf8).write(to: url)
        defer { try? FileManager.default.removeItem(at: url) }

        let file = try AttachmentProcessing.read(url)
        #expect(file.data == Data("hello".utf8))
        #expect(file.type?.conforms(to: .plainText) == true)
    }

    @Test func pickedFilesArriveInOrderAndStopAtTen() async throws {
        let backend = SocketScriptBackend()
        let store = AppStore(backend: backend, hostLabel: "test")
        let attachments = ComposerAttachments(agentId: "w1:p1", store: store)
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("pick-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: dir) }
        let urls = try (1...12).map { i in
            let url = dir.appendingPathComponent("file-\(i).txt")
            try Data("file \(i)".utf8).write(to: url)
            return url
        }

        attachments.add(files: urls)
        for _ in 0..<50 where attachments.items.count < 10 || attachments.isBusy {
            try await Task.sleep(for: .milliseconds(50))
        }

        #expect(attachments.items.map(\.name) == (1...10).map { "file-\($0).txt" })
        #expect(attachments.uploaded.count == 10)
        #expect(store.errorMessage == "Up to 10 files per message.")
    }
}
