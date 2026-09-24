import Foundation
import RelayKit
import Testing
@testable import Relay

@Suite("Transcript state")
struct TranscriptStateTests {
    private func agent(_ extra: String) throws -> Agent {
        let json = #"{"id":"w9:p1","name":null,"kind":"claude","title":"claude","workspaceId":"w9","workspaceName":"tools","cwdName":"tools","status":"idle","updatedAt":"2026-09-25T10:00:00+00:00""# + extra + "}"
        return try RelayJSON.decoder().decode(Agent.self, from: Data(json.utf8))
    }

    @Test func decodesAndFallsBack() throws {
        #expect(try agent(#","hasTranscript":true,"transcriptState":"pending""#).transcript == .pending)
        #expect(try agent(#","hasTranscript":false,"transcriptState":"unsupported""#).transcript == .unsupported)
        #expect(try agent(#","hasTranscript":true"#).transcript == .ready, "older bridge: from hasTranscript")
        #expect(try agent(#","hasTranscript":false"#).transcript == .pending, "older bridge, claude without a file yet")
        #expect(try agent(#","hasTranscript":true,"transcriptState":"somethingNew""#).transcript == .ready)
    }

    @Test func unknownKindsWithoutTranscriptAreUnsupported() throws {
        let json = #"{"id":"w9:p2","name":null,"kind":"gemini","title":"g","workspaceId":"w9","workspaceName":"tools","cwdName":"tools","status":"idle","hasTranscript":false,"updatedAt":"2026-09-25T10:00:00+00:00"}"#
        #expect(try RelayJSON.decoder().decode(Agent.self, from: Data(json.utf8)).transcript == .unsupported)
        let fixture = try FixtureFiles.decode([Agent].self, "agents.json")
        #expect(fixture.first { $0.id == "w2:p3" }?.transcript == .pending)
    }

    @Test func liveScreenDropsTheFence() {
        #expect(LiveScreenCard.screenText("```\n› hello\n  world\n```") == "› hello\n  world")
        #expect(LiveScreenCard.screenText("no fence") == "no fence")
    }
}

@MainActor
@Suite("Pending bubbles by transcript state")
struct PendingByStateTests {
    /// A brand-new agent (`pending`) will get a transcript that echoes the prompt, so its bubble waits.
    @Test func pendingAgentKeepsBubbleAfterTurn() async throws {
        var agent = try #require(FixtureFiles.decode([Agent].self, "agents.json").first { $0.id == "w2:p1" })
        let backend = RecordingBackend(agent: agent, approval: Approval(agentId: "", question: "", options: []))
        let store = AppStore(backend: backend, hostLabel: "test")
        store.selectedAgentId = agent.id
        await store.refresh()
        agent.hasTranscript = false
        agent.transcriptState = .pending
        agent.status = .idle
        store.apply(.agentUpdated(agent))
        store.send("first", to: agent.id)
        agent.status = .working
        store.apply(.agentUpdated(agent))
        agent.status = .idle
        store.apply(.agentUpdated(agent))
        #expect(store.pending[agent.id]?.count == 1)
    }
}
