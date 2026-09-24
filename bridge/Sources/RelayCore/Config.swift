import Foundation

/// `~/.relay` (overridable with `RELAY_HOME`) holds the token, uploads and logs.
public enum RelayHome {
    public static var url: URL {
        if let h = override { return URL(fileURLWithPath: h) }
        return FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".relay")
    }

    static var override: String? {
        ProcessInfo.processInfo.environment["RELAY_HOME"].flatMap { $0.isEmpty ? nil : $0 }
    }

    /// The data dir before the rename to Relay.
    public static var legacyURL: URL {
        FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".herd")
    }

    /// Moves `~/.herd` to `~/.relay` once (token, uploads, logs, e2e), so paired phones keep their
    /// token. Only when `~/.relay` doesn't exist yet and no `RELAY_HOME` override is set.
    /// Returns a line describing what happened, for the log.
    @discardableResult
    public static func migrateIfNeeded(from legacy: URL = legacyURL, to target: URL? = nil) -> String? {
        let target = target ?? url
        if target == url, override != nil { return nil }
        let fm = FileManager.default
        guard fm.fileExists(atPath: legacy.path) else { return nil }
        do {
            if !fm.fileExists(atPath: target.path) {
                try fm.moveItem(at: legacy, to: target)
                return "moved \(legacy.path) to \(target.path) (token, uploads, logs kept)"
            }
            // `~/.relay` already exists (e.g. launchd created it for the log) but was never used:
            // move the old contents in, never overwriting, so the phone keeps its token.
            guard !fm.fileExists(atPath: target.appendingPathComponent("token").path),
                  fm.fileExists(atPath: legacy.appendingPathComponent("token").path)
            else { return nil }
            var moved: [String] = []
            for name in try fm.contentsOfDirectory(atPath: legacy.path)
            where !fm.fileExists(atPath: target.appendingPathComponent(name).path) {
                try fm.moveItem(at: legacy.appendingPathComponent(name), to: target.appendingPathComponent(name))
                moved.append(name)
            }
            if (try? fm.contentsOfDirectory(atPath: legacy.path))?.isEmpty == true { try? fm.removeItem(at: legacy) }
            return "moved \(moved.sorted().joined(separator: ", ")) from \(legacy.path) into \(target.path)"
        } catch {
            return "could not move \(legacy.path) to \(target.path): \(error.localizedDescription)"
        }
    }

    static func ensure() throws {
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
    }
}

public enum TokenStore {
    public static var url: URL { RelayHome.url.appendingPathComponent("token") }

    /// Reads the token, creating one on first run (after moving an old `~/.herd` over).
    public static func load() throws -> String {
        if let note = RelayHome.migrateIfNeeded() { print("relay: \(note)") }
        if let s = try? String(contentsOf: url, encoding: .utf8) {
            let t = s.trimmingCharacters(in: .whitespacesAndNewlines)
            if !t.isEmpty { return t }
        }
        return try rotate()
    }

    @discardableResult
    public static func rotate() throws -> String {
        try RelayHome.ensure()
        let token = generate()
        let fm = FileManager.default
        if fm.fileExists(atPath: url.path) { try fm.removeItem(at: url) }
        guard fm.createFile(atPath: url.path, contents: Data((token + "\n").utf8), attributes: [.posixPermissions: 0o600]) else {
            throw CocoaError(.fileWriteUnknown)
        }
        return token
    }

    static func generate() -> String {
        var g = SystemRandomNumberGenerator()
        let bytes = (0..<32).map { _ in UInt8.random(in: .min ... .max, using: &g) }
        return Data(bytes).base64EncodedString()
            .replacingOccurrences(of: "+", with: "-")
            .replacingOccurrences(of: "/", with: "_")
            .replacingOccurrences(of: "=", with: "")
    }
}

public enum Tailscale {
    static let candidates = [
        "/usr/local/bin/tailscale",
        "/opt/homebrew/bin/tailscale",
        "/Applications/Tailscale.app/Contents/MacOS/Tailscale",
    ]

    /// `tailscale ip -4`, or nil when Tailscale isn't installed or running.
    public static func ipv4() -> String? {
        let pathDirs = (ProcessInfo.processInfo.environment["PATH"] ?? "").split(separator: ":").map { "\($0)/tailscale" }
        guard let bin = (pathDirs + candidates).first(where: { FileManager.default.isExecutableFile(atPath: $0) }) else { return nil }
        let p = Process()
        p.executableURL = URL(fileURLWithPath: bin)
        p.arguments = ["ip", "-4"]
        let out = Pipe()
        p.standardOutput = out
        p.standardError = FileHandle.nullDevice
        do { try p.run() } catch { return nil }
        let deadline = Date().addingTimeInterval(3)
        while p.isRunning && Date() < deadline { Thread.sleep(forTimeInterval: 0.05) }
        if p.isRunning {
            p.terminate()
            return nil
        }
        guard p.terminationStatus == 0 else { return nil }
        let s = String(decoding: out.fileHandleForReading.readDataToEndOfFile(), as: UTF8.self)
        return s.split(separator: "\n").map { $0.trimmingCharacters(in: .whitespaces) }.first(where: isIPv4)
    }

    static func isIPv4(_ s: String) -> Bool {
        let parts = s.split(separator: ".")
        return parts.count == 4 && parts.allSatisfy { UInt8($0) != nil }
    }
}

public enum Pairing {
    public static func url(host: String, port: Int, token: String) -> String {
        var c = URLComponents()
        c.scheme = "relay"
        c.host = "pair"
        c.queryItems = [
            URLQueryItem(name: "url", value: "http://\(host):\(port)"),
            URLQueryItem(name: "token", value: token),
        ]
        // URLComponents leaves ":" and "/" in query values; encode them so the app can split safely.
        let allowed = CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "-._~"))
        c.percentEncodedQueryItems = c.queryItems?.map {
            URLQueryItem(name: $0.name, value: $0.value?.addingPercentEncoding(withAllowedCharacters: allowed))
        }
        return c.string!
    }
}

public enum LaunchAgent {
    public static let label = "com.relay.bridge"

    public static var plistURL: URL {
        FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent("Library/LaunchAgents/\(label).plist")
    }

    public static func plist(executable: String, port: Int) throws -> Data {
        let log = RelayHome.url.appendingPathComponent("relay.log").path
        let dict: [String: Any] = [
            "Label": label,
            // At login Tailscale may not be up yet; exiting lets KeepAlive retry until it is.
            "ProgramArguments": [executable, "serve", "--port", String(port), "--require-tailscale"],
            "RunAtLoad": true,
            "KeepAlive": true,
            "StandardOutPath": log,
            "StandardErrorPath": log,
            "EnvironmentVariables": [
                "PATH": "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin",
            ],
        ]
        return try PropertyListSerialization.data(fromPropertyList: dict, format: .xml, options: 0)
    }
}
