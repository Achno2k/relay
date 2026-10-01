import Foundation
import RelayKit
import Testing
@testable import Relay

/// Round 12: images in tool results (api.md "Tool result images").
@Suite("Tool result images")
@MainActor
struct ToolImageTests {
    // MARK: - Contract

    @Test func fixtureDecodesImages() throws {
        let page = try FixtureFiles.decode(MessagePage.self, "messages-images.json")
        let results = page.messages.flatMap(\.blocks).compactMap { block -> ToolResult? in
            if case .toolResult(let r) = block { r } else { nil }
        }
        #expect(results.count == 2)
        #expect(results[0].images == [ToolImage(index: 0, mediaType: "image/png", bytes: 73, width: 3, height: 2)])
        #expect(results[1].images.map(\.index) == [0, 1])
        // width/height are optional: a webp the bridge couldn't size has no aspect.
        #expect(results[1].images[1].mediaType == "image/webp")
        #expect(results[1].images[1].aspectRatio == nil)
        #expect(results[0].images[0].aspectRatio == 1.5)
    }

    /// An older bridge sends no `images`; the result decodes exactly as before.
    @Test func resultWithoutImagesDecodesEmpty() throws {
        let json = #"{"type":"toolResult","toolCallId":"t1","isError":false,"preview":"[image]"}"#
        let block = try RelayJSON.decoder().decode(Block.self, from: Data(json.utf8))
        #expect(block == .toolResult(ToolResult(toolCallId: "t1", isError: false, preview: "[image]")))
    }

    @Test func imagesRoundTripAndStayOutWhenEmpty() throws {
        let withImages = Block.toolResult(ToolResult(
            toolCallId: "t1", isError: false, preview: "[image]",
            images: [ToolImage(index: 0, mediaType: "image/jpeg", bytes: 10, width: 4, height: 3)]
        ))
        let data = try RelayJSON.encoder().encode(withImages)
        #expect(try RelayJSON.decoder().decode(Block.self, from: data) == withImages)

        let plain = try RelayJSON.encoder().encode(Block.toolResult(ToolResult(toolCallId: "t2", isError: false)))
        #expect(!String(decoding: plain, as: UTF8.self).contains("images"))
    }

    // MARK: - Chat items

    @Test func stepsCarryImagesAndDropPlaceholderLines() throws {
        let page = try FixtureFiles.decode(MessagePage.self, "messages-images.json")
        let steps = ChatItem.build(from: page.messages).flatMap { item -> [ToolStep] in
            if case .tools(_, let steps, _) = item { steps } else { [] }
        }
        #expect(steps.map(\.images.count) == [1, 2])
        #expect(steps[0].preview == "[image]")
        #expect(steps[0].textPreview == nil)
        #expect(steps[1].textPreview == "Captured 2 frames")
    }

    // MARK: - API

    private func client(_ base: String) -> APIClient {
        APIClient(baseURL: URL(string: base)!, token: "t", session: StubURLProtocol.session())
    }

    @Test func clientFetchesRawBytes() async throws {
        let api = client("http://tool-image.test")
        let bytes = Data([0x89, 0x50, 0x4E, 0x47])
        StubURLProtocol.stub(
            URL(string: "http://tool-image.test/agents/w1%3Ap1/tool-images/toolu_01%2Fx/1")!,
            .init(data: bytes, contentType: "image/png")
        )
        #expect(try await api.toolImage(agentId: "w1:p1", toolCallId: "toolu_01/x", index: 1) == bytes)
    }

    @Test func clientMapsMissingAndTooLarge() async throws {
        let api = client("http://tool-image-err.test")
        StubURLProtocol.stub(
            URL(string: "http://tool-image-err.test/agents/w1%3Ap1/tool-images/t1/0")!,
            .init(data: Data(#"{"error":{"code":"not_found","message":"no image"}}"#.utf8), statusCode: 404)
        )
        StubURLProtocol.stub(
            URL(string: "http://tool-image-err.test/agents/w1%3Ap1/tool-images/t1/1")!,
            .init(data: Data(#"{"error":{"code":"too_large","message":"over 20 MB"}}"#.utf8), statusCode: 413)
        )
        await #expect(throws: RelayError.http(status: 404, code: "not_found", message: "no image")) {
            try await api.toolImage(agentId: "w1:p1", toolCallId: "t1", index: 0)
        }
        await #expect(throws: RelayError.http(status: 413, code: "too_large", message: "over 20 MB")) {
            try await api.toolImage(agentId: "w1:p1", toolCallId: "t1", index: 1)
        }
    }

    // MARK: - Store

    private func machine(_ id: String) -> (FakeBridge, MachineConnection) {
        let bridge = FakeBridge(
            id: id, agents: [MachinesTests.agent("w1:p1", ws: "w1", title: id)], workspaces: [Workspace(id: "w1", name: "proj", agentCount: 1)]
        )
        return (bridge, MachineConnection(record: MachineRecord(id: id, url: URL(string: "http://\(id):7878")!), raw: bridge))
    }

    /// Goes to the agent's own machine with the raw id, and the second look is a cache hit.
    @Test func storeRoutesByMachineAndCaches() async throws {
        AppDefaults.resetTestState()
        let (mac, macConnection) = machine("mac")
        let (vm, vmConnection) = machine("vm")
        let store = AppStore(connections: [macConnection, vmConnection], pairingStore: nil, makeBackend: { _ in mac })

        let first = try await store.toolImageData(agentId: "vm/w1:p1", toolCallId: "toolu_1", index: 0)
        #expect(String(decoding: first, as: UTF8.self) == "vm|w1:p1|toolu_1|0")
        let again = try await store.toolImageData(agentId: "vm/w1:p1", toolCallId: "toolu_1", index: 0)
        #expect(again == first)
        #expect(await vm.toolImageFetches == ["w1:p1|toolu_1|0"])
        #expect(await mac.toolImageFetches.isEmpty)

        // Same call id on another machine is another image.
        let other = try await store.toolImageData(agentId: "mac/w1:p1", toolCallId: "toolu_1", index: 0)
        #expect(String(decoding: other, as: UTF8.self) == "mac|w1:p1|toolu_1|0")
    }

    /// A bridge without the endpoint answers 404; the row falls back to its preview on that.
    @Test func olderBridgeThrowsNotFound() async throws {
        let backend = RecordingBackend(agent: MachinesTests.agent("w1:p1", ws: "w1", title: "t"), approval: try FixtureFiles.decode(Approval.self, "approval.json"))
        let store = AppStore(backend: backend, hostLabel: "test")
        await #expect(throws: RelayError.http(status: 404, code: "not_found", message: nil)) {
            try await store.toolImageData(agentId: "w1:p1", toolCallId: "t1", index: 0)
        }
    }

    @Test func cacheKeysByEndpointPath() {
        let cache = ToolImageCache()
        let key = ToolImageCache.key(agentId: "vm/w1:p1", toolCallId: "t1", index: 2)
        #expect(key == "vm/w1:p1/tool-images/t1/2")
        #expect(cache[key] == nil)
        cache[key] = Data([1, 2, 3])
        #expect(cache[key] == Data([1, 2, 3]))
        #expect(cache[ToolImageCache.key(agentId: "vm/w1:p1", toolCallId: "t1", index: 1)] == nil)
    }

    // MARK: - Mock

    @Test func mockServesItsToolImages() async throws {
        let mock = MockBackend(replayInterval: nil)
        let hero = try await mock.toolImage(agentId: "w2:p1", toolCallId: "l-t3", index: 0)
        #expect(hero.starts(with: [0x89, 0x50, 0x4E, 0x47]))
        _ = try await mock.toolImage(agentId: "w2:p1", toolCallId: "l-t5", index: 1)
        await #expect(throws: RelayError.self) { try await mock.toolImage(agentId: "w2:p1", toolCallId: "l-t5", index: 2) }
    }
}
