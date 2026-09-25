import Foundation
import Testing
@testable import RelayCore

@Suite struct UploadsTests {
    func tempStore() -> UploadStore {
        UploadStore(root: URL(fileURLWithPath: "/tmp").appendingPathComponent("relay-uploads-\(UUID().uuidString.prefix(8))"))
    }

    @Test(arguments: [
        ("photo.jpg", "photo.jpg"),
        ("My Screen Shot 2026-09-24 at 10.00.00.png", "My-Screen-Shot-2026-09-24-at-10.00.00.png"),
        ("../../etc/passwd", "etc-passwd"),
        ("a/b.txt", "a-b.txt"),
        ("dir/sub/file.png", "dir-sub-file.png"),
        ("a\\b.txt", "a-b.txt"),
        ("/", "file"),
        ("../..", "file"),
        ("x/../../y.txt", "x-..-..-y.txt"),
        ("résumé ✓.pdf", "r-sum.pdf"),
        ("", "file"),
        ("✓✓✓", "file"),
        (".hidden", "hidden"),
        (String(repeating: "a", count: 200) + ".txt", String(repeating: "a", count: 80) + ".txt"),
    ])
    func sanitize(raw: String, expected: String) {
        #expect(UploadStore.sanitize(raw) == expected)
    }

    /// QA-2: `/` is sanitised like any other character, and the stored file still lands directly
    /// inside the agent's upload folder.
    @Test(arguments: ["a/b.txt", "../../../../tmp/relay_traversal_marker.txt", "/etc/passwd", "../..", "..\\..\\x"])
    func slashNamesStayInsideTheUploadFolder(raw: String) throws {
        let store = tempStore()
        defer { try? FileManager.default.removeItem(at: store.root) }
        let a = try store.save(paneId: "w1:p1", data: Data("x".utf8), filename: raw)
        let url = try #require(store.find(a.id))
        #expect(url.deletingLastPathComponent().standardizedFileURL.path
                == store.root.appendingPathComponent(UploadStore.paneDir("w1:p1")).standardizedFileURL.path)
        #expect(url.lastPathComponent == "\(a.id)-\(a.name)")
        #expect(!a.name.contains("/"))
    }

    @Test func kinds() {
        #expect(AttachmentKind.of(name: "a.jpg") == .image)
        #expect(AttachmentKind.of(name: "a.HEIC") == .image)
        #expect(AttachmentKind.of(name: "a.pdf") == .pdf)
        #expect(AttachmentKind.of(name: "a.swift") == .file)
        #expect(AttachmentKind.of(name: "Makefile") == .file)
    }

    @Test func saveFindAndPermissions() throws {
        let store = tempStore()
        defer { try? FileManager.default.removeItem(at: store.root) }
        let a = try store.save(paneId: "w14:p2", data: Data("hi".utf8), filename: "notes.txt")
        #expect(a.id.count == 16)
        #expect(a.kind == .file)
        let url = try #require(store.find(a.id))
        #expect(url.lastPathComponent == "\(a.id)-notes.txt")
        #expect(url.deletingLastPathComponent().lastPathComponent == "w14_p2")
        let perms = try FileManager.default.attributesOfItem(atPath: url.path)[.posixPermissions] as? NSNumber
        #expect(perms?.intValue == 0o600)
        #expect(store.find("../../etc/passwd") == nil)
        #expect(store.find("0123456789abcdef") == nil)
    }

    @Test func cleanupRemovesOldFiles() throws {
        let store = tempStore()
        defer { try? FileManager.default.removeItem(at: store.root) }
        let old = try store.save(paneId: "w1:p1", data: Data("o".utf8), filename: "old.txt")
        let new = try store.save(paneId: "w1:p2", data: Data("n".utf8), filename: "new.txt")
        let oldURL = try #require(store.find(old.id))
        try FileManager.default.setAttributes([.modificationDate: Date().addingTimeInterval(-8 * 24 * 3600)], ofItemAtPath: oldURL.path)
        #expect(store.cleanup() == 1)
        #expect(store.find(old.id) == nil)
        #expect(store.find(new.id) != nil)
        #expect(!FileManager.default.fileExists(atPath: oldURL.deletingLastPathComponent().path))
    }

    @Test func markerRoundTrip() throws {
        let store = tempStore()
        defer { try? FileManager.default.removeItem(at: store.root) }
        let img = try store.save(paneId: "w1:p1", data: Data([1]), filename: "shot.png")
        let pdf = try store.save(paneId: "w1:p1", data: Data([2]), filename: "spec.pdf")
        let text = UploadStore.prompt(text: "Look\nat these", paths: [store.find(img.id)!, store.find(pdf.id)!])
        let parsed = try #require(store.parseMarker(text))
        #expect(parsed.text == "Look\nat these")
        #expect(parsed.attachments.map(\.id) == [img.id, pdf.id])
        #expect(parsed.attachments.map(\.kind) == [.image, .pdf])
        #expect(store.parseMarker(UploadStore.prompt(text: "", paths: [store.find(img.id)!]))?.text == "")
    }

    @Test func markerIgnoresOtherPaths() {
        let store = UploadStore(root: URL(fileURLWithPath: "/Users/dev/.relay/uploads"))
        #expect(store.parseMarker("see\n\nAttached files: /etc/passwd") == nil)
        #expect(store.parseMarker("Attached files: /Users/dev/.relay/uploads/w1_p1/nothex-a.png") == nil)
        #expect(store.parseMarker("Attached files: are great") == nil)
        #expect(store.parseMarker("no marker") == nil)
    }

    @Test func transcriptTurnsMarkerIntoBlocks() throws {
        let store = UploadStore(root: URL(fileURLWithPath: "/Users/dev/.relay/uploads"))
        let text = "What does it say?\\n\\nAttached files: /Users/dev/.relay/uploads/w1_p1/9f2c4e1a7b3d5f60-shot.png /Users/dev/.relay/uploads/w1_p1/0a1b2c3d4e5f6071-spec.pdf"
        let only = "Attached files: /Users/dev/.relay/uploads/w1_p1/1122334455667788-notes.txt"
        let lines = [
            #"{"type":"user","uuid":"u1","timestamp":"2026-09-24T12:00:00Z","message":{"content":"\#(text)"}}"#,
            #"{"type":"user","uuid":"u2","timestamp":"2026-09-24T12:01:00Z","message":{"content":"\#(only)"}}"#,
        ].joined(separator: "\n")
        let ms = TranscriptParser.parse(Data(lines.utf8), format: .claude, cwd: "/Users/dev/shop", uploads: store)
        #expect(ms[0].blocks == [
            .attachment(id: "9f2c4e1a7b3d5f60", name: "shot.png", kind: .image),
            .attachment(id: "0a1b2c3d4e5f6071", name: "spec.pdf", kind: .pdf),
            .text("What does it say?"),
        ])
        #expect(ms[1].blocks == [.attachment(id: "1122334455667788", name: "notes.txt", kind: .file)])
        let json = String(decoding: try JSONEncoder().encode(ms), as: UTF8.self)
        #expect(!json.contains("/Users/"))
    }

    @Test func contractFixturesDecode() throws {
        let docs = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
            .appendingPathComponent("docs/fixtures")
        let page = try JSONDecoder().decode(MessagePage.self, from: Data(contentsOf: docs.appendingPathComponent("messages-attachments.json")))
        #expect(page.messages[0].blocks.first == .attachment(id: "9f2c4e1a7b3d5f60", name: "screenshot.jpg", kind: .image))
        _ = try JSONDecoder().decode(RelayCore.Attachment.self, from: Data(contentsOf: docs.appendingPathComponent("attachment.json")))
        let m = try JSONDecoder().decode(Machine.self, from: Data(contentsOf: docs.appendingPathComponent("machine.json")))
        #expect(m.kind == "laptop")
    }

    @Test func machineKind() {
        #expect(Machine.kind(forModel: "MacBookPro18,3") == "laptop")
        #expect(Machine.kind(forModel: "Mac14,13") == "desktop")
        #expect(Machine.kind(forModel: "Mac17,2", hasBattery: true) == "laptop")
    }
}

@Suite struct SentLogTests {
    /// The user message exactly as Claude Code 2.1.280 stored it: image path swapped for
    /// `[Image #2]`, paste wrapped in tags, long lines hard-wrapped.
    static func rewritten(pdfPath: String) -> String {
        "[Image #2]\n\n<pasted_content id=\"9aae\">\nWhat is the secret code word in the PDF, and what word is in the image? Reply as: CODE\n/ WORD\nAttached files:\n\(pdfPath)\n</pasted_content id=\"9aae\">\n"
    }

    @Test func matchesRewrittenPromptToWhatWasSent() throws {
        let store = UploadStore(root: URL(fileURLWithPath: "/tmp").appendingPathComponent("relay-sent-\(UUID().uuidString.prefix(8))"))
        defer { try? FileManager.default.removeItem(at: store.root) }
        let pdf = try store.save(paneId: "w14:p2", data: Data([1]), filename: "codeword.pdf")
        let img = try store.save(paneId: "w14:p2", data: Data([2]), filename: "relay.png")
        let text = "What is the secret code word in the PDF, and what word is in the image? Reply as: CODE / WORD"
        let sentAt = Timestamps.parse("2026-09-24T12:00:00Z")!
        store.recordSent(pane: "w14:p2", text: text, files: [store.find(pdf.id)!, store.find(img.id)!], at: sentAt)

        let content = Self.rewritten(pdfPath: store.find(pdf.id)!.path)
        let line = try JSONSerialization.data(withJSONObject: [
            "type": "user", "uuid": "u1", "timestamp": "2026-09-24T12:00:01.500Z",
            "message": ["role": "user", "content": [["type": "text", "text": content], ["type": "image", "source": ["type": "base64"]]]],
        ])
        let ms = TranscriptParser.parse(line, format: .claude, cwd: "/Users/dev/e2e", uploads: store)
        #expect(ms.first?.blocks == [
            .attachment(id: pdf.id, name: "codeword.pdf", kind: .pdf),
            .attachment(id: img.id, name: "relay.png", kind: .image),
            .text(text),
        ])
    }

    @Test func noRecordFallsBackToCleanText() {
        let store = UploadStore(root: URL(fileURLWithPath: "/tmp/relay-none-\(UUID().uuidString.prefix(8))"))
        let content = "<pasted_content id=\"ab12\">\nline one\nline two\n</pasted_content id=\"ab12\">\n"
        let line = #"{"type":"user","uuid":"u1","timestamp":"2026-09-24T12:00:00Z","message":{"content":\#(String(decoding: try! JSONSerialization.data(withJSONObject: [content]), as: UTF8.self).dropFirst().dropLast())}}"#
        let ms = TranscriptParser.parse(Data(line.utf8), format: .claude, cwd: nil, uploads: store)
        #expect(ms.first?.blocks == [.text("line one\nline two")])
    }

    @Test func oldRecordsDontMatch() throws {
        let store = UploadStore(root: URL(fileURLWithPath: "/tmp").appendingPathComponent("relay-sent-\(UUID().uuidString.prefix(8))"))
        defer { try? FileManager.default.removeItem(at: store.root) }
        let img = try store.save(paneId: "w1:p1", data: Data([2]), filename: "a.png")
        store.recordSent(pane: "w1:p1", text: "look", files: [store.find(img.id)!], at: Timestamps.parse("2026-09-01T00:00:00Z")!)
        #expect(store.lookupSent("[Image #1]look\n\nAttached files:", at: Timestamps.parse("2026-09-24T00:00:00Z")) == nil)
        #expect(store.lookupSent("[Image #1]look\n\nAttached files:", at: Timestamps.parse("2026-09-01T00:00:02Z"))?.attachments.first?.id == img.id)
        // Pruned with the uploads.
        store.cleanup(now: Timestamps.parse("2026-09-24T00:00:00Z")!)
        #expect(store.lookupSent("[Image #1]look", at: nil) == nil)
    }

    @Test func fingerprintIgnoresClaudeRewrites() {
        #expect(UploadStore.fingerprint(Self.rewritten(pdfPath: "/x/y.pdf"))
            == UploadStore.fingerprint("What is the secret code word in the PDF, and what word is in the image? Reply as: CODE / WORD"))
    }
}
