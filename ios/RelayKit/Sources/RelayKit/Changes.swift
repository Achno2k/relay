import Foundation

// The read-only changes review (api.md "Changes"): uncommitted files and unpushed commits in the agent's cwd.

/// `GET /agents/:id/changes`.
public struct Changes: Decodable, Hashable, Sendable {
    /// false: the cwd isn't inside a git work tree, and every other field is empty.
    public var repo: Bool
    /// nil when HEAD is detached.
    public var branch: String?
    /// nil when the branch has no upstream.
    public var upstream: String?
    /// vs `upstream`, from the last fetch; nil without one.
    public var ahead: Int?
    public var behind: Int?
    /// Staged + unstaged vs HEAD, plus untracked. Sorted by path.
    public var files: [ChangedFile]
    /// Only the first 500 files are listed.
    public var moreFiles: Bool
    /// Newest first.
    public var commits: [CommitSummary]
    /// Only the first 50 unpushed commits are listed.
    public var moreCommits: Bool

    public init(
        repo: Bool, branch: String? = nil, upstream: String? = nil, ahead: Int? = nil, behind: Int? = nil,
        files: [ChangedFile] = [], moreFiles: Bool = false, commits: [CommitSummary] = [], moreCommits: Bool = false
    ) {
        self.repo = repo
        self.branch = branch
        self.upstream = upstream
        self.ahead = ahead
        self.behind = behind
        self.files = files
        self.moreFiles = moreFiles
        self.commits = commits
        self.moreCommits = moreCommits
    }

    enum CodingKeys: String, CodingKey {
        case repo, branch, upstream, ahead, behind, files, moreFiles, commits, moreCommits
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        repo = try c.decode(Bool.self, forKey: .repo)
        branch = try c.decodeIfPresent(String.self, forKey: .branch)
        upstream = try c.decodeIfPresent(String.self, forKey: .upstream)
        ahead = try c.decodeIfPresent(Int.self, forKey: .ahead)
        behind = try c.decodeIfPresent(Int.self, forKey: .behind)
        files = try c.decodeIfPresent([ChangedFile].self, forKey: .files) ?? []
        moreFiles = try c.decodeIfPresent(Bool.self, forKey: .moreFiles) ?? false
        commits = try c.decodeIfPresent([CommitSummary].self, forKey: .commits) ?? []
        moreCommits = try c.decodeIfPresent(Bool.self, forKey: .moreCommits) ?? false
    }
}

public enum ChangeStatus: String, Decodable, Hashable, Sendable {
    case modified, added, deleted, renamed, untracked, conflicted, unknown

    public init(from decoder: any Decoder) throws {
        self = ChangeStatus(rawValue: try decoder.singleValueContainer().decode(String.self)) ?? .unknown
    }
}

public struct ChangedFile: Decodable, Hashable, Sendable {
    /// cwd-relative.
    public var path: String
    /// Renames only.
    public var oldPath: String?
    public var status: ChangeStatus
    /// 0 and 0 for binary files.
    public var additions: Int
    public var deletions: Int
    public var binary: Bool

    public init(path: String, oldPath: String? = nil, status: ChangeStatus, additions: Int = 0, deletions: Int = 0, binary: Bool = false) {
        self.path = path
        self.oldPath = oldPath
        self.status = status
        self.additions = additions
        self.deletions = deletions
        self.binary = binary
    }

    enum CodingKeys: String, CodingKey {
        case path, oldPath, status, additions, deletions, binary
    }

    public init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        path = try c.decode(String.self, forKey: .path)
        oldPath = try c.decodeIfPresent(String.self, forKey: .oldPath)
        status = try c.decode(ChangeStatus.self, forKey: .status)
        additions = try c.decodeIfPresent(Int.self, forKey: .additions) ?? 0
        deletions = try c.decodeIfPresent(Int.self, forKey: .deletions) ?? 0
        binary = try c.decodeIfPresent(Bool.self, forKey: .binary) ?? false
    }
}

public struct CommitSummary: Decodable, Hashable, Identifiable, Sendable {
    public var sha: String
    public var shortSha: String
    public var subject: String
    public var time: Date
    /// The commit's whole change (vs its first parent), not only the part under the cwd.
    public var fileCount: Int
    public var additions: Int
    public var deletions: Int

    public var id: String { sha }

    public init(sha: String, shortSha: String, subject: String, time: Date, fileCount: Int, additions: Int, deletions: Int) {
        self.sha = sha
        self.shortSha = shortSha
        self.subject = subject
        self.time = time
        self.fileCount = fileCount
        self.additions = additions
        self.deletions = deletions
    }
}

/// `GET /agents/:id/changes/diff?path=`: one uncommitted file.
public struct FileDiffText: Decodable, Hashable, Sendable {
    public var path: String
    /// Unified, 3 lines of context, `--- a/<path>` / `+++ b/<path>` headers. Empty for a binary file.
    public var diff: String
    public var binary: Bool
    /// Cut at 256 KB on a line boundary.
    public var truncated: Bool

    public init(path: String, diff: String, binary: Bool = false, truncated: Bool = false) {
        self.path = path
        self.diff = diff
        self.binary = binary
        self.truncated = truncated
    }
}

/// `GET /agents/:id/changes/commits/:sha`.
public struct CommitDetail: Decodable, Hashable, Sendable {
    public var sha: String
    public var shortSha: String
    public var subject: String
    /// The message after the subject, trimmed; "" when none.
    public var body: String
    public var time: Date
    public var files: [CommitFile]
    /// Some file's diff was cut or emptied (1 MB across the commit).
    public var truncated: Bool

    public init(sha: String, shortSha: String, subject: String, body: String = "", time: Date, files: [CommitFile], truncated: Bool = false) {
        self.sha = sha
        self.shortSha = shortSha
        self.subject = subject
        self.body = body
        self.time = time
        self.files = files
        self.truncated = truncated
    }
}

/// A `ChangedFile` with its diff in the commit.
public struct CommitFile: Decodable, Hashable, Sendable {
    public var file: ChangedFile
    public var diff: String
    public var truncated: Bool

    public init(file: ChangedFile, diff: String, truncated: Bool = false) {
        self.file = file
        self.diff = diff
        self.truncated = truncated
    }

    enum CodingKeys: String, CodingKey { case diff, truncated }

    public init(from decoder: any Decoder) throws {
        file = try ChangedFile(from: decoder)
        let c = try decoder.container(keyedBy: CodingKeys.self)
        diff = try c.decodeIfPresent(String.self, forKey: .diff) ?? ""
        truncated = try c.decodeIfPresent(Bool.self, forKey: .truncated) ?? false
    }
}
