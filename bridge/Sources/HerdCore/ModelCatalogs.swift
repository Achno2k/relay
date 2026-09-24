import Foundation
import Synchronization

/// Model lists that come from the agents themselves (`pi --list-models`, `codex debug models`),
/// cached for 10 minutes per kind.
public final class ModelCatalogs: Sendable {
    public struct PiModel: Sendable, Equatable {
        public var provider: String
        public var id: String
        public var thinking: Bool
        public var full: String { "\(provider)/\(id)" }
    }

    public struct CodexModel: Sendable, Equatable {
        public var slug: String
        public var displayName: String
        public var visible: Bool
        public var efforts: [String]
        public var defaultEffort: String?
    }

    public struct PiSettings: Sendable, Equatable {
        public var defaultModel: String?      // provider/id
        public var defaultThinking: String?
        public var enabledModels: [String]    // patterns as written
    }

    /// Runs a command and returns stdout; injectable for tests.
    public typealias Runner = @Sendable (_ args: [String]) -> Data?

    let run: Runner
    let piSettingsURL: URL
    let codexSessions: URL
    private let cache = Mutex<[String: (at: Date, data: Data)]>([:])
    private let rollouts = Mutex<[String: URL]>([:])

    public init(run: @escaping Runner = ModelCatalogs.process,
                piSettingsURL: URL = FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".pi/agent/settings.json"),
                codexSessions: URL = FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".codex/sessions")) {
        self.run = run
        self.piSettingsURL = piSettingsURL
        self.codexSessions = codexSessions
    }

    /// `~/.codex/sessions/YYYY/MM/DD/rollout-*-<sessionId>.jsonl`, newest days first.
    public func codexRollout(_ sessionId: String) -> URL? {
        if let hit = rollouts.withLock({ $0[sessionId] }), FileManager.default.fileExists(atPath: hit.path) { return hit }
        let fm = FileManager.default
        func sortedDirs(_ u: URL) -> [URL] {
            ((try? fm.contentsOfDirectory(at: u, includingPropertiesForKeys: nil)) ?? [])
                .filter(\.hasDirectoryPath).sorted { $0.lastPathComponent > $1.lastPathComponent }
        }
        var days: [URL] = []
        for y in sortedDirs(codexSessions) {
            for m in sortedDirs(y) { days += sortedDirs(m) }
            if days.count > 60 { break }
        }
        for d in days.prefix(60) {
            let files = (try? fm.contentsOfDirectory(atPath: d.path)) ?? []
            if let f = files.first(where: { $0.hasSuffix("\(sessionId).jsonl") }) {
                let url = d.appendingPathComponent(f)
                rollouts.withLock { $0[sessionId] = url }
                return url
            }
        }
        return nil
    }

    private func cached(_ key: String, _ args: [String]) -> Data? {
        if let hit = cache.withLock({ $0[key] }), Date().timeIntervalSince(hit.at) < 600 { return hit.data }
        guard let data = run(args), !data.isEmpty else { return nil }
        cache.withLock { $0[key] = (Date(), data) }
        return data
    }

    public func pi() -> [PiModel] {
        guard let data = cached("pi", ["pi", "--list-models"]) else { return [] }
        return Self.parsePiList(String(decoding: data, as: UTF8.self))
    }

    public func codex() -> [CodexModel] {
        guard let data = cached("codex", ["codex", "debug", "models"]) else { return [] }
        return Self.parseCodexCatalog(data)
    }

    public func piSettings() -> PiSettings {
        guard let data = try? Data(contentsOf: piSettingsURL),
              let o = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any]
        else { return PiSettings(enabledModels: []) }
        let model: String? = {
            guard let m = o["defaultModel"] as? String else { return nil }
            if m.contains("/") { return m }
            return (o["defaultProvider"] as? String).map { "\($0)/\(m)" } ?? m
        }()
        return PiSettings(defaultModel: model, defaultThinking: o["defaultThinkingLevel"] as? String,
                          enabledModels: o["enabledModels"] as? [String] ?? [])
    }

    // MARK: Parsing (pure)

    /// `provider  model  context  max-out  thinking  images` table.
    public static func parsePiList(_ text: String) -> [PiModel] {
        text.components(separatedBy: "\n").dropFirst().compactMap { line in
            let cols = line.split(separator: " ", omittingEmptySubsequences: true).map(String.init)
            guard cols.count >= 5, cols[0] != "provider" else { return nil }
            return PiModel(provider: cols[0], id: cols[1], thinking: cols[4] == "yes")
        }
    }

    public static func parseCodexCatalog(_ data: Data) -> [CodexModel] {
        guard let o = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
              let models = o["models"] as? [[String: Any]] else { return [] }
        return models
            .sorted { ($0["priority"] as? Int ?? 999) < ($1["priority"] as? Int ?? 999) }
            .compactMap { m in
                guard let slug = m["slug"] as? String else { return nil }
                let efforts = (m["supported_reasoning_levels"] as? [[String: Any]] ?? []).compactMap { $0["effort"] as? String }
                return CodexModel(slug: slug, displayName: m["display_name"] as? String ?? slug,
                                  visible: (m["visibility"] as? String) == "list", efforts: efforts,
                                  defaultEffort: m["default_reasoning_level"] as? String)
            }
    }

    // MARK: Process

    public static let process: Runner = { args in
        let p = Process()
        p.executableURL = URL(fileURLWithPath: "/usr/bin/env")
        p.arguments = args
        var env = ProcessInfo.processInfo.environment
        env["PATH"] = "/opt/homebrew/bin:/usr/local/bin:" + (env["PATH"] ?? "/usr/bin:/bin")
        env["NO_COLOR"] = "1"
        p.environment = env
        let out = Pipe()
        p.standardOutput = out
        p.standardError = FileHandle.nullDevice
        p.standardInput = FileHandle.nullDevice
        do { try p.run() } catch { return nil }
        let data = out.fileHandleForReading.readDataToEndOfFile()
        p.waitUntilExit()
        return p.terminationStatus == 0 ? data : nil
    }
}
