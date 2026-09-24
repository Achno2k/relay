import Foundation
import Synchronization

/// Finds codex rollout files: `~/.codex/sessions/YYYY/MM/DD/rollout-<time>-<sessionId>.jsonl`.
public final class CodexRollouts: Sendable {
    public let root: URL
    private let byId = Mutex<[String: URL]>([:])

    public init(root: URL = FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".codex/sessions")) {
        self.root = root
    }

    /// Day folders, newest first (at most `limit`).
    func days(limit: Int = 60) -> [URL] {
        let fm = FileManager.default
        func sorted(_ u: URL) -> [URL] {
            ((try? fm.contentsOfDirectory(at: u, includingPropertiesForKeys: nil)) ?? [])
                .filter(\.hasDirectoryPath).sorted { $0.lastPathComponent > $1.lastPathComponent }
        }
        var out: [URL] = []
        for y in sorted(root) {
            for m in sorted(y) { out += sorted(m) }
            if out.count >= limit { break }
        }
        return Array(out.prefix(limit))
    }

    public func find(sessionId: String) -> URL? {
        if let hit = byId.withLock({ $0[sessionId] }), FileManager.default.fileExists(atPath: hit.path) { return hit }
        for d in days() {
            let files = (try? FileManager.default.contentsOfDirectory(atPath: d.path)) ?? []
            if let f = files.first(where: { $0.hasPrefix("rollout-") && $0.hasSuffix("\(sessionId).jsonl") }) {
                let url = d.appendingPathComponent(f)
                byId.withLock { $0[sessionId] = url }
                return url
            }
        }
        return nil
    }

    /// When herdr has no session id yet: the newest rollout (last 2 days) started in `cwd`.
    public func newest(cwd: String) -> URL? {
        let fm = FileManager.default
        var best: (URL, Date)?
        for d in days(limit: 2) {
            for f in (try? fm.contentsOfDirectory(atPath: d.path)) ?? [] where f.hasPrefix("rollout-") && f.hasSuffix(".jsonl") {
                let url = d.appendingPathComponent(f)
                guard Self.sessionCwd(url) == cwd else { continue }
                let m = (try? url.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate) ?? .distantPast
                if best == nil || m > best!.1 { best = (url, m) }
            }
        }
        return best?.0
    }

    /// `session_meta.payload.cwd` from the first line.
    static func sessionCwd(_ url: URL) -> String? {
        guard let h = try? FileHandle(forReadingFrom: url) else { return nil }
        defer { try? h.close() }
        guard let data = try? h.read(upToCount: 64 * 1024),
              let line = data.split(separator: UInt8(ascii: "\n"), maxSplits: 1).first,
              let o = (try? JSONSerialization.jsonObject(with: Data(line))) as? [String: Any],
              o["type"] as? String == "session_meta"
        else { return nil }
        return (o["payload"] as? [String: Any])?["cwd"] as? String
    }
}
