import Foundation
import Synchronization

public struct TranscriptRef: Sendable, Equatable {
    public var url: URL
    public var format: TranscriptFormat
    public var cwd: String?
}

/// Finds the transcript file behind a herdr agent.
/// Claude: `~/.claude/projects/<cwd with non-alphanumerics as ->/<sessionId>.jsonl`. pi: the session path.
public final class TranscriptLocator: Sendable {
    public let claudeProjects: URL
    private let found = Mutex<[String: URL]>([:])

    public init(claudeProjects: URL = FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent(".claude/projects")) {
        self.claudeProjects = claudeProjects
    }

    public static func projectDirName(for cwd: String) -> String {
        String(cwd.unicodeScalars.map { s -> Character in
            CharacterSet.alphanumerics.contains(s) && s.isASCII || s == "-" ? Character(s) : "-"
        })
    }

    public func locate(_ a: HerdrAgent) -> TranscriptRef? {
        guard let session = a.agentSession else { return nil }
        let kind = (a.agent ?? session.agent).lowercased()
        let cwd = a.cwd ?? a.foregroundCwd
        let fm = FileManager.default

        if session.kind == "path" {
            let url = URL(fileURLWithPath: session.value)
            guard fm.fileExists(atPath: url.path) else { return nil }
            return TranscriptRef(url: url, format: kind == "claude" ? .claude : .pi, cwd: cwd)
        }
        guard session.kind == "id", kind == "claude" else { return nil }
        let file = session.value + ".jsonl"
        guard !session.value.contains("/") else { return nil }

        for dir in [a.cwd, a.foregroundCwd].compactMap({ $0 }) {
            let url = claudeProjects.appendingPathComponent(Self.projectDirName(for: dir)).appendingPathComponent(file)
            if fm.fileExists(atPath: url.path) { return TranscriptRef(url: url, format: .claude, cwd: cwd) }
        }
        if let cached = found.withLock({ $0[session.value] }), fm.fileExists(atPath: cached.path) {
            return TranscriptRef(url: cached, format: .claude, cwd: cwd)
        }
        // Slow path: the project dir name didn't match; look in every project dir.
        let dirs = (try? fm.contentsOfDirectory(at: claudeProjects, includingPropertiesForKeys: nil)) ?? []
        for d in dirs {
            let url = d.appendingPathComponent(file)
            if fm.fileExists(atPath: url.path) {
                found.withLock { $0[session.value] = url }
                return TranscriptRef(url: url, format: .claude, cwd: cwd)
            }
        }
        return nil
    }
}
