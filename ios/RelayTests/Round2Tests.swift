import Foundation
import HerdKit
import Testing
import UIKit
import UniformTypeIdentifiers
@testable import Herd

@Suite("Attachments")
struct AttachmentTests {
    @Test func decodesAttachmentBlocksAndMachine() throws {
        let page = try FixtureFiles.decode(MessagePage.self, "messages-attachments.json")
        #expect(page.messages[0].attachments.map(\.kind) == [.image, .pdf])
        #expect(page.messages[0].plainText == "What's wrong with this layout? The spec is attached.")
        #expect(page.messages[2].attachments == [AttachmentRef(id: "1122334455667788", name: "notes.txt", kind: .file)])
        #expect(page.messages[2].plainText.isEmpty)

        let items = ChatItem.build(from: page.messages)
        guard case .user(_, let text, _, let attachments) = items[0] else { Issue.record("expected a user row"); return }
        #expect(text.hasPrefix("What's wrong"))
        #expect(attachments.count == 2)
        guard case .user(_, let onlyText, _, let only) = items.last else { Issue.record("attachment-only message dropped"); return }
        #expect(onlyText.isEmpty && only.count == 1)

        let attachment = try FixtureFiles.decode(Attachment.self, "attachment.json")
        #expect(attachment.kind == .image && attachment.size == 183422)
        let machine = try FixtureFiles.decode(Machine.self, "machine.json")
        #expect(machine.kind == .laptop && machine.os == "macOS 26.4")
    }

    @Test func imagesAreDownscaledToJPEG() throws {
        let big = UIGraphicsImageRenderer(size: CGSize(width: 4000, height: 3000), format: {
            let f = UIGraphicsImageRendererFormat()
            f.scale = 1
            return f
        }()).pngData { ctx in
            UIColor.systemTeal.setFill()
            ctx.fill(CGRect(x: 0, y: 0, width: 4000, height: 3000))
        }
        let prepared = try AttachmentProcessing.prepare(data: big, name: "IMG_0001.PNG", type: .png)
        #expect(prepared.contentType == "image/jpeg")
        #expect(prepared.name == "IMG_0001.jpg")
        #expect(prepared.kind == .image)
        let image = try #require(UIImage(data: prepared.data))
        #expect(max(image.size.width * image.scale, image.size.height * image.scale) == 2048)

        let small = TestAttachments.herdImage()
        let kept = try #require(UIImage(data: try AttachmentProcessing.prepare(data: small, name: "herd.png", type: nil).data))
        #expect(kept.size.width * kept.scale == 640, "never upscales")
    }

    @Test func otherFilesPassThrough() throws {
        let text = Data("hello".utf8)
        let prepared = try AttachmentProcessing.prepare(data: text, name: "notes.txt", type: nil)
        #expect(prepared.data == text && prepared.kind == .file && prepared.contentType == "text/plain")
        let pdf = try AttachmentProcessing.prepare(data: TestAttachments.codewordPDF(), name: "spec.pdf", type: .pdf)
        #expect(pdf.kind == .pdf && pdf.contentType == "application/pdf")
        #expect(throws: AttachmentProcessing.Failure.tooLarge(name: "big.bin")) {
            try AttachmentProcessing.prepare(data: Data(count: 20 * 1024 * 1024 + 1), name: "big.bin", type: nil)
        }
    }
}

@Suite("Sidebar model")
struct SidebarModelTests {
    /// Fixture agents: w1:p1 working, w1:p2 blocked, w2:p1 idle, w2:p3 done.
    private func model(archived: Set<String> = [], unseen: Set<String> = ["w2:p3"], extraWorkspace: Bool = false) throws -> SidebarModel {
        var state = HerdState()
        state.agents = try FixtureFiles.decode([Agent].self, "agents.json")
        state.workspaces = try FixtureFiles.decode([Workspace].self, "workspaces.json")
        if extraWorkspace { state.workspaces.append(Workspace(id: "w9", name: "empty", agentCount: 0)) }
        return SidebarModel(state: state, archived: archived, isUnseen: { unseen.contains($0.id) })
    }

    @Test func filters() throws {
        let m = try model()
        #expect(m.sessions(.needsInput).map(\.id) == ["w1:p2"])
        #expect(m.sessions(.working).map(\.id) == ["w1:p1"])
        #expect(m.sessions(.readyForReview).map(\.id) == ["w2:p3"])
        #expect(Set(m.sessions(.completed).map(\.id)) == ["w2:p1"], "idle counts as completed; unseen done doesn't")
        #expect(try model(unseen: []).sessions(.completed).map(\.id).contains("w2:p3"), "done and seen is completed")
        #expect(m.sessions(.all).map(\.id) == ["w1:p1", "w1:p2", "w2:p1", "w2:p3"], "newest first")
        #expect(m.needsInputCount == 1)
    }

    @Test func archivedChatsOnlyShowUnderArchived() throws {
        let m = try model(archived: ["w1:p2", "w2:p1"])
        #expect(m.sessions(.archived).map(\.id).sorted() == ["w1:p2", "w2:p1"])
        #expect(!m.sessions(.all).contains { $0.id == "w1:p2" })
        #expect(m.sessions(.needsInput).isEmpty)
        #expect(m.projects().flatMap(\.agents).map(\.id).sorted() == ["w1:p1", "w2:p3"])
        #expect(m.needsInputCount == 0)
    }

    @Test func projectsListEveryFolderWithBadges() throws {
        let projects = try model(extraWorkspace: true).projects()
        #expect(projects.map(\.name) == ["shop-api", "website", "empty"])
        #expect(projects[0].agents.first?.id == "w1:p2", "blocked floats to the top")
        #expect(projects.map(\.needsInput) == [1, 0, 0])
        #expect(projects[2].agents.isEmpty)
    }

    @Test func searchMatchesTitleAndFolder() throws {
        let m = try model()
        #expect(m.sessions(.all, query: "landing").map(\.id) == ["w2:p1"])
        #expect(m.sessions(.all, query: "SHOP").count == 2)
    }
}

@MainActor
@Suite("AppStore round 2")
struct StoreRound2Tests {
    private func store() async throws -> (AppStore, RecordingBackend, [Agent]) {
        AppDefaults.shared.set([String](), forKey: "archivedAgents")
        let agents = try FixtureFiles.decode([Agent].self, "agents.json")
        let backend = RecordingBackend(agent: agents[2], approval: Approval(agentId: "", question: "", options: []))
        let store = AppStore(backend: backend, hostLabel: "test")
        await store.refresh()
        return (store, backend, agents)
    }

    @Test func blockedAgentUnarchivesItself() async throws {
        let (store, _, agents) = try await store()
        var agent = agents[2]
        store.setArchived(agent.id, true)
        #expect(store.isArchived(agent.id))
        agent.status = .working
        store.apply(.agentUpdated(agent))
        #expect(store.isArchived(agent.id), "working doesn't unarchive")
        agent.status = .blocked
        store.apply(.agentUpdated(agent))
        #expect(!store.isArchived(agent.id), "a question brings it back")
        #expect(AppDefaults.shared.stringArray(forKey: "archivedAgents") == [])
    }

    /// Tests run inside the app on the user's phone; they must never write the user's real state.
    @Test func testsUseSeparateDefaults() async throws {
        #expect(AppDefaults.isIsolated)
        let real = PairingStore.sharedDefaults.stringArray(forKey: "archivedAgents")
        let realFilter = UserDefaults.standard.string(forKey: "sessionFilter")
        let (store, _, agents) = try await store()
        store.setArchived(agents[0].id, true)
        store.filter = .archived
        #expect(PairingStore.sharedDefaults.stringArray(forKey: "archivedAgents") == real)
        #expect(UserDefaults.standard.string(forKey: "sessionFilter") == realFilter)
        #expect(AppDefaults.shared.stringArray(forKey: "archivedAgents")?.contains(agents[0].id) == true)
        store.setArchived(agents[0].id, false)
        store.filter = .all
    }

    @Test func machineIsLoaded() async throws {
        let (store, _, _) = try await store()
        #expect(store.machines.map(\.name) == ["Dev's MacBook Pro"])
    }

    @Test func pendingPromptWithAttachmentsResolvesOnSameIds() async throws {
        let (store, backend, agents) = try await store()
        let id = agents[2].id
        let file = HerdKit.Attachment(id: "a1", name: "herd.jpg", kind: .image, size: 10)
        store.send("", attachments: [file], to: id)
        #expect(store.pending[id]?.first?.attachments.map(\.id) == ["a1"])
        try await Task.sleep(for: .milliseconds(100))
        #expect(await backend.calls.contains(.prompt("", attachments: ["a1"])))

        let other = Message(id: "t0", role: .user, createdAt: .now, blocks: [.attachment(AttachmentRef(id: "zz", name: "x.jpg", kind: .image))])
        store.apply(.messageUpserted(agentId: id, message: other))
        #expect(store.pending[id]?.count == 1, "different attachment, still pending")
        let echoed = Message(id: "t1", role: .user, createdAt: .now, blocks: [.attachment(file.ref)])
        store.apply(.messageUpserted(agentId: id, message: echoed))
        #expect(store.pending[id] == nil)
    }
}
