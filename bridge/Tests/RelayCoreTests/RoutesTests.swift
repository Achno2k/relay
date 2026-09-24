import Foundation
import Hummingbird
import HummingbirdTesting
import HummingbirdWSTesting
import Synchronization
import Testing
@testable import RelayCore

@Suite(.serialized) struct RoutesTests {
    static let token = "test-token"
    static let auth: HTTPFields = [.authorization: "Bearer \(token)"]

    /// Sets up a fake herdr with a claude agent (w1:p1, with transcript), a blocked claude agent whose
    /// transcript doesn't exist yet (w1:p2) and a gemini agent, a kind without a parser (w2:p3).
    func withApp(
        status: @escaping @Sendable (String) -> String = { $0 == "w1:p2" ? "blocked" : "idle" },
        _ body: @escaping @Sendable (any TestClientProtocol, FakeHerdr, EventHub) async throws -> Void
    ) async throws {
        let projects = URL(fileURLWithPath: "/tmp").appendingPathComponent("relay-projects-\(UUID().uuidString.prefix(8))")
        let dir = projects.appendingPathComponent(TranscriptLocator.projectDirName(for: "/Users/dev/shop-api"))
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        try Fixture.data("claude-session.jsonl").write(to: dir.appendingPathComponent("sess-1.jsonl"))
        defer { try? FileManager.default.removeItem(at: projects) }

        let fake = try FakeHerdr { method, params in
            let agents: [[String: Any]] = [
                World.agent("w1:p1", name: "api-refactor", status: status("w1:p1"), cwd: "/Users/dev/shop-api",
                            session: ["source": "herdr:claude", "agent": "claude", "kind": "id", "value": "sess-1"], title: "Refactor /Users/dev/shop-api/auth"),
                World.agent("w1:p2", name: "tests", status: status("w1:p2"), cwd: "/Users/dev/shop-api",
                            session: ["source": "herdr:claude", "agent": "claude", "kind": "id", "value": "missing"]),
                {
                    var a = World.agent("w2:p3", name: nil, status: status("w2:p3"), cwd: "/Users/dev/website", session: nil, title: "")
                    a["agent"] = "gemini"
                    return a
                }(),
            ]
            let target = params["target"] as? String
            switch method {
            case "agent.list": return ["type": "agent_list", "agents": agents]
            case "workspace.list": return ["type": "workspace_list", "workspaces": World.workspaces]
            case "agent.get":
                guard let a = agents.first(where: { $0["pane_id"] as? String == target || $0["name"] as? String == target }) else {
                    return FakeError(code: "agent_not_found", message: "no agent \(target ?? "")")
                }
                return ["type": "agent_info", "agent": a]
            case "agent.read":
                let text = (params["source"] as? String) == "detection"
                    ? " Bash command\n\n   rm -rf /Users/dev/shop-api/build\n\n Do you want to proceed?\n ❯ 1. Yes\n   2. No, and tell Claude what to do differently (esc)\n"
                    : "$ codex\n> working in /Users/dev/website/src\n"
                return ["type": "pane_read", "read": ["pane_id": target ?? "", "workspace_id": "w1", "tab_id": "w1:t1", "source": params["source"] ?? "", "format": "text", "text": text, "revision": 7, "truncated": false]]
            case "agent.prompt":
                if target == "w1:p2" { return FakeError(code: "agent_blocked", message: "agent is blocked") }
                return ["type": "agent_prompted", "agent": agents[0]]
            case "agent.send_keys", "pane.send_text": return ["type": "ok"]
            case "events.subscribe": return ["type": "subscription_started"]
            default: return FakeError(code: "unknown_method", message: method)
            }
        }
        defer { fake.stop() }

        let uploads = UploadStore(root: projects.appendingPathComponent("uploads"))
        let service = AgentService(herdr: HerdrClient(socketPath: fake.socketPath), locator: TranscriptLocator(claudeProjects: projects, codex: CodexRollouts(root: projects.appendingPathComponent("codex"))), uploads: uploads)
        let hub = EventHub()
        let router = RelayRoutes.router(service: service, hub: hub, token: Self.token)
        let app = Application(router: router, server: .http1WebSocketUpgrade(webSocketRouter: router))
        try await app.test(.live) { client in try await body(client, fake, hub) }
    }

    func decode<T: Decodable>(_ t: T.Type, _ r: TestResponse) throws -> T {
        try JSONDecoder().decode(T.self, from: Data(buffer: r.body))
    }

    @Test func healthNeedsNoAuth() async throws {
        try await withApp { client, _, _ in
            try await client.execute(uri: "/health", method: .get) { r throws in
                #expect(r.status == .ok)
                let o = try JSONSerialization.jsonObject(with: Data(buffer: r.body)) as? [String: Any]
                #expect(o?["ok"] as? Bool == true)
                #expect(o?["version"] as? String == Relay.version)
            }
        }
    }

    @Test func rejectsMissingOrWrongToken() async throws {
        try await withApp { client, _, _ in
            try await client.execute(uri: "/agents", method: .get) { r throws in
                #expect(r.status == .unauthorized)
                let err = try decode(ErrorBody.self, r)
                #expect(err.error.code == "unauthorized")
            }
            try await client.execute(uri: "/agents", method: .get, headers: [.authorization: "Bearer nope"]) { r throws in
                #expect(r.status == .unauthorized)
            }
        }
    }

    @Test func listsAgentsWithoutLeakingPaths() async throws {
        try await withApp { client, _, _ in
            try await client.execute(uri: "/agents", method: .get, headers: Self.auth) { r throws in
                #expect(r.status == .ok)
                let body = String(buffer: r.body)
                #expect(!body.contains("/Users/"))
                let agents = try decode([Agent].self, r)
                #expect(agents.map(\.id) == ["w1:p1", "w1:p2", "w2:p3"])
                #expect(agents[0].name == "api-refactor")
                #expect(agents[0].title == "Refactor auth")
                #expect(agents[0].workspaceName == "shop-api")
                #expect(agents[0].cwdName == "shop-api")
                #expect(agents[0].hasTranscript)
                #expect(!agents[1].hasTranscript)
                #expect(agents[1].status == .blocked)
                #expect(agents[2].kind == "gemini")
                #expect(agents[2].name == nil)
                #expect(agents[2].title == "gemini")
                #expect(agents.map(\.transcriptState) == [.ready, .pending, .unsupported])
                #expect(body.contains(#""transcriptState":"pending""#))
                #expect(body.contains(#""name":null"#))
            }
        }
    }

    @Test func workspacesCountAgents() async throws {
        try await withApp { client, _, _ in
            try await client.execute(uri: "/workspaces", method: .get, headers: Self.auth) { r throws in
                #expect(try decode([Workspace].self, r) == [
                    Workspace(id: "w1", name: "shop-api", agentCount: 2),
                    Workspace(id: "w2", name: "website", agentCount: 1),
                ])
            }
        }
    }

    @Test func getAgentDecodesEncodedId() async throws {
        try await withApp { client, _, _ in
            try await client.execute(uri: "/agents/w1%3Ap1", method: .get, headers: Self.auth) { r throws in
                #expect(r.status == .ok)
                #expect(try decode(Agent.self, r).id == "w1:p1")
            }
            try await client.execute(uri: "/agents/w9%3Ap9", method: .get, headers: Self.auth) { r throws in
                #expect(r.status == .notFound)
                let err = try decode(ErrorBody.self, r)
                #expect(err.error.code == "not_found")
            }
        }
    }

    @Test func pagesMessagesOldestFirst() async throws {
        try await withApp { client, _, _ in
            try await client.execute(uri: "/agents/w1%3Ap1/messages?limit=2", method: .get, headers: Self.auth) { r throws in
                let page = try decode(MessagePage.self, r)
                #expect(page.messages.map(\.id) == ["u2", "a6"])
                #expect(page.hasMore)
                #expect(!String(buffer: r.body).contains("/Users/"))
            }
            try await client.execute(uri: "/agents/w1%3Ap1/messages?before=u2&limit=50", method: .get, headers: Self.auth) { r throws in
                let page = try decode(MessagePage.self, r)
                #expect(page.messages.map(\.id) == ["u1", "a1"])
                #expect(!page.hasMore)
            }
            try await client.execute(uri: "/agents/w1%3Ap1/messages?before=nope", method: .get, headers: Self.auth) { r throws in
                #expect(r.status == .notFound)
            }
        }
    }

    @Test func screenFallbackWithoutTranscript() async throws {
        try await withApp { client, _, _ in
            try await client.execute(uri: "/agents/w2%3Ap3/messages", method: .get, headers: Self.auth) { r throws in
                let page = try decode(MessagePage.self, r)
                #expect(page.messages.count == 1)
                #expect(page.messages[0].id == "screen:w2:p3")
                #expect(page.messages[0].blocks == [.text("```\n$ codex\n> working in src\n```")])
            }
        }
    }

    @Test func pendingTranscriptIsEmptyNotAScreenRead() async throws {
        try await withApp { client, fake, _ in
            try await client.execute(uri: "/agents/w1%3Ap2/messages", method: .get, headers: Self.auth) { r throws in
                let page = try decode(MessagePage.self, r)
                #expect(page.messages.isEmpty)
                #expect(!page.hasMore)
            }
            // No screen read for a kind that will have a transcript.
            #expect(fake.params(of: "agent.read")?.contains("recent") != true)
        }
    }

    @Test func approvalWhenBlockedElse204() async throws {
        try await withApp { client, _, _ in
            try await client.execute(uri: "/agents/w1%3Ap2/approval", method: .get, headers: Self.auth) { r throws in
                #expect(r.status == .ok)
                #expect(try decode(Approval.self, r) == Approval(agentId: "w1:p2", question: "Do you want to proceed?", options: [
                    .init(label: "Yes", keys: ["1"]),
                    .init(label: "No, and tell Claude what to do differently", keys: ["esc"]),
                ]))
            }
            try await client.execute(uri: "/agents/w1%3Ap1/approval", method: .get, headers: Self.auth) { r throws in
                #expect(r.status == .noContent)
            }
        }
    }

    @Test func promptAndKeysForwardToHerdr() async throws {
        try await withApp { client, fake, _ in
            try await client.execute(uri: "/agents/w1%3Ap1/prompt", method: .post, headers: Self.auth, body: ByteBuffer(string: #"{"text":"hi"}"#)) { r throws in
                #expect(r.status == .accepted)
                #expect(String(buffer: r.body) == "{}")
            }
            #expect(fake.params(of: "agent.prompt") == #"{"target":"w1:p1","text":"hi"}"#)
            try await client.execute(uri: "/agents/w1%3Ap1/keys", method: .post, headers: Self.auth, body: ByteBuffer(string: #"{"keys":["esc"]}"#)) { r throws in
                #expect(r.status == .accepted)
            }
            #expect(fake.params(of: "agent.send_keys") == #"{"keys":["esc"],"target":"w1:p1"}"#)
        }
    }

    @Test func textRoute() async throws {
        try await withApp { client, fake, _ in
            try await client.execute(uri: "/agents/w1%3Ap2/text", method: .post, headers: Self.auth, body: ByteBuffer(string: #"{"text":"Green tea"}"#)) { r throws in
                #expect(r.status == .accepted)
            }
            #expect(fake.params(of: "pane.send_text") == #"{"pane_id":"w1:p2","text":"Green tea"}"#)
            #expect(fake.params(of: "agent.send_keys") == #"{"keys":["enter"],"target":"w1:p2"}"#)
            try await client.execute(uri: "/agents/w1%3Ap2/text", method: .post, headers: Self.auth, body: ByteBuffer(string: #"{"text":""}"#)) { r throws in
                #expect(r.status == .badRequest)
            }
        }
    }

    @Test func uploadServeAndPromptWithAttachments() async throws {
        try await withApp { client, fake, _ in
            var headers = Self.auth
            headers[.contentType] = "image/png"
            headers[.init("X-Filename")!] = "My%20Screen%20Shot%20%E2%9C%93.png"
            let png = Data([0x89, 0x50, 0x4E, 0x47, 1, 2, 3])
            var att: RelayCore.Attachment?
            try await client.execute(uri: "/agents/w1%3Ap1/attachments", method: .post, headers: headers, body: ByteBuffer(bytes: png)) { r throws in
                #expect(r.status == .created)
                att = try JSONDecoder().decode(RelayCore.Attachment.self, from: Data(buffer: r.body))
            }
            let a = try #require(att)
            #expect(a.name == "My-Screen-Shot.png")
            #expect(a.kind == .image)
            #expect(a.size == png.count)

            try await client.execute(uri: "/agents/w1%3Ap1/attachments/\(a.id)", method: .get, headers: Self.auth) { r throws in
                #expect(r.status == .ok)
                #expect(r.headers[.contentType] == "image/png")
                #expect(Data(buffer: r.body) == png)
            }
            try await client.execute(uri: "/agents/w1%3Ap1/attachments/0000000000000000", method: .get, headers: Self.auth) { r throws in
                #expect(r.status == .notFound)
            }

            let body = #"{"text":"What does it say?","attachments":["\#(a.id)"]}"#
            try await client.execute(uri: "/agents/w1%3Ap1/prompt", method: .post, headers: Self.auth, body: ByteBuffer(string: body)) { r throws in
                #expect(r.status == .accepted)
            }
            let sent = try #require(fake.params(of: "agent.prompt"))
            let text = try #require((try JSONSerialization.jsonObject(with: Data(sent.utf8)) as? [String: Any])?["text"] as? String)
            #expect(text.hasPrefix("What does it say?\n\nAttached files: /"))
            #expect(text.hasSuffix("/w1_p1/\(a.id)-My-Screen-Shot.png"))

            // Attachments only, no text.
            try await client.execute(uri: "/agents/w1%3Ap1/prompt", method: .post, headers: Self.auth, body: ByteBuffer(string: #"{"attachments":["\#(a.id)"]}"#)) { r throws in
                #expect(r.status == .accepted)
            }
            try await client.execute(uri: "/agents/w1%3Ap1/prompt", method: .post, headers: Self.auth, body: ByteBuffer(string: #"{"text":"x","attachments":["ffffffffffffffff"]}"#)) { r throws in
                #expect(r.status == .badRequest)
            }
        }
    }

    @Test func uploadLimits() async throws {
        try await withApp { client, _, _ in
            try await client.execute(uri: "/agents/w1%3Ap1/attachments", method: .post, headers: Self.auth, body: ByteBuffer()) { r throws in
                #expect(r.status == .badRequest)
            }
            let big = ByteBuffer(repeating: 0, count: UploadStore.maxBytes + 1)
            try await client.execute(uri: "/agents/w1%3Ap1/attachments", method: .post, headers: Self.auth, body: big) { r throws in
                #expect(r.status == .contentTooLarge)
                let err = try decode(ErrorBody.self, r)
                #expect(err.error.code == "too_large")
            }
            try await client.execute(uri: "/agents/w9%3Ap9/attachments", method: .post, headers: Self.auth, body: ByteBuffer(string: "x")) { r throws in
                #expect(r.status == .notFound)
            }
        }
    }

    @Test func machine() async throws {
        try await withApp { client, _, _ in
            try await client.execute(uri: "/machine", method: .get, headers: Self.auth) { r throws in
                let m = try JSONDecoder().decode(Machine.self, from: Data(buffer: r.body))
                #expect(!m.name.isEmpty)
                #expect(["laptop", "desktop"].contains(m.kind))
                #expect(m.os.hasPrefix("macOS "))
                #expect(!m.id.isEmpty)
            }
            try await client.execute(uri: "/machine", method: .get) { r throws in
                #expect(r.status == .unauthorized)
            }
        }
    }

    @Test func promptErrors() async throws {
        try await withApp { client, _, _ in
            try await client.execute(uri: "/agents/w1%3Ap2/prompt", method: .post, headers: Self.auth, body: ByteBuffer(string: #"{"text":"hi"}"#)) { r throws in
                #expect(r.status == .conflict)
                let err = try decode(ErrorBody.self, r)
                #expect(err.error.code == "agent_blocked")
            }
            try await client.execute(uri: "/agents/w1%3Ap1/prompt", method: .post, headers: Self.auth, body: ByteBuffer(string: "nope")) { r throws in
                #expect(r.status == .badRequest)
                let err = try decode(ErrorBody.self, r)
                #expect(err.error.code == "bad_request")
            }
            try await client.execute(uri: "/agents", method: .post, headers: Self.auth, body: ByteBuffer(string: #"{"workspaceId":"w1","kind":"claude","name":"Bad Name"}"#)) { r throws in
                #expect(r.status == .badRequest)
            }
        }
    }

    @Test func webSocketNeedsTokenAndStreamsEvents() async throws {
        try await withApp { client, _, hub in
            try await client.execute(uri: "/ws", method: .get) { r throws in
                #expect(r.status == .unauthorized)
            }
            let received = Mutex<[String]>([])
            try await client.ws("/ws?token=\(Self.token)") { inbound, _, _ in
                var frames = inbound.messages(maxSize: 1 << 20).makeAsyncIterator()
                if case .text(let s) = try await frames.next() { received.withLock { $0.append(s) } }
                while hub.count == 0 { try await Task.sleep(for: .milliseconds(10)) }
                hub.broadcast(.agentClosed("w1:p1"))
                if case .text(let s) = try await frames.next() { received.withLock { $0.append(s) } }
            }
            // Key order in encoded JSON isn't stable, so compare parsed objects.
            let frames = try received.withLock { $0 }.map {
                try JSONSerialization.jsonObject(with: Data($0.utf8)) as? [String: String]
            }
            #expect(frames == [["type": "hello"], ["type": "agent.closed", "agentId": "w1:p1"]])
        }
    }

    @Test func monitorEmitsCreatedUpdatedClosed() async throws {
        let statuses = Mutex<[String: String]>(["w1:p1": "idle", "w1:p2": "blocked", "w2:p3": "idle"])
        try await withApp(status: { id in statuses.withLock { $0[id] ?? "idle" } }) { _, fake, _ in
            let service = AgentService(herdr: HerdrClient(socketPath: fake.socketPath), locator: TranscriptLocator(claudeProjects: URL(fileURLWithPath: "/nonexistent"), codex: CodexRollouts(root: URL(fileURLWithPath: "/nonexistent"))))
            let hub = EventHub()
            let (sid, events) = hub.subscribe()
            defer { hub.unsubscribe(sid) }
            let monitor = AgentMonitor(service: service, hub: hub, stream: HerdrEventStream(socketPath: fake.socketPath), interval: .seconds(3600))
            await monitor.trigger()  // baseline: no events
            statuses.withLock { $0["w1:p1"] = "working" }
            await monitor.trigger()
            var it = events.makeAsyncIterator()
            guard case .agentUpdated(let a)? = await it.next() else {
                Issue.record("expected agent.updated")
                return
            }
            #expect(a.id == "w1:p1")
            #expect(a.status == .working)
        }
    }
}
