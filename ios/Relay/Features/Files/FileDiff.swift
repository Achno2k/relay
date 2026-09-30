import Foundation

/// What a file-changing tool call did, as the viewer draws it (from `toolCall.edit`, api.md "Tool call files and edits").
enum FileChange: Hashable {
    /// Edit / MultiEdit: string replacements, in order.
    case edits([Replacement])
    /// Write: the whole content, drawn as all-added.
    case write(String)
    /// codex FileChange: a unified diff, possibly for several files.
    case diff(String)

    struct Replacement: Hashable {
        var old: String
        var new: String
        var replaceAll = false
    }
}

/// One row of the code view: a file line, a diff line, or a header between hunks and files.
struct CodeLine: Hashable, Identifiable {
    enum Kind: Hashable { case context, added, removed, hunk, file }

    let id: Int
    var kind: Kind
    var text: String
    /// The line number shown in the gutter: the new side's for context and added lines, the old side's for removed ones.
    var number: Int?
}

enum FileDiff {
    /// Lines longer than this are cut for display; a minified file would otherwise lay out one huge `Text`.
    static let maxLineLength = 4000

    /// A text file as numbered lines.
    static func fileLines(_ content: String) -> [CodeLine] {
        split(content).enumerated().map { i, text in
            CodeLine(id: i, kind: .context, text: clip(text), number: i + 1)
        }
    }

    /// The rows for a change. `file` is the file as it is now, when loaded: it places an edit's lines at their
    /// real line numbers. Without it (or when the new text isn't found) the rows have no numbers.
    static func lines(for change: FileChange, file: String? = nil) -> [CodeLine] {
        var out = Builder()
        switch change {
        case .write(let content):
            for (i, text) in split(content).enumerated() {
                out.add(.added, text, number: i + 1)
            }
        case .edits(let replacements):
            for (i, r) in replacements.enumerated() {
                let start = file.flatMap { startLine(of: r.new, in: $0) }
                if replacements.count > 1 || start != nil {
                    let place = start.map { "Line \($0)" }
                    let label = replacements.count > 1 ? "Change \(i + 1) of \(replacements.count)" : nil
                    out.add(.hunk, [label, place].compactMap { $0 }.joined(separator: " · "))
                }
                for line in lineDiff(old: split(r.old), new: split(r.new), start: start) {
                    out.add(line.kind, line.text, number: line.number)
                }
            }
        case .diff(let text):
            for line in parseUnified(text) {
                out.add(line.kind, line.text, number: line.number)
            }
        }
        return out.lines
    }

    /// Removed lines before added ones in each changed block, unchanged lines as context. `start` is the new side's
    /// first line number; old and new sides share it, since an edit replaces text in place.
    static func lineDiff(old: [String], new: [String], start: Int?) -> [CodeLine] {
        let diff = new.difference(from: old)
        var removed = Set<Int>()
        var inserted = Set<Int>()
        for change in diff {
            switch change {
            case .remove(let offset, _, _): removed.insert(offset)
            case .insert(let offset, _, _): inserted.insert(offset)
            }
        }
        var out = Builder()
        var i = 0, j = 0
        while i < old.count || j < new.count {
            if i < old.count, removed.contains(i) {
                out.add(.removed, old[i], number: start.map { $0 + i })
                i += 1
            } else if j < new.count, inserted.contains(j) {
                out.add(.added, new[j], number: start.map { $0 + j })
                j += 1
            } else {
                // Unchanged on both sides (the difference guarantees i and j stay in step here).
                out.add(.context, j < new.count ? new[j] : old[i], number: start.map { $0 + j })
                i += 1
                j += 1
            }
        }
        return out.lines
    }

    /// A unified diff (`--- a/x`, `+++ b/x`, `@@ -1,3 +1,4 @@`) as rows. Each file gets a `.file` header with its
    /// path; hunk headers set the line numbers that follow. Lines the format doesn't explain are shown as context.
    static func parseUnified(_ text: String) -> [CodeLine] {
        let raw = split(text)
        var out = Builder()
        var oldNo: Int?
        var newNo: Int?
        var k = 0
        while k < raw.count {
            let line = raw[k]
            if line.hasPrefix("--- "), k + 1 < raw.count, raw[k + 1].hasPrefix("+++ ") {
                let newPath = headerPath(raw[k + 1])
                out.add(.file, newPath == "/dev/null" ? headerPath(line) : newPath)
                oldNo = nil
                newNo = nil
                k += 2
                continue
            }
            if line.hasPrefix("diff --git ") || line.hasPrefix("index ") || line.hasPrefix("\\ ") {
                k += 1
                continue
            }
            if line.hasPrefix("@@"), let (o, n) = hunkStarts(line) {
                oldNo = o
                newNo = n
                let trailing = line.components(separatedBy: "@@").dropFirst(2).joined(separator: "@@")
                    .trimmingCharacters(in: .whitespaces)
                out.add(.hunk, trailing.isEmpty ? "Line \(n)" : "Line \(n) · \(trailing)")
            } else if line.hasPrefix("+") {
                out.add(.added, String(line.dropFirst()), number: newNo)
                newNo = newNo.map { $0 + 1 }
            } else if line.hasPrefix("-") {
                out.add(.removed, String(line.dropFirst()), number: oldNo)
                oldNo = oldNo.map { $0 + 1 }
            } else {
                out.add(.context, line.hasPrefix(" ") ? String(line.dropFirst()) : line, number: newNo)
                oldNo = oldNo.map { $0 + 1 }
                newNo = newNo.map { $0 + 1 }
            }
            k += 1
        }
        return out.lines
    }

    /// 1-based line where `text` starts in `file`, if it's there.
    static func startLine(of text: String, in file: String) -> Int? {
        guard !text.isEmpty, let range = file.range(of: text) else { return nil }
        return file[..<range.lowerBound].reduce(1) { $1.isNewline ? $0 + 1 : $0 }
    }

    /// Lines without their terminators. A trailing newline doesn't make an extra empty line; an empty string has none.
    static func split(_ text: String) -> [String] {
        guard !text.isEmpty else { return [] }
        // `\r\n` is one Character in Swift, so split on any newline, not on "\n".
        var lines = text.split(omittingEmptySubsequences: false, whereSeparator: \.isNewline).map(String.init)
        if lines.last == "" { lines.removeLast() }
        return lines
    }

    private static func clip(_ line: String) -> String {
        line.count > maxLineLength ? String(line.prefix(maxLineLength)) + " …" : line
    }

    private static func headerPath(_ line: String) -> String {
        var path = String(line.dropFirst(4))
        // Git appends a tab and a timestamp in some modes.
        if let tab = path.firstIndex(of: "\t") { path = String(path[..<tab]) }
        if path.hasPrefix("a/") || path.hasPrefix("b/") { path.removeFirst(2) }
        return path
    }

    /// `@@ -12,3 +12,4 @@` → (12, 12).
    private static func hunkStarts(_ line: String) -> (Int, Int)? {
        let parts = line.split(separator: " ")
        guard parts.count >= 3,
              let old = parts.first(where: { $0.hasPrefix("-") }),
              let new = parts.first(where: { $0.hasPrefix("+") }),
              let o = Int(old.dropFirst().split(separator: ",").first ?? ""),
              let n = Int(new.dropFirst().split(separator: ",").first ?? "") else { return nil }
        return (o, n)
    }

    private struct Builder {
        var lines: [CodeLine] = []

        mutating func add(_ kind: CodeLine.Kind, _ text: String, number: Int? = nil) {
            lines.append(CodeLine(id: lines.count, kind: kind, text: FileDiff.clip(text), number: number))
        }
    }
}
