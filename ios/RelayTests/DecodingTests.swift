import Foundation
import HerdKit
import Testing
@testable import Herd

/// Reads `docs/fixtures` straight from the repo so the tests track the contract.
enum FixtureFiles {
    static let directory = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent()
        .deletingLastPathComponent()
        .deletingLastPathComponent()
        .appending(path: "docs/fixtures")

    static func data(_ name: String) throws -> Data {
        try Data(contentsOf: directory.appending(path: name))
    }

    static func decode<T: Decodable>(_ type: T.Type, _ name: String) throws -> T {
        try HerdJSON.decoder().decode(T.self, from: data(name))
    }

    static func events() throws -> [ServerEvent] {
        let text = try String(contentsOf: directory.appending(path: "ws-events.jsonl"), encoding: .utf8)
        return try text.split(separator: "\n").map { try HerdJSON.decoder().decode(ServerEvent.self, from: Data($0.utf8)) }
    }
}

@Suite("Decoding fixtures")
struct DecodingTests {
    @Test func agents() throws {
        let agents = try FixtureFiles.decode([Agent].self, "agents.json")
        #expect(agents.count == 4)
        let first = try #require(agents.first)
        #expect(first.id == "w1:p1")
        #expect(first.status == .working)
        #expect(first.workspaceName == "shop-api")
        #expect(first.updatedAt == HerdJSON.date(from: "2026-09-23T13:04:01Z"))
        #expect(agents[1].status == .blocked)
        #expect(agents[3].name == nil)
        #expect(agents[3].hasTranscript == false)
        #expect(agents[3].displayTitle == "Codex")
    }

    @Test func workspaces() throws {
        let workspaces = try FixtureFiles.decode([Workspace].self, "workspaces.json")
        #expect(workspaces.map(\.name) == ["shop-api", "website"])
        #expect(workspaces.allSatisfy { $0.agentCount == 2 })
    }

    @Test func messages() throws {
        let page = try FixtureFiles.decode(MessagePage.self, "messages.json")
        #expect(page.hasMore == false)
        #expect(page.messages.map(\.role) == [.user, .assistant, .user, .assistant])
        let blocks = page.messages[1].blocks
        #expect(blocks.count == 8)
        #expect(blocks[0] == .thinking("Need to find where the token is read first."))
        guard case .toolCall(let call) = blocks[1] else { Issue.record("expected toolCall"); return }
        #expect(call.name == "Grep")
        #expect(call.summary == "Searched for read_token")
        guard case .toolResult(let result) = blocks[6] else { Issue.record("expected toolResult"); return }
        #expect(result.toolCallId == "t3")
        #expect(result.isError)
    }

    @Test func approval() throws {
        let approval = try FixtureFiles.decode(Approval.self, "approval.json")
        #expect(approval.agentId == "w1:p2")
        #expect(approval.options.map(\.keys) == [["1"], ["2"], ["esc"]])
    }

    @Test func events() throws {
        let events = try FixtureFiles.events()
        #expect(events.count == 4)
        #expect(events[0] == .hello)
        guard case .agentUpdated(let agent) = events[1] else { Issue.record("expected agent.updated"); return }
        #expect(agent.status == .working)
        guard case .messageUpserted(let agentId, let message) = events[2] else { Issue.record("expected message.upserted"); return }
        #expect(agentId == "w1:p1")
        #expect(message.blocks.count == 2)
        #expect(events[3] == .agentClosed(agentId: "w2:p3"))
    }

    @Test func approvalStepAndFreeText() throws {
        let json = #"""
        {"agentId":"w14:p2","question":"Focus?","step":{"index":2,"count":3,"title":"Focus"},
         "options":[{"label":"Deep work","keys":["1"]},{"label":"Type something.","keys":["3"]},{"label":"Other","keys":["4"],"freeText":true}]}
        """#
        let approval = try HerdJSON.decoder().decode(Approval.self, from: Data(json.utf8))
        #expect(approval.step == ApprovalStep(index: 2, count: 3, title: "Focus"))
        #expect(approval.options.map(\.isFreeText) == [false, true, true])
        let fixture = try FixtureFiles.decode(Approval.self, "approval.json")
        #expect(fixture.step == nil)
        #expect(fixture.options.allSatisfy { !$0.isFreeText })
    }

    @Test func unknownValuesDontBreakDecoding() throws {
        let json = #"""
        {"id":"m","role":"assistant","createdAt":"2026-09-23T13:00:00.250+02:00","blocks":[{"type":"image","url":"x"},{"type":"text","text":"hi"}]}
        """#
        let message = try HerdJSON.decoder().decode(Message.self, from: Data(json.utf8))
        #expect(message.blocks == [.unknown(type: "image"), .text("hi")])
        let agent = #"{"id":"a","name":null,"kind":"pi","title":"","workspaceId":"w","workspaceName":"w","cwdName":"w","status":"sleeping","hasTranscript":false,"updatedAt":"2026-09-23T13:00:00+00:00"}"#
        #expect(try HerdJSON.decoder().decode(Agent.self, from: Data(agent.utf8)).status == .unknown)
        let event = try HerdJSON.decoder().decode(ServerEvent.self, from: Data(#"{"type":"agent.renamed"}"#.utf8))
        #expect(event == .unknown(type: "agent.renamed"))
    }

    @Test func blockRoundTrip() throws {
        let page = try FixtureFiles.decode(MessagePage.self, "messages.json")
        let data = try HerdJSON.encoder().encode(page)
        #expect(try HerdJSON.decoder().decode(MessagePage.self, from: data) == page)
    }
}

@Suite("Pairing")
struct PairingTests {
    @Test func parsesLink() throws {
        let pairing = try #require(Pairing(linkString: "herd://pair?url=http%3A%2F%2F100.64.0.1%3A7878&token=abc123"))
        #expect(pairing.url.absoluteString == "http://100.64.0.1:7878")
        #expect(pairing.token == "abc123")
    }

    @Test func rejectsOtherLinks() {
        #expect(Pairing(linkString: "https://pair?url=http://x&token=a") == nil)
        #expect(Pairing(linkString: "herd://pair?url=http://x") == nil)
        #expect(Pairing(linkString: "herd://pair?url=ftp://x&token=a") == nil)
    }

    @Test func baseURLAddsScheme() {
        #expect(Pairing.baseURL(from: "100.64.0.1:7878")?.absoluteString == "http://100.64.0.1:7878")
        #expect(Pairing.baseURL(from: "  ") == nil)
    }

    @Test func restURLs() {
        let client = APIClient(baseURL: URL(string: "http://100.64.0.1:7878")!, token: "t")
        let url = client.url("/agents/\(APIClient.encode("w13:p1"))/messages", query: [URLQueryItem(name: "limit", value: "50")])
        #expect(url.absoluteString == "http://100.64.0.1:7878/agents/w13%3Ap1/messages?limit=50")
    }

    @Test func socketURL() {
        let ws = WSClient(baseURL: URL(string: "http://100.64.0.1:7878")!, token: "t o")
        #expect(ws.url.absoluteString == "ws://100.64.0.1:7878/ws?token=t%20o")
        #expect(WSClient.backoff(attempt: 0) == .seconds(1))
        #expect(WSClient.backoff(attempt: 3) == .seconds(8))
        #expect(WSClient.backoff(attempt: 9) == .seconds(30))
    }
}
