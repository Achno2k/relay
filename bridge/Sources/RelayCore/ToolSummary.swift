import Foundation

/// One-line human summaries for tool calls ("Edited api.md", "Ran swift test").
public enum ToolSummary {
    public static let inputLimit = 1000
    public static let previewLimit = 400

    public static func summary(name: String, input: [String: Any], scrubber: PathScrubber) -> String {
        func str(_ keys: String...) -> String? {
            for k in keys {
                if let v = input[k] as? String, !v.isEmpty { return v }
            }
            return nil
        }
        func path(_ keys: String...) -> String? {
            for k in keys {
                if let v = input[k] as? String, !v.isEmpty { return scrubber.scrub(v) }
            }
            return nil
        }
        let raw: String
        switch name.lowercased() {
        case "edit", "multiedit":
            raw = path("file_path", "path").map { "Edited \($0)" } ?? "Edited a file"
        case "write":
            raw = path("file_path", "path").map { "Wrote \($0)" } ?? "Wrote a file"
        case "read":
            raw = path("file_path", "path").map { "Read \($0)" } ?? "Read a file"
        case "notebookedit":
            raw = path("notebook_path").map { "Edited \($0)" } ?? "Edited a notebook"
        case "bash":
            raw = str("command").map { "Ran \(firstLine($0))" } ?? "Ran a command"
        case "grep":
            raw = str("pattern").map { "Searched for \($0)" } ?? "Searched"
        case "glob", "find", "ls":
            raw = (str("pattern") ?? path("path")).map { "Listed \($0)" } ?? "Listed files"
        case "webfetch":
            raw = str("url").map { "Fetched \($0)" } ?? "Fetched a page"
        case "websearch":
            raw = str("query").map { "Searched the web for \($0)" } ?? "Searched the web"
        case "task", "agent":
            raw = str("description").map { "Ran agent: \($0)" } ?? "Ran an agent"
        case "todowrite":
            raw = "Updated todos"
        case "skill":
            raw = str("skill", "command").map { "Used skill \($0)" } ?? "Used a skill"
        case "toolsearch":
            raw = "Loaded tools"
        default:
            raw = name
        }
        return truncate(scrubber.scrub(raw), 120)
    }

    public static func inputString(_ input: Any, scrubber: PathScrubber) -> String {
        guard JSONSerialization.isValidJSONObject(input),
              let data = try? JSONSerialization.data(withJSONObject: input, options: [.sortedKeys, .withoutEscapingSlashes]),
              let s = String(data: data, encoding: .utf8)
        else { return "{}" }
        return truncate(scrubber.scrub(s), inputLimit)
    }

    public static func preview(_ text: String, scrubber: PathScrubber) -> String {
        truncate(scrubber.scrub(text), previewLimit)
    }

    static func firstLine(_ s: String) -> String {
        let line = s.split(separator: "\n", maxSplits: 1, omittingEmptySubsequences: true).first.map(String.init) ?? s
        return line.trimmingCharacters(in: .whitespaces)
    }

    static func truncate(_ s: String, _ n: Int) -> String {
        s.count <= n ? s : String(s.prefix(n)) + "…"
    }
}
