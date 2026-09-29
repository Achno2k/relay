#if DEBUG
import Foundation
import RelayKit

/// A made-up machine for `-mock`. The Mac serves the fixtures; the VM (`-mockVM`) and any machine added
/// with a `mock-…` pair link serve the small sets below. The VM shares raw ids with the Mac on purpose
/// (workspace `w2`, pane `w2:p1`) so keying by machine is exercised.
struct MockMachine: Sendable, Hashable {
    enum Variant: Sendable, Hashable { case mac, vm, blank }

    var machine: Machine
    var variant: Variant
    /// Added to attachment ids so two mock machines never hand out the same one.
    var idOffset: Int

    static let mac = MockMachine(
        machine: Machine(id: "mock-mac", name: "Mock MacBook Pro", kind: .laptop, model: "Mac15,9", os: "macOS 26.4"),
        variant: .mac, idOffset: 0
    )
    static let vm = MockMachine(
        machine: Machine(id: "mock-vm", name: "Mock VM", kind: .desktop, model: "x86_64", os: "Ubuntu 24.04.1 LTS"),
        variant: .vm, idOffset: 1 << 40
    )

    /// `relay://pair?url=http://mock-third:7878&token=t` → id `mock-third`, "Mock Third".
    static func extra(host: String) -> MockMachine {
        let words = host.split(separator: "-").map { $0.prefix(1).uppercased() + $0.dropFirst() }
        return MockMachine(
            machine: Machine(id: host, name: words.joined(separator: " "), kind: .desktop, model: "x86_64", os: "Ubuntu 24.04.1 LTS"),
            variant: .blank, idOffset: (host.utf8.reduce(0) { $0 &+ Int($1) } % 1000 + 2) << 40
        )
    }
}

enum MockMachines {
    struct Content {
        var agents: [Agent]
        var workspaces: [Workspace]
        var chats: [String: [Message]]
        var approvals: [String: Approval]
    }

    /// The non-Mac machines' agents and chats.
    static func content(for profile: MockMachine, now: Date = Date()) -> Content {
        func agent(_ id: String, _ kind: String, _ title: String, _ ws: Workspace, _ status: AgentStatus, minutesAgo: Double) -> Agent {
            Agent(
                id: id, name: nil, kind: kind, title: title,
                workspaceId: ws.id, workspaceName: ws.name, cwdName: ws.name,
                status: status, hasTranscript: true, updatedAt: now.addingTimeInterval(-minutesAgo * 60),
                model: kind == "claude" ? "claude-opus-5-5" : nil, modelLabel: kind == "claude" ? "Opus 5.5" : nil,
                permissionMode: kind == "claude" ? "default" : nil, sessionId: "\(profile.machine.id)-\(id)"
            )
        }
        func chat(_ prefix: String, _ prompt: String, _ reply: String, minutesAgo: Double) -> [Message] {
            let at = now.addingTimeInterval(-minutesAgo * 60)
            return [
                Message(id: "\(prefix)-u1", role: .user, createdAt: at, blocks: [.text(prompt)]),
                Message(id: "\(prefix)-a1", role: .assistant, createdAt: at.addingTimeInterval(20), blocks: [.text(reply)]),
            ]
        }
        switch profile.variant {
        case .mac:
            return Content(agents: [], workspaces: [], chats: [:], approvals: [:])
        case .vm:
            let infra = Workspace(id: "w2", name: "infra", agentCount: 0)
            let db = Workspace(id: "w5", name: "database", agentCount: 0)
            let agents = [
                agent("w2:p1", "claude", "Rotate TLS certificates", infra, .idle, minutesAgo: 9),
                agent("w2:p2", "codex", "Nightly backup check", infra, .working, minutesAgo: 1),
                agent("w5:p1", "claude", "Tune Postgres autovacuum", db, .blocked, minutesAgo: 4),
            ]
            let approval = Approval(
                agentId: "w5:p1",
                question: "Run ALTER SYSTEM SET autovacuum_naptime = '30s'?",
                options: [
                    ApprovalOption(label: "Yes", keys: ["enter"]),
                    ApprovalOption(label: "No", keys: ["esc"]),
                ]
            )
            return Content(
                agents: agents,
                workspaces: [infra, db],
                chats: [
                    "w2:p1": chat("vm-tls", "Rotate the TLS certificates on the VM.", "Rotated both certificates. nginx reloaded cleanly.", minutesAgo: 9),
                    "w2:p2": chat("vm-backup", "Check last night's backup.", "Checking the backup logs now.", minutesAgo: 2),
                    "w5:p1": chat("vm-pg", "Autovacuum is lagging. Tune it.", "I'd like to shorten the naptime.", minutesAgo: 4),
                ],
                approvals: ["w5:p1": approval]
            )
        case .blank:
            let scratch = Workspace(id: "w1", name: "scratch", agentCount: 0)
            return Content(
                agents: [agent("w1:p1", "claude", "Hello from \(profile.machine.name)", scratch, .idle, minutesAgo: 30)],
                workspaces: [scratch],
                chats: ["w1:p1": chat("\(profile.machine.id)-hello", "Say hello.", "Hello from \(profile.machine.name).", minutesAgo: 30)],
                approvals: [:]
            )
        }
    }
}
#endif
