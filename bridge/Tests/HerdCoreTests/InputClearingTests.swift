import Foundation
import Synchronization
import Testing
@testable import HerdCore

/// A fake Claude whose input box refills after Esc (like Claude Code) and empties on ctrl+u.
@Suite(.serialized) struct InputClearingTests {
    final class Screen: Sendable {
        let box = Mutex<String>("")
        let status = Mutex<String>("idle")
        let keys = Mutex<[[String]]>([])
        let texts = Mutex<[String]>([])
        let prompts = Mutex<[String]>([])
    }

    func withService(_ screen: Screen, _ body: (AgentService) async throws -> Void) async throws {
        let fake = try FakeHerdr { method, params in
            switch method {
            case "agent.get":
                return ["type": "agent_info", "agent": World.agent("w14:p2", name: "e2e", status: screen.status.withLock { $0 }, cwd: "/Users/dev/e2e", session: nil)]
            case "agent.read":
                let box = screen.box.withLock { $0 }
                let rule = String(repeating: "─", count: 40)
                let text = "⎿  Interrupted\n\(rule)\n❯ \(box.replacingOccurrences(of: "\n", with: "\n  "))\n\(rule)\n  status"
                return ["type": "pane_read", "read": ["pane_id": "w14:p2", "workspace_id": "w14", "tab_id": "w14:t1", "source": "detection", "format": "text", "text": text, "revision": 1, "truncated": false]]
            case "agent.send_keys":
                let keys = params["keys"] as? [String] ?? []
                screen.keys.withLock { $0.append(keys) }
                if keys == ["esc"] { screen.box.withLock { $0 = "first prompt\nsecond line" } }
                if keys.contains("ctrl+u") { screen.box.withLock { $0 = "" } }
                return ["type": "ok"]
            case "agent.prompt":
                screen.prompts.withLock { $0.append(params["text"] as? String ?? "") }
                return ["type": "agent_prompted", "agent": [:] as [String: Any]]
            case "pane.send_text":
                screen.texts.withLock { $0.append(params["text"] as? String ?? "") }
                return ["type": "ok"]
            default:
                return FakeError(code: "unknown_method", message: method)
            }
        }
        defer { fake.stop() }
        try await body(AgentService(herdr: HerdrClient(socketPath: fake.socketPath), locator: TranscriptLocator(claudeProjects: URL(fileURLWithPath: "/nonexistent"), codex: CodexRollouts(root: URL(fileURLWithPath: "/nonexistent")))))
    }

    @Test func stopClearsTheRestoredPrompt() async throws {
        let screen = Screen()
        try await withService(screen) { service in
            try await service.sendKeys(id: "w14:p2", keys: ["esc"])
            #expect(screen.keys.withLock { $0 } == [["esc"], ["ctrl+u", "ctrl+u", "ctrl+u", "ctrl+u"]])
            #expect(screen.box.withLock { $0 } == "")
        }
    }

    @Test func promptClearsLeftoverInputFirst() async throws {
        let screen = Screen()
        screen.box.withLock { $0 = "leftover" }
        try await withService(screen) { service in
            try await service.prompt(id: "w14:p2", text: "second")
            #expect(screen.keys.withLock { $0 } == [["ctrl+u", "ctrl+u"]])
            #expect(screen.prompts.withLock { $0 } == ["second"])
        }
    }

    @Test func emptyInputPromptsWithoutKeys() async throws {
        let screen = Screen()
        try await withService(screen) { service in
            try await service.prompt(id: "w14:p2", text: "hi")
            #expect(screen.keys.withLock { $0 }.isEmpty)
        }
    }

    @Test func blockedAgentIsNeverCleared() async throws {
        let screen = Screen()
        screen.status.withLock { $0 = "blocked" }
        screen.box.withLock { $0 = "leftover" }
        try await withService(screen) { service in
            try await service.sendKeys(id: "w14:p2", keys: ["down", "down"])
            #expect(screen.keys.withLock { $0 } == [["down", "down"]])
        }
    }

    @Test func textTypesLiterallyThenEnter() async throws {
        let screen = Screen()
        screen.status.withLock { $0 = "blocked" }
        try await withService(screen) { service in
            try await service.text(id: "w14:p2", text: "Green tea, please", submit: true)
            #expect(screen.texts.withLock { $0 } == ["Green tea, please"])
            #expect(screen.keys.withLock { $0 } == [["enter"]])
            try await service.text(id: "w14:p2", text: "draft", submit: false)
            #expect(screen.keys.withLock { $0 } == [["enter"]])
        }
    }
}
