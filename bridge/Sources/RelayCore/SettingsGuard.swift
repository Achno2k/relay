import Foundation

/// Claude Code's `/model` and `/effort` also save the choice as the default for new sessions
/// (`~/.claude/settings.json`). A switch from the phone is meant for one agent, so the bridge
/// snapshots the file before the command and puts the exact bytes back afterwards.
/// The running session keeps its new model/effort; only the saved default is restored.
/// Runs async work one call at a time (FIFO).
actor SerialLock {
    private var busy = false
    private var waiters: [CheckedContinuation<Void, Never>] = []

    func run<T: Sendable>(_ work: @Sendable () async throws -> T) async throws -> T {
        if busy {
            await withCheckedContinuation { waiters.append($0) }
        } else {
            busy = true
        }
        defer {
            if waiters.isEmpty { busy = false } else { waiters.removeFirst().resume() }
        }
        return try await work()
    }
}

public struct SettingsGuard: Sendable {
    public let url: URL?

    public init(url: URL? = FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".claude/settings.json")) {
        self.url = url
    }

    public struct Snapshot: Sendable {
        let data: Data?
    }

    public func snapshot() -> Snapshot {
        Snapshot(data: url.flatMap { try? Data(contentsOf: $0) })
    }

    /// Writes the snapshot back if the file changed. Returns true if it restored something.
    @discardableResult
    public func restore(_ s: Snapshot) -> Bool {
        guard let url, let original = s.data else { return false }
        guard let now = try? Data(contentsOf: url), now != original else { return false }
        // In place, so the file keeps its inode and 0600 permissions.
        return (try? original.write(to: url)) != nil
    }
}
