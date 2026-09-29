import Foundation
import RelayKit
import Testing
@testable import Relay

private actor UsageBackend: Backend {
    var snapshot = UsageSnapshot(providers: [])
    var refreshError: (any Error)?
    private(set) var refreshCalls = 0

    func workspaces() async throws -> [Workspace] { [] }
    func agents() async throws -> [Agent] { [] }
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
    func sendKeys(agentId: String, keys: [String]) async throws {}
    func sendText(agentId: String, text: String, submit: Bool) async throws {}
    func approval(agentId: String) async throws -> Approval? { nil }
    func createAgent(_ request: CreateAgentRequest) async throws -> Agent {
        Agent(id: "w1:p1", name: nil, kind: "claude", title: "t", workspaceId: "w1", workspaceName: "w", cwdName: "w", status: .idle, hasTranscript: true, updatedAt: .now)
    }
    func controls() async throws -> ControlsCatalog { ControlsCatalog(models: [], modes: [], efforts: []) }
    func kindControls(kind: String) async throws -> AgentControlsInfo {
        AgentControlsInfo(models: [], efforts: [], modes: [], supports: .init(model: false, effort: false, mode: false, compact: false, clear: false))
    }
    func agentControls(agentId: String) async throws -> AgentControlsInfo {
        throw RelayError.http(status: 404, code: "not_found", message: nil)
    }
    func control(agentId: String, _ request: ControlRequest) async throws -> Agent { try await agents()[0] }
    nonisolated func events() -> AsyncStream<ConnectionEvent> { AsyncStream { $0.finish() } }

    var usageError: (any Error)?
    func usage() async throws -> UsageSnapshot {
        if let usageError { throw usageError }
        return snapshot
    }
    func setUsageError(_ e: (any Error)?) { usageError = e }
    func refreshUsage() async throws {
        refreshCalls += 1
        if let refreshError { throw refreshError }
    }

    func setSnapshot(_ s: UsageSnapshot) { snapshot = s }
    func setRefreshError(_ e: (any Error)?) { refreshError = e }
}

@MainActor
@Suite("UsageStore")
struct UsageStoreTests {
    private func provider(id: String = "claude", percent: Double = 10, usedBy: [String] = ["claude"]) -> UsageProvider {
        UsageProvider(id: id, label: id.capitalized, plan: "Max",
                      windows: [UsageWindow(id: "session", label: "Session", usedPercent: percent)],
                      updatedAt: .now, source: "test", stale: false, usedBy: usedBy)
    }

    @Test func loadPopulatesFromTheBackend() async throws {
        let backend = UsageBackend()
        await backend.setSnapshot(UsageSnapshot(providers: [provider()]))
        let store = UsageStore(backend: backend)
        await store.load()
        #expect(store.providers.map(\.id) == ["claude"])
        #expect(store.errorMessage == nil)
    }

    @Test func loadReportsAFriendlyErrorOnFailure() async throws {
        struct Failing: Backend {
            func workspaces() async throws -> [Workspace] { [] }
            func agents() async throws -> [Agent] { [] }
            func messages(agentId: String, before: String?, limit: Int) async throws -> MessagePage { MessagePage(messages: [], hasMore: false) }
            func prompt(agentId: String, text: String, attachments: [String]) async throws {}
            func uploadAttachment(agentId: String, data: Data, filename: String, contentType: String, progress: @escaping @Sendable (Double) -> Void) async throws -> RelayKit.Attachment {
                RelayKit.Attachment(id: "a", name: filename, kind: .file, size: 0)
            }
            func attachmentData(agentId: String, attachmentId: String) async throws -> Data { Data() }
            func machine() async throws -> Machine { Machine(id: "m", name: "m", kind: .laptop) }
            func sendKeys(agentId: String, keys: [String]) async throws {}
            func sendText(agentId: String, text: String, submit: Bool) async throws {}
            func approval(agentId: String) async throws -> Approval? { nil }
            func createAgent(_ request: CreateAgentRequest) async throws -> Agent { throw RelayError.badResponse }
            func controls() async throws -> ControlsCatalog { ControlsCatalog(models: [], modes: [], efforts: []) }
            func kindControls(kind: String) async throws -> AgentControlsInfo { throw RelayError.badResponse }
            func agentControls(agentId: String) async throws -> AgentControlsInfo { throw RelayError.badResponse }
            func control(agentId: String, _ request: ControlRequest) async throws -> Agent { throw RelayError.badResponse }
            func events() -> AsyncStream<ConnectionEvent> { AsyncStream { $0.finish() } }
            func usage() async throws -> UsageSnapshot { throw RelayError.unreachable(timedOut: false) }
            func refreshUsage() async throws { throw RelayError.unreachable(timedOut: false) }
        }
        let store = UsageStore(backend: Failing())
        await store.load()
        #expect(store.providers.isEmpty)
        #expect(store.errorMessage != nil)
    }

    @Test func refreshCallsTheBridgeThenReloads() async throws {
        let backend = UsageBackend()
        await backend.setSnapshot(UsageSnapshot(providers: [provider(percent: 20)]))
        let store = UsageStore(backend: backend)
        await store.refresh()
        #expect(await backend.refreshCalls == 1)
        #expect(store.providers.first?.windows.first?.usedPercent == 20)
        #expect(store.errorMessage == nil)
    }

    @Test func refreshTreats429AsAlreadyFreshNotAnError() async throws {
        let backend = UsageBackend()
        await backend.setRefreshError(RelayError.http(status: 429, code: "rate_limited", message: "try again"))
        await backend.setSnapshot(UsageSnapshot(providers: [provider()]))
        let store = UsageStore(backend: backend)
        await store.refresh()
        #expect(store.errorMessage == nil)
        #expect(store.providers.map(\.id) == ["claude"])
    }

    @Test func applyUpsertsByProviderId() async throws {
        let store = UsageStore(backend: UsageBackend())
        store.apply(provider(id: "claude", percent: 5))
        store.apply(provider(id: "codex", percent: 8))
        store.apply(provider(id: "claude", percent: 11))
        #expect(store.providers.count == 2)
        #expect(store.providers.first { $0.id == "claude" }?.windows.first?.usedPercent == 11)
    }

    @Test func decodesTheContractFixture() throws {
        let snapshot = try FixtureFiles.decode(UsageSnapshot.self, "usage.json")
        #expect(snapshot.providers.map(\.id) == ["claude", "codex", "opencode-go"])
        #expect(snapshot.providers[0].windows.count == 2)
        #expect(snapshot.providers[0].usedBy == ["claude"])
        #expect(snapshot.providers[1].usedBy == ["codex", "pi"])
        let openCodeGo = snapshot.providers[2]
        #expect(openCodeGo.label == "OpenCode Go")
        #expect(openCodeGo.windows.isEmpty)
        #expect(openCodeGo.stale == false)
        #expect(openCodeGo.unavailableReason == "Usage not available from OpenCode")
        #expect(openCodeGo.usedBy == ["pi"])
    }

    @Test func usedByDefaultsToEmptyWhenAbsentFromJSON() throws {
        // A bridge that predates `usedBy` still decodes.
        let json = """
        {"id":"claude","label":"Claude","windows":[],"updatedAt":"2026-01-01T00:00:00Z","source":"test","stale":false}
        """
        let decoded = try RelayJSON.decoder().decode(UsageProvider.self, from: Data(json.utf8))
        #expect(decoded.usedBy == [])
    }
}

@MainActor
@Suite("Usage across machines")
struct UsageMachinesTests {
    private let resets = Date(timeIntervalSince1970: 2_000_000)

    private func provider(
        _ id: String = "claude", plan: String? = "Max", percent: Double? = 10, resetsAt: Date? = nil,
        updatedAt: Date = Date(timeIntervalSince1970: 1_000_000), usedBy: [String] = ["claude"]
    ) -> UsageProvider {
        UsageProvider(id: id, label: id.capitalized, plan: plan,
                      windows: [UsageWindow(id: "session", label: "Session", usedPercent: percent, resetsAt: resetsAt ?? resets)],
                      updatedAt: updatedAt, source: "test", stale: false, usedBy: usedBy)
    }

    private let machines: [(id: String, name: String)] = [("mac", "Mac"), ("vm", "Test VM")]

    @Test func sameProviderPlanAndNumbersMergeIntoOneCard() {
        let cards = UsageCard.merge(machines, [
            "mac": [provider(percent: 40, usedBy: ["claude"])],
            "vm": [provider(percent: 41, updatedAt: Date(timeIntervalSince1970: 1_000_060), usedBy: ["claude", "pi"])],
        ])
        #expect(cards.count == 1)
        #expect(cards[0].machineNames == ["Mac", "Test VM"])
        // The freshest snapshot wins; usedBy is the union.
        #expect(cards[0].provider.windows[0].usedPercent == 41)
        #expect(cards[0].provider.usedBy == ["claude", "pi"])
    }

    @Test func samePlanButDifferentUseStaysPerMachine() {
        let cards = UsageCard.merge(machines, [
            "mac": [provider(percent: 40)],
            "vm": [provider(percent: 5)],
        ])
        #expect(cards.map(\.machineNames) == [["Mac"], ["Test VM"]])
    }

    @Test func differentResetTimeStaysPerMachine() {
        let cards = UsageCard.merge(machines, [
            "mac": [provider(percent: 40)],
            "vm": [provider(percent: 40, resetsAt: resets.addingTimeInterval(3600))],
        ])
        #expect(cards.count == 2)
    }

    @Test func differentOrMissingPlanStaysPerMachine() {
        #expect(UsageCard.merge(machines, ["mac": [provider(plan: "Max")], "vm": [provider(plan: "Pro")]]).count == 2)
        #expect(UsageCard.merge(machines, ["mac": [provider(plan: nil)], "vm": [provider(plan: nil)]]).count == 2)
    }

    @Test func differentProvidersNeverMerge() {
        let cards = UsageCard.merge(machines, ["mac": [provider("claude")], "vm": [provider("codex")]])
        #expect(cards.map(\.provider.id) == ["claude", "codex"])
    }

    @Test func storeKeepsEachMachineApartAndRemovesCleanly() async {
        let mac = UsageBackend()
        let vm = UsageBackend()
        await mac.setSnapshot(UsageSnapshot(providers: [provider(percent: 40)]))
        await vm.setSnapshot(UsageSnapshot(providers: [provider(percent: 40), provider("codex", plan: "Plus")]))
        let store = UsageStore()
        store.setMachines([("mac", "Mac"), ("vm", "Test VM")])
        await store.load(machineId: "vm", backend: vm)
        await store.load(machineId: "mac", backend: mac)
        #expect(store.showsMachines)
        #expect(store.cards.map(\.machineNames) == [["Mac", "Test VM"], ["Test VM"]])

        store.apply(provider(percent: 90), machineId: "vm")
        #expect(store.cards.count == 3)

        store.setMachines([("mac", "Mac")])
        #expect(!store.showsMachines)
        #expect(store.cards.map(\.machineIds) == [["mac"]])
        store.apply(provider("codex"), machineId: "vm")
        #expect(store.cards.count == 1)
    }

    @Test func oneMachineFailingDoesNotHideTheOther() async {
        let mac = UsageBackend()
        await mac.setSnapshot(UsageSnapshot(providers: [provider()]))
        let vm = UsageBackend()
        await vm.setUsageError(RelayError.unreachable(timedOut: true))
        let store = UsageStore()
        store.setMachines([("mac", "Mac"), ("vm", "Test VM")])
        await store.load(machineId: "mac", backend: mac)
        await store.load(machineId: "vm", backend: vm)
        // Pull-to-refresh reaches both through the backends they were loaded with.
        await store.refresh()
        #expect(await mac.refreshCalls == 1)
        #expect(await vm.refreshCalls == 1)
        #expect(store.cards.map(\.machineIds) == [["mac"]])
        #expect(store.errors["vm"] != nil)
        #expect(store.errors["mac"] == nil)
    }
}

@Suite("Usage bar level and countdown")
struct UsageLevelTests {
    @Test func neutralBelow75() {
        #expect(UsageLevel.classify(0) == .neutral)
        #expect(UsageLevel.classify(74.9) == .neutral)
    }

    @Test func amberFrom75Below90() {
        #expect(UsageLevel.classify(75) == .amber)
        #expect(UsageLevel.classify(89.9) == .amber)
    }

    @Test func redFrom90() {
        #expect(UsageLevel.classify(90) == .red)
        #expect(UsageLevel.classify(100) == .red)
    }

    @Test func countdownBuckets() {
        let now = Date(timeIntervalSince1970: 1_000_000)
        #expect(UsageWindowRow.countdown(to: now.addingTimeInterval(30), now: now) == "soon")
        #expect(UsageWindowRow.countdown(to: now.addingTimeInterval(-30), now: now) == "soon")
        #expect(UsageWindowRow.countdown(to: now.addingTimeInterval(90), now: now) == "in 1m")
        #expect(UsageWindowRow.countdown(to: now.addingTimeInterval(2 * 3600), now: now) == "in 2h")
        #expect(UsageWindowRow.countdown(to: now.addingTimeInterval(3 * 86_400), now: now) == "in 3d")
    }
}
