import RelayKit
import SwiftUI

/// Wording and rows for the Changes screen, kept out of the views so they're unit-tested.
enum ChangesDisplay {
    /// "↑2 ↓1" vs the upstream; nil without one. Zero sides are left out; both zero is "Up to date".
    static func aheadBehind(_ changes: Changes) -> String? {
        guard changes.upstream != nil else { return nil }
        let ahead = changes.ahead ?? 0, behind = changes.behind ?? 0
        if ahead == 0 && behind == 0 { return "Up to date" }
        return [ahead > 0 ? "↑\(ahead)" : nil, behind > 0 ? "↓\(behind)" : nil].compactMap { $0 }.joined(separator: " ")
    }

    static func aheadBehindLabel(_ changes: Changes) -> String? {
        guard changes.upstream != nil else { return nil }
        let ahead = changes.ahead ?? 0, behind = changes.behind ?? 0
        if ahead == 0 && behind == 0 { return "Up to date with the upstream" }
        return [
            ahead > 0 ? "\(ahead) ahead" : nil,
            behind > 0 ? "\(behind) behind" : nil,
        ].compactMap { $0 }.joined(separator: ", ")
    }

    static func branchTitle(_ changes: Changes) -> String { changes.branch ?? "Detached HEAD" }

    static func upstreamText(_ changes: Changes) -> String { changes.upstream ?? "No upstream branch" }

    /// The diff rows for one file. Its title already names the file, so the `--- a/ +++ b/` header row goes.
    static func diffLines(_ diff: String) -> [CodeLine] {
        FileDiff.parseUnified(diff).filter { $0.kind != .file }
    }

    /// Files grouped by status, in `ChangeGroup` order; empty groups left out. Order within a group is kept.
    static func groups<T>(_ items: [T], file: (T) -> ChangedFile) -> [(group: ChangeGroup, items: [T])] {
        let byGroup = Dictionary(grouping: items) { ChangeGroup(file($0).status) }
        return ChangeGroup.allCases.compactMap { group in byGroup[group].map { (group, $0) } }
    }

    /// "+12 −3".
    static func counts(additions: Int, deletions: Int) -> String { "+\(additions) −\(deletions)" }

    /// "Promo.tsx → Banner.tsx" for a rename in place, the paths when the folder changed too.
    static func renameTitle(_ file: ChangedFile) -> String? {
        guard let old = file.oldPath else { return nil }
        let oldFolder = old.lastIndex(of: "/").map { String(old[..<$0]) }
        let oldName = old.split(separator: "/").last.map(String.init) ?? old
        return oldFolder == file.folder ? "\(oldName) → \(file.fileName)" : "\(old) → \(file.path)"
    }

    /// The row's second line: folder and counts.
    static func detail(_ file: ChangedFile) -> String {
        let lines = file.binary ? "Binary" : counts(additions: file.additions, deletions: file.deletions)
        let folder = file.oldPath != nil && renameTitle(file)?.contains("/") == true ? nil : file.folder
        return [folder, lines].compactMap { $0 }.joined(separator: " · ")
    }

    /// "Open file" fetches the file as it is now, so a deleted one has nothing to open.
    static func canOpen(_ file: ChangedFile) -> Bool { file.status != .deleted }

    /// What's worth knowing before reading the diff, above it. Empty when the diff speaks for itself.
    static func notices(for file: ChangedFile, diff: String, truncated: Bool) -> [DiffNotice] {
        var out: [DiffNotice] = []
        if let old = file.oldPath { out.append(.renamed(from: old)) }
        if truncated { out.append(.truncated(canOpen: canOpen(file))) }
        if !file.binary, diff.isEmpty, !truncated { out.append(file.status == .renamed ? .sameContent : .noLineChanges) }
        return out
    }
}

enum DiffNotice: Hashable {
    case renamed(from: String)
    case truncated(canOpen: Bool)
    /// A pure rename.
    case sameContent
    /// e.g. only the file mode changed.
    case noLineChanges

    var text: String {
        switch self {
        case .renamed(let old): "Renamed from \(old)."
        case .truncated(let canOpen):
            canOpen ? "The diff is too long to show in full. Open the file to see all of it." : "The diff is too long to show in full."
        case .sameContent: "The content didn't change."
        case .noLineChanges: "No line changes."
        }
    }

    var symbol: String {
        switch self {
        case .renamed: "arrow.right.doc.on.clipboard"
        case .truncated: "scissors"
        case .sameContent, .noLineChanges: "equal.circle"
        }
    }

    var tint: Color {
        if case .truncated = self { .orange } else { .secondary }
    }
}

extension ChangeStatus {
    var label: String {
        switch self {
        case .modified: "Modified"
        case .added: "Added"
        case .deleted: "Deleted"
        case .renamed: "Renamed"
        case .untracked: "Untracked"
        case .conflicted: "Conflicted"
        case .unknown: "Changed"
        }
    }
}

/// The list's sections, in order: Modified, New (added + untracked), Deleted, Renamed, Conflicts.
enum ChangeGroup: String, CaseIterable, Hashable {
    case modified = "Modified"
    case new = "New"
    case deleted = "Deleted"
    case renamed = "Renamed"
    case conflicts = "Conflicts"

    init(_ status: ChangeStatus) {
        switch status {
        case .modified, .unknown: self = .modified
        case .added, .untracked: self = .new
        case .deleted: self = .deleted
        case .renamed: self = .renamed
        case .conflicted: self = .conflicts
        }
    }
}

extension ChangedFile {
    var fileName: String { path.split(separator: "/").last.map(String.init) ?? path }

    /// The folder part of `path`; nil at the project root.
    var folder: String? {
        guard let slash = path.lastIndex(of: "/") else { return nil }
        return String(path[..<slash])
    }

    var accessibilityText: String {
        var parts = [fileName, status.label]
        if binary {
            parts.append("binary")
        } else {
            parts.append(ChangesDisplay.countLabel(additions: additions, deletions: deletions))
        }
        if let folder { parts.append("in \(folder)") }
        if let oldPath { parts.append("from \(oldPath)") }
        return parts.joined(separator: ", ")
    }

    /// The viewer's request for this file as it is now (File tab only).
    var openRequest: FileRequest {
        FileRequest(id: "changes|" + path, path: path, summary: path)
    }
}

extension ChangesDisplay {
    static func countLabel(additions: Int, deletions: Int) -> String {
        "\(additions) \(additions == 1 ? "addition" : "additions"), \(deletions) \(deletions == 1 ? "deletion" : "deletions")"
    }
}

/// Why a Changes request failed, as the screen shows it.
struct ChangesError: Equatable {
    var title: String
    var message: String
    var symbol: String
    var canRetry: Bool

    init(_ error: Error) {
        switch error as? RelayError {
        case .http(404, _, _):
            self.init("Nothing to show", "The bridge doesn't know this agent's project folder, or it's gone.", "questionmark.folder", retry: true)
        case .http(503, _, _):
            self.init("git isn't installed", "Install git on that machine to see changes here.", "wrench.and.screwdriver", retry: true)
        case .http(504, _, _):
            self.init("git took too long", "The repository may be busy or very large. Try again in a moment.", "hourglass", retry: true)
        case .http(403, _, _):
            self.init("Outside the project", "Relay only shows changes inside the agent's project folder.", "lock.doc", retry: false)
        case .unauthorized:
            self.init("Couldn't load changes", error.localizedDescription, "lock", retry: false)
        default:
            self.init("Couldn't load changes", error.localizedDescription, "wifi.exclamationmark", retry: true)
        }
    }

    private init(_ title: String, _ message: String, _ symbol: String, retry: Bool) {
        self.title = title
        self.message = message
        self.symbol = symbol
        canRetry = retry
    }
}
