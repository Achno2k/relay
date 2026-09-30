import Foundation

/// `GET /agents/:id/file` for a text file (api.md "Files").
public struct FileContent: Codable, Hashable, Sendable {
    /// cwd-relative, as the bridge normalised it.
    public var path: String
    public var content: String
    /// Bytes on disk, even when `content` is cut.
    public var size: Int
    /// The file is over 1 MB and `content` is its first 1 MB.
    public var truncated: Bool
    /// Lowercase hint from the extension (`swift`, `ts`, `md`, …).
    public var language: String?

    public init(path: String, content: String, size: Int, truncated: Bool = false, language: String? = nil) {
        self.path = path
        self.content = content
        self.size = size
        self.truncated = truncated
        self.language = language
    }
}

/// The two `200` shapes of `GET /agents/:id/file`, told apart by `Content-Type`.
public enum AgentFile: Hashable, Sendable {
    case text(FileContent)
    case image(Data, contentType: String)
}
