import Foundation
import Testing
@testable import RelayCore

@Suite struct MigrationTests {
    func tempDirs() throws -> (legacy: URL, target: URL, cleanup: () -> Void) {
        let base = URL(fileURLWithPath: "/private/tmp").appendingPathComponent("relay-mig-\(UUID().uuidString.prefix(8))")
        let legacy = base.appendingPathComponent(".herd")
        try FileManager.default.createDirectory(at: legacy.appendingPathComponent("uploads/w1_p1"), withIntermediateDirectories: true)
        try Data("tok\n".utf8).write(to: legacy.appendingPathComponent("token"))
        try Data("x".utf8).write(to: legacy.appendingPathComponent("uploads/w1_p1/0123456789abcdef-a.png"))
        try Data("log".utf8).write(to: legacy.appendingPathComponent("herd.log"))
        try FileManager.default.createDirectory(at: legacy.appendingPathComponent("e2e"), withIntermediateDirectories: true)
        return (legacy, base.appendingPathComponent(".relay"), { try? FileManager.default.removeItem(at: base) })
    }

    @Test func movesTheWholeFolderOnce() throws {
        let (legacy, target, cleanup) = try tempDirs()
        defer { cleanup() }
        let note = RelayHome.migrateIfNeeded(from: legacy, to: target)
        #expect(note?.hasPrefix("moved") == true)
        #expect(try String(contentsOf: target.appendingPathComponent("token"), encoding: .utf8) == "tok\n")
        #expect(FileManager.default.fileExists(atPath: target.appendingPathComponent("uploads/w1_p1/0123456789abcdef-a.png").path))
        #expect(FileManager.default.fileExists(atPath: target.appendingPathComponent("e2e").path))
        #expect(!FileManager.default.fileExists(atPath: legacy.path))
        #expect(RelayHome.migrateIfNeeded(from: legacy, to: target) == nil)
    }

    @Test func mergesIntoAnUnusedRelayDir() throws {
        let (legacy, target, cleanup) = try tempDirs()
        defer { cleanup() }
        // launchd created ~/.relay for its log before the first start.
        try FileManager.default.createDirectory(at: target, withIntermediateDirectories: true)
        try Data("new log".utf8).write(to: target.appendingPathComponent("relay.log"))
        let note = RelayHome.migrateIfNeeded(from: legacy, to: target)
        #expect(note?.contains("token") == true)
        #expect(try String(contentsOf: target.appendingPathComponent("token"), encoding: .utf8) == "tok\n")
        #expect(try String(contentsOf: target.appendingPathComponent("relay.log"), encoding: .utf8) == "new log")
        #expect(!FileManager.default.fileExists(atPath: legacy.path))
    }

    @Test func leavesAnInUseRelayDirAlone() throws {
        let (legacy, target, cleanup) = try tempDirs()
        defer { cleanup() }
        try FileManager.default.createDirectory(at: target, withIntermediateDirectories: true)
        try Data("current\n".utf8).write(to: target.appendingPathComponent("token"))
        #expect(RelayHome.migrateIfNeeded(from: legacy, to: target) == nil)
        #expect(try String(contentsOf: target.appendingPathComponent("token"), encoding: .utf8) == "current\n")
        #expect(FileManager.default.fileExists(atPath: legacy.path))
    }

    @Test func oldUploadMarkersStillResolve() throws {
        let store = UploadStore(root: URL(fileURLWithPath: "/Users/dev/.relay/uploads"), legacyRoot: URL(fileURLWithPath: "/Users/dev/.herd/uploads"))
        let parsed = try #require(store.parseMarker("look\n\nAttached files: /Users/dev/.herd/uploads/w1_p1/0123456789abcdef-a.png"))
        #expect(parsed.attachments.map(\.id) == ["0123456789abcdef"])
    }
}
