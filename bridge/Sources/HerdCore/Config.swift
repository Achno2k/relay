import Foundation

/// `~/.herd` (overridable with `HERD_HOME`) holds the token and logs.
public enum HerdHome {
    public static var url: URL {
        if let h = ProcessInfo.processInfo.environment["HERD_HOME"], !h.isEmpty { return URL(fileURLWithPath: h) }
        return FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".herd")
    }

    static func ensure() throws {
        try FileManager.default.createDirectory(at: url, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
    }
}

public enum TokenStore {
    public static var url: URL { HerdHome.url.appendingPathComponent("token") }

    /// Reads the token, creating one on first run.
    public static func load() throws -> String {
        if let s = try? String(contentsOf: url, encoding: .utf8) {
            let t = s.trimmingCharacters(in: .whitespacesAndNewlines)
            if !t.isEmpty { return t }
        }
        return try rotate()
    }

    @discardableResult
    public static func rotate() throws -> String {
        try HerdHome.ensure()
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
        c.scheme = "herd"
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
    public static let label = "com.herd.bridge"

    public static var plistURL: URL {
        FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent("Library/LaunchAgents/\(label).plist")
    }

    public static func plist(executable: String, port: Int) throws -> Data {
        let log = HerdHome.url.appendingPathComponent("herd.log").path
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
