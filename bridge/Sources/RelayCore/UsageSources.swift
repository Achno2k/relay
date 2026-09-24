import Foundation

/// Spawns `codex app-server`, asks `account/rateLimits/read` once over JSON-RPC/stdio, and returns
/// the raw response line (or nil). Always reaps the child, even if it never answers.
public struct CodexUsageProbe: Sendable {
    var run: @Sendable () -> Data?

    public init(run: @escaping @Sendable () -> Data? = CodexUsageProbe.liveRun) {
        self.run = run
    }

    public func fetch() -> Data? { run() }

    public static func liveRun() -> Data? {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: "/usr/bin/env")
        p.arguments = ["codex", "app-server"]
        var env = ProcessInfo.processInfo.environment
        env["PATH"] = "/opt/homebrew/bin:/usr/local/bin:" + (env["PATH"] ?? "/usr/bin:/bin")
        p.environment = env
        let stdin = Pipe()
        let stdout = Pipe()
        p.standardInput = stdin
        p.standardOutput = stdout
        p.standardError = FileHandle.nullDevice
        do { try p.run() } catch { return nil }

        // Guarantees the child is reaped even if it never answers: a watchdog on another thread
        // kills it after `timeout`, which closes its stdout and unblocks the read loop below.
        let timeout: TimeInterval = 6
        let watchdog = DispatchWorkItem { if p.isRunning { p.terminate() } }
        DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + timeout, execute: watchdog)
        defer { reap(p, watchdog: watchdog) }

        func send(_ obj: [String: Any]) {
            guard var data = try? JSONSerialization.data(withJSONObject: obj) else { return }
            data.append(UInt8(ascii: "\n"))
            stdin.fileHandleForWriting.write(data)
        }
        send(["jsonrpc": "2.0", "id": 1, "method": "initialize",
              "params": ["clientInfo": ["name": "relay-bridge", "version": Relay.version]]])
        send(["jsonrpc": "2.0", "id": 2, "method": "account/rateLimits/read", "params": [String: Any]()])

        let out = stdout.fileHandleForReading
        var buffer = Data()
        while true {
            let chunk = out.availableData
            if chunk.isEmpty { return nil }  // EOF: exited on its own, or the watchdog just killed it
            buffer.append(chunk)
            while let nl = buffer.firstIndex(of: UInt8(ascii: "\n")) {
                let lineData = buffer[buffer.startIndex..<nl]
                buffer.removeSubrange(buffer.startIndex...nl)
                guard !lineData.isEmpty,
                      let obj = try? JSONSerialization.jsonObject(with: Data(lineData)) as? [String: Any],
                      obj["id"] as? Int == 2
                else { continue }
                return Data(lineData)
            }
        }
    }

    /// SIGTERM, wait briefly, SIGKILL if it's still alive, then reap. Never leaves a zombie.
    public static func reap(_ p: Process, watchdog: DispatchWorkItem) {
        watchdog.cancel()
        if p.isRunning {
            p.terminate()
            let deadline = Date().addingTimeInterval(2)
            while p.isRunning && Date() < deadline { Thread.sleep(forTimeInterval: 0.02) }
            if p.isRunning { kill(p.processIdentifier, SIGKILL) }
        }
        p.waitUntilExit()
    }
}

/// Runs `claude -p "/usage"` (structured `usage_report`) and `claude auth status` (plan name), both
/// from a dedicated cwd with `--no-session-persistence` so they never pollute the user's own history.
public struct ClaudeUsageProbe: Sendable {
    var runUsage: @Sendable (URL) -> Data?
    var runAuthStatus: @Sendable () -> Data?

    public init(runUsage: @escaping @Sendable (URL) -> Data? = ClaudeUsageProbe.liveUsage,
                runAuthStatus: @escaping @Sendable () -> Data? = ClaudeUsageProbe.liveAuthStatus) {
        self.runUsage = runUsage
        self.runAuthStatus = runAuthStatus
    }

    /// `~/.relay/usage-probe`: a dedicated cwd so this never shows up mixed in with the user's real
    /// project sessions. `--no-session-persistence` keeps it from writing a transcript at all; verified
    /// (round 5) that repeated calls leave no new files under `~/.claude/projects/…`.
    public static let probeDir = RelayHome.url.appendingPathComponent("usage-probe")

    public func fetch() -> (usage: Data?, auth: Data?) {
        try? FileManager.default.createDirectory(at: Self.probeDir, withIntermediateDirectories: true)
        return (runUsage(Self.probeDir), runAuthStatus())
    }

    public static func liveUsage(cwd: URL) -> Data? {
        run(["claude", "-p", "/usage", "--output-format", "stream-json", "--verbose", "--no-session-persistence"], cwd: cwd, timeout: 20)
    }

    public static func liveAuthStatus() -> Data? {
        run(["claude", "auth", "status", "--json"], cwd: nil, timeout: 8)
    }

    public static func run(_ args: [String], cwd: URL?, timeout: TimeInterval) -> Data? {
        let p = Process()
        p.executableURL = URL(fileURLWithPath: "/usr/bin/env")
        p.arguments = args
        p.currentDirectoryURL = cwd
        var env = ProcessInfo.processInfo.environment
        env["PATH"] = "/opt/homebrew/bin:/usr/local/bin:" + (env["PATH"] ?? "/usr/bin:/bin")
        p.environment = env
        let out = Pipe()
        p.standardOutput = out
        p.standardError = FileHandle.nullDevice
        p.standardInput = FileHandle.nullDevice
        do { try p.run() } catch { return nil }

        let watchdog = DispatchWorkItem { if p.isRunning { p.terminate() } }
        DispatchQueue.global(qos: .utility).asyncAfter(deadline: .now() + timeout, execute: watchdog)
        defer { CodexUsageProbe.reap(p, watchdog: watchdog) }

        let data = out.fileHandleForReading.readDataToEndOfFile()
        return data.isEmpty ? nil : data
    }
}

/// `pi auth check --provider <id> --json`: reports whether pi has a *valid* stored credential for a
/// provider, never the credential itself. Used to populate `usedBy` (pi joins a subscription's card
/// only when this reports `status: "ready"`) and to build the `opencode-go` card. See api.md "Usage".
public struct PiAuthProbe: Sendable {
    var run: @Sendable (String) -> Data?

    public init(run: @escaping @Sendable (String) -> Data? = PiAuthProbe.liveRun) {
        self.run = run
    }

    /// nil on any failure (pi not on PATH, timeout, non-JSON output) — callers treat that the same as
    /// "not ready", never as "ready".
    public func isReady(provider: String) -> Bool {
        guard let data = run(provider),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              obj["status"] as? String == "ready"
        else { return false }
        return true
    }

    public static func liveRun(provider: String) -> Data? {
        ClaudeUsageProbe.run(["pi", "auth", "check", "--provider", provider, "--json"], cwd: nil, timeout: 8)
    }
}
