import Foundation

/// A tool call as it shows on screen, before the transcript has it.
public struct LiveToolCall: Sendable, Equatable {
    /// The name the transcript's `toolCall` will carry for this kind (`Bash`, `Shell`, `bash`, …).
    public var name: String
    /// What the screen shows of the arguments, keyed like the real tool input (`command`, `file_path`, …).
    public var input: [String: String]
    /// A summary that doesn't come from `ToolSummary` (codex builds its own, e.g. `Ran <cmd>`).
    public var fixedSummary: String?
    /// The screen doesn't show the arguments yet, so the summary is only the generic one for `name`.
    public var generic: Bool

    public init(name: String, input: [String: String] = [:], fixedSummary: String? = nil, generic: Bool = false) {
        self.name = name
        self.input = input
        self.fixedSummary = fixedSummary
        self.generic = generic
    }

    /// Built and scrubbed the same way as the transcript's `toolCall.summary`.
    public func summary(scrubber: PathScrubber) -> String {
        if let fixedSummary { return ToolSummary.truncate(scrubber.scrub(fixedSummary), 120) }
        return ToolSummary.summary(name: name, input: input, scrubber: scrubber)
    }
}

/// What a pane's visible screen shows of the current turn: the assistant prose being written and
/// the tool that's running. Tool text is never part of `text`.
public struct LiveScreen: Sendable, Equatable {
    public var text: String?
    public var tool: LiveToolCall?

    public init(text: String? = nil, tool: LiveToolCall? = nil) {
        self.text = text
        self.tool = tool
    }
}

/// Pulls the in-progress assistant text and the running tool out of a pane's visible screen, per
/// agent kind. Input is already ANSI-stripped. Only the current turn counts: anything above the
/// user's last prompt on screen is ignored. Paths are not scrubbed here; callers do that.
public enum LiveReplyParser {
    public static func parse(screen: String, kind: String) -> LiveScreen {
        let lines = screen.components(separatedBy: "\n").map(stripInlineBanner)
        switch kind {
        case "claude": return claude(lines)
        case "codex": return codex(lines)
        case "pi": return pi(lines)
        default: return LiveScreen()
        }
    }

    /// Just the prose, for callers that don't care about tools.
    public static func extract(screen: String, kind: String) -> String? {
        parse(screen: screen, kind: kind).text
    }

    // MARK: Claude

    private static let claudeSpinners: Set<Character> = ["✢", "✻", "✽", "✳", "✶", "✺", "✵"]

    private enum BlockKind { case text, tool }
    private struct Block {
        var kind: BlockKind
        var lines: [String]
    }

    private static func claude(_ lines: [String]) -> LiveScreen {
        let region = claudeTurn(lines)
        var blocks: [Block] = []
        for chunk in chunks(region) {
            let first = chunk[0].trimmingCharacters(in: .whitespaces)
            let marked = first.hasPrefix("⏺")
            var content = chunk
            if marked { content[0] = String(first.dropFirst()).trimmingCharacters(in: .whitespaces) }
            let tool = isClaudeTool(content)
            if marked || tool || blocks.isEmpty {
                // A running tool's `⏺` blinks, so a tool block can start without it.
                let lines = marked || tool ? content : content.filter { !isClaudeChrome($0) }
                guard !lines.isEmpty else { continue }
                blocks.append(Block(kind: tool ? .tool : .text, lines: lines))
            } else {
                // A later paragraph of the same text block, or more output of the same tool.
                blocks[blocks.count - 1].lines += [""] + content
            }
        }
        let text = blocks.last(where: { $0.kind == .text }).flatMap { nonEmpty(joinBlock($0.lines)) }
        let tool = blocks.last.flatMap { $0.kind == .tool ? claudeToolCall($0.lines) : nil }
        return LiveScreen(text: text, tool: tool)
    }

    /// The lines of the current turn: after the user's last prompt echo (and its wrapped lines),
    /// before the spinner and the input box. The whole screen when the echo has scrolled off.
    private static func claudeTurn(_ lines: [String]) -> [String] {
        var end = lines.count
        // The input box: a rule with the `❯` prompt line right under it.
        for i in stride(from: lines.count - 1, through: 0, by: -1) where isRule(lines[i].trimmingCharacters(in: .whitespaces)) {
            if let next = lines[(i + 1)...].first(where: { !$0.trimmingCharacters(in: .whitespaces).isEmpty }),
               next.trimmingCharacters(in: .whitespaces).hasPrefix("❯") {
                end = i
                break
            }
        }
        var start = 0
        if let echo = lines[..<end].lastIndex(where: { $0.trimmingCharacters(in: .whitespaces).hasPrefix("❯") }) {
            start = echo + 1
            while start < end, !lines[start].trimmingCharacters(in: .whitespaces).isEmpty { start += 1 }
        }
        guard start < end else { return [] }
        var region = Array(lines[start..<end])
        if let spinner = region.firstIndex(where: isClaudeSpinner) { region = Array(region[..<spinner]) }
        if let rule = region.firstIndex(where: { isRule($0.trimmingCharacters(in: .whitespaces)) }) { region = Array(region[..<rule]) }
        return region
    }

    /// Spinner/status lines: `✻ Crafting… (3s)`, `· Crafting…`, `✻ Cooked for 3s · done`.
    private static func isClaudeSpinner(_ line: String) -> Bool {
        let t = line.trimmingCharacters(in: .whitespaces)
        guard let first = t.first, t.dropFirst().first == " " else { return false }
        if claudeSpinners.contains(first) { return true }
        return (first == "·" || first == "*") && t.contains("…")
    }

    private static func isClaudeTool(_ content: [String]) -> Bool {
        if content.contains(where: { $0.trimmingCharacters(in: .whitespaces).hasPrefix("⎿") }) { return true }
        let header = claudeHeader(content)
        return callSignature(header) != nil || claudeGroupLabel(header) != nil
    }

    /// The block's first lines up to its `⎿` output, rejoined, with a trailing `· 3s` timer dropped.
    private static func claudeHeader(_ content: [String]) -> String {
        let head = content.prefix { !$0.trimmingCharacters(in: .whitespaces).hasPrefix("⎿") && !$0.trimmingCharacters(in: .whitespaces).isEmpty }
        let joined = head.map { $0.trimmingCharacters(in: .whitespaces) }.joined(separator: " ")
        return replacing(#"\s+·\s+\d+(?:m\s*\d+)?s$"#, in: joined, with: "")
    }

    /// Claude's display names that differ from the transcript's tool name.
    private static let claudeDisplayNames = [
        "Update": "Edit", "Search": "Grep", "Fetch": "WebFetch", "Web Search": "WebSearch", "Task": "Agent",
    ]
    private static let claudePathTools: Set<String> = ["Read", "Write", "Edit", "MultiEdit", "NotebookEdit"]

    private static func claudeToolCall(_ content: [String]) -> LiveToolCall {
        // `⎿  $ ping -c 8 127.0.0.1 (3s · 5 lines)`: the command itself, the most specific source.
        if let dollar = content.first(where: { $0.trimmingCharacters(in: .whitespaces).hasPrefix("⎿  $ ") || $0.trimmingCharacters(in: .whitespaces).hasPrefix("⎿ $ ") }) {
            let t = dollar.trimmingCharacters(in: .whitespaces).dropFirst().trimmingCharacters(in: .whitespaces).dropFirst(2)
            let command = replacing(#"\s+\(\d+(?:m\s*\d+)?s(?:\s+·\s+\d+ lines?)?\)$"#, in: String(t), with: "")
            if !command.isEmpty { return LiveToolCall(name: "Bash", input: ["command": command]) }
        }
        let header = claudeHeader(content)
        if let (display, args) = callSignature(header) {
            let name = claudeDisplayNames[display] ?? display
            let arg = unquote(args)
            switch name {
            case "Bash": return LiveToolCall(name: name, input: ["command": arg])
            case _ where claudePathTools.contains(name):
                return LiveToolCall(name: name, input: [name == "NotebookEdit" ? "notebook_path" : "file_path": arg])
            case "Grep", "Glob":
                let pattern = keyed(args, "pattern") ?? arg
                return LiveToolCall(name: name, input: ["pattern": pattern])
            case "WebFetch": return LiveToolCall(name: name, input: ["url": arg])
            case "WebSearch": return LiveToolCall(name: name, input: ["query": arg])
            case "Agent": return LiveToolCall(name: name, input: ["description": arg])
            case "Skill": return LiveToolCall(name: name, input: ["skill": arg])
            default: return LiveToolCall(name: name, generic: true)
            }
        }
        if let name = claudeGroupLabel(header) { return LiveToolCall(name: name, generic: true) }
        return LiveToolCall(name: "Tool", fixedSummary: header.isEmpty ? "Tool" : header, generic: true)
    }

    /// Claude's grouped tool labels, e.g. `Running 1 shell command…`, `Read 3 files`,
    /// `Searched for 2 patterns, read 1 file`. Returns the transcript name of the first tool.
    private static func claudeGroupLabel(_ header: String) -> String? {
        let pattern = #"^(Running|Ran|Reading|Read|Searching for|Searched for|Searching|Searched|Listing|Listed|Editing|Edited|Updating|Updated|Writing|Wrote|Creating|Created|Fetching|Fetched) \d+ (?:shell )?(?:commands?|files?|patterns?|director(?:y|ies)|pages?|searches|queries|agents?|tasks?|tools?)\b[^\n]*$"#
        guard let verb = firstGroup(pattern, header) else { return nil }
        switch verb {
        case "Running", "Ran": return "Bash"
        case "Reading", "Read": return "Read"
        case "Searching for", "Searched for", "Searching", "Searched": return "Grep"
        case "Listing", "Listed": return "Glob"
        case "Editing", "Edited", "Updating", "Updated": return "Edit"
        case "Writing", "Wrote", "Creating", "Created": return "Write"
        default: return "WebFetch"
        }
    }

    private static let claudeFooterMarkers = ["Update available!", "auto mode on"]

    private static func isClaudeChrome(_ line: String) -> Bool {
        let t = line.trimmingCharacters(in: .whitespaces)
        if t.isEmpty { return false }  // blank lines are paragraph separators, not chrome
        if isRule(t) { return true }
        if t.hasPrefix("❯") { return true }
        if t.hasPrefix("⎿") { return true }
        if isClaudeSpinner(t) { return true }
        if claudeFooterMarkers.contains(where: { t.contains($0) }) { return true }
        if isTokenStats(t) { return true }
        return false
    }

    // MARK: Codex

    /// Headers of codex's tool cells (`• Ran ping …`, `• Explored`, `• Edited App.swift (+2 -1)`).
    private static let codexToolHeads = [
        "Running ", "Ran ", "Explored", "Exploring", "Edited ", "Editing ", "Added ", "Deleted ", "Called ",
        "Calling ", "Waiting", "Waited", "Searched ", "Searching ", "Updated Plan", "Viewed Image", "Read ",
    ]

    private static func codex(_ lines: [String]) -> LiveScreen {
        let region = codexTurn(lines)
        var blocks: [Block] = []
        var inBlock = false
        for line in region {
            let t = line.trimmingCharacters(in: .whitespaces)
            if t.hasPrefix("• ") || t.hasPrefix("◦ ") {
                let first = String(t.dropFirst(2))
                if first.hasPrefix("Working (") || first.hasPrefix("Model changed") { inBlock = false; continue }
                let tool = codexToolHeads.contains(where: { first.hasPrefix($0) })
                blocks.append(Block(kind: tool ? .tool : .text, lines: [first]))
                inBlock = true
            } else if isCodexStop(t) {
                inBlock = false
            } else if inBlock {
                blocks[blocks.count - 1].lines.append(line)
                if t.hasPrefix("└") { blocks[blocks.count - 1].kind = .tool }
            }
        }
        let text = blocks.last(where: { $0.kind == .text }).flatMap { nonEmpty(joinBlock($0.lines)) }
        let tool = blocks.last.flatMap { $0.kind == .tool ? codexToolCall($0.lines[0]) : nil }
        return LiveScreen(text: text, tool: tool)
    }

    /// After the user's last prompt echo, before the input line. The input is the last `›` line;
    /// the echo is the `›` line above it.
    private static func codexTurn(_ lines: [String]) -> [String] {
        let prompts = lines.indices.filter { lines[$0].trimmingCharacters(in: .whitespaces).hasPrefix("›") }
        guard let input = prompts.last else { return lines }
        let start = prompts.dropLast().last.map { $0 + 1 } ?? 0
        return start < input ? Array(lines[start..<input]) : []
    }

    private static func codexToolCall(_ header: String) -> LiveToolCall {
        func rest(_ prefix: String) -> String { String(header.dropFirst(prefix.count)).trimmingCharacters(in: .whitespaces) }
        for verb in ["Running ", "Ran "] where header.hasPrefix(verb) {
            return LiveToolCall(name: "Shell", input: ["command": rest(verb)], fixedSummary: "Ran \(rest(verb))")
        }
        for verb in ["Edited ", "Editing ", "Added ", "Deleted "] where header.hasPrefix(verb) {
            let path = replacing(#"\s+\(\+\d+ -\d+\)$"#, in: rest(verb), with: "")
            return LiveToolCall(name: "Edit", input: ["path": path], fixedSummary: "Edited \(path)")
        }
        for verb in ["Called ", "Calling "] where header.hasPrefix(verb) {
            let name = String(rest(verb).prefix { $0 != "(" })
            return LiveToolCall(name: name, fixedSummary: "Called \(name)")
        }
        for verb in ["Searched ", "Searching "] where header.hasPrefix(verb) {
            return LiveToolCall(name: "WebSearch", input: ["query": rest(verb)], fixedSummary: "Searched the web for \(rest(verb))")
        }
        if header.hasPrefix("Explored") || header.hasPrefix("Exploring") || header.hasPrefix("Waiting") || header.hasPrefix("Waited")
            || header.hasPrefix("Read ") {
            return LiveToolCall(name: "Shell", generic: true)
        }
        return LiveToolCall(name: "Tool", fixedSummary: header, generic: true)
    }

    private static func isCodexStop(_ t: String) -> Bool {
        if t.hasPrefix("›") { return true }
        if t.hasPrefix("✗") || t.hasPrefix("✔") || t.hasPrefix("■") { return true }
        if isRule(t) { return true }
        if isTimestamp(t) { return true }
        if t.contains("background terminal") && t.contains("/ps") { return true }  // status line, not reply text
        return false
    }

    // MARK: pi

    private static func pi(_ lines: [String]) -> LiveScreen {
        let working = lines.contains(where: isPiSpinner)
        let content = lines.filter { !isPiChrome($0) }
        let lastTool = content.lastIndex(where: { piToolCall($0) != nil })

        var tool: LiveToolCall?
        if working, let lastTool {
            // Still running unless a `Took …` line closed its box.
            let after = content[(lastTool + 1)...]
            if !after.contains(where: { $0.trimmingCharacters(in: .whitespaces).hasPrefix("Took ") }) {
                tool = piToolCall(content[lastTool])
            }
        }
        // pi renders its reply in one piece, and never while its own `Working` spinner is up.
        guard !working else { return LiveScreen(text: nil, tool: tool) }
        // Prose comes after the last tool box: after its `Took …` line for bash, else after its header
        // (a read/edit box has no footer, so its output can't be told apart; the reply's last paragraph
        // is still prose in practice).
        var from = 0
        if let lastTool {
            let took = content[(lastTool + 1)...].firstIndex { $0.trimmingCharacters(in: .whitespaces).hasPrefix("Took ") }
            if let took {
                from = took + 1
            } else if piToolCall(content[lastTool])?.name == "bash" {
                return LiveScreen()
            } else {
                from = lastTool + 1
            }
        }
        let prose = content[from...].filter { !isPiToolFooter($0) }
        return LiveScreen(text: lastParagraph(Array(prose)).flatMap { nonEmpty(joinBlock($0)) })
    }

    /// pi's tool box headers: `$ cmd`, `read path:1-20`, `write path`, `edit path`, `ls path`,
    /// `grep /pattern/ in path`, `find pattern in path`.
    private static func piToolCall(_ line: String) -> LiveToolCall? {
        let t = line.trimmingCharacters(in: .whitespaces)
        if t.hasPrefix("$ ") {
            let command = replacing(#"\s+\(timeout \d+s\)$"#, in: String(t.dropFirst(2)), with: "")
            return command.isEmpty ? nil : LiveToolCall(name: "bash", input: ["command": command])
        }
        if let m = groups(#"^grep /(.*)/ in (\S+)"#, t) { return LiveToolCall(name: "grep", input: ["pattern": m[0], "path": m[1]]) }
        if let m = groups(#"^find (\S+) in (\S+)"#, t) { return LiveToolCall(name: "find", input: ["pattern": m[0], "path": m[1]]) }
        if let m = groups(#"^(read|write|edit|ls) (\S+)$"#, t) {
            let path = m[0] == "read" ? replacing(#":\d+(?:-\d*)?$"#, in: m[1], with: "") : m[1]
            return LiveToolCall(name: m[0], input: ["path": path])
        }
        return nil
    }

    private static func isPiToolFooter(_ line: String) -> Bool {
        let t = line.trimmingCharacters(in: .whitespaces)
        return t.hasPrefix("Took ") || t.hasPrefix("Elapsed ") || t.hasPrefix("... (")
    }

    private static func isPiSpinner(_ line: String) -> Bool {
        let t = line.trimmingCharacters(in: .whitespaces)
        return t.contains("Working") && t.contains("─")
    }

    private static let piChromePrefixes = [
        "Update Available", "New version", "Changelog:", "Model:", "Warning:", "Thinking level:",
        "Manage extra usage",
    ]

    private static func isPiChrome(_ line: String) -> Bool {
        let t = line.trimmingCharacters(in: .whitespaces)
        if t.isEmpty { return false }  // blank lines are paragraph separators, not chrome
        if isRule(t) { return true }
        if isPiSpinner(t) { return true }
        if piChromePrefixes.contains(where: { t.hasPrefix($0) }) { return true }
        if t.hasPrefix("$") && t.contains("(") && !t.hasPrefix("$ ") { return true }  // cost/token footer chip
        if t.contains("(auto)") || t.contains("(sub)") { return true }
        if t.hasPrefix("~") { return true }  // cwd + git branch footer line
        return false
    }

    // MARK: Shared helpers

    /// A tool-call signature, e.g. `Write(answer.txt)`, `Bash(ping -c 45 127.0.0.1)`,
    /// `Web Search("swift actors")`. Returns the display name and the raw arguments.
    private static func callSignature(_ text: String) -> (String, String)? {
        guard let m = groups(#"^([A-Za-z][A-Za-z0-9_.]*|Web Search)\((.*)\)$"#, text) else { return nil }
        return (m[0], m[1])
    }

    /// `pattern: "x", path: "y"` → the value for `key`.
    private static func keyed(_ args: String, _ key: String) -> String? {
        groups(key + #": "([^"]*)""#, args)?.first
    }

    private static func unquote(_ s: String) -> String {
        s.count >= 2 && s.hasPrefix("\"") && s.hasSuffix("\"") ? String(s.dropFirst().dropLast()) : s
    }

    /// Runs of non-blank lines.
    private static func chunks(_ lines: [String]) -> [[String]] {
        var out: [[String]] = []
        var current: [String] = []
        for line in lines {
            if line.trimmingCharacters(in: .whitespaces).isEmpty {
                if !current.isEmpty { out.append(current); current = [] }
            } else {
                current.append(line)
            }
        }
        if !current.isEmpty { out.append(current) }
        return out
    }

    private static func isRule(_ line: String) -> Bool {
        !line.isEmpty && line.allSatisfy { $0 == "─" || $0 == "-" }
    }

    private static func isTimestamp(_ line: String) -> Bool {
        groups(#"^(\d{1,2}:\d{2}\s?(?:AM|PM))$"#, line) != nil
    }

    /// Claude's footer token/usage line, e.g. `72.2k/1.0M · in 72.2k out 977 · 5h 10%(2h13m) · wk 11%(4d15h)`.
    private static func isTokenStats(_ line: String) -> Bool {
        groups(#"^([\d.]+[kM]?/[\d.]+[kM]?)\s*·"#, line) != nil
    }

    /// The last run of non-blank lines, i.e. the last paragraph. It's fine to show only the tail
    /// of a scrolled-off reply.
    private static func lastParagraph(_ lines: [String]) -> [String]? {
        var end = lines.count
        while end > 0, lines[end - 1].trimmingCharacters(in: .whitespaces).isEmpty { end -= 1 }
        guard end > 0 else { return nil }
        var start = end
        while start > 0, !lines[start - 1].trimmingCharacters(in: .whitespaces).isEmpty { start -= 1 }
        return Array(lines[start..<end])
    }

    /// Rejoins wrapped lines into flowing paragraphs; a blank line is kept as a paragraph break.
    private static func joinBlock(_ lines: [String]) -> String {
        var paragraphs: [String] = []
        var current: [String] = []
        for raw in lines {
            let line = raw.trimmingCharacters(in: .whitespaces)
            if line.isEmpty {
                if !current.isEmpty {
                    paragraphs.append(current.joined(separator: " "))
                    current = []
                }
            } else {
                current.append(line)
            }
        }
        if !current.isEmpty { paragraphs.append(current.joined(separator: " ")) }
        return paragraphs.joined(separator: "\n\n")
    }

    private static func nonEmpty(_ s: String) -> String? { s.isEmpty ? nil : s }

    /// The capture groups of the first match, or `nil`.
    private static func groups(_ pattern: String, _ text: String) -> [String]? {
        guard let re = try? NSRegularExpression(pattern: pattern),
              let m = re.firstMatch(in: text, range: NSRange(text.startIndex..., in: text))
        else { return nil }
        return (1..<m.numberOfRanges).map { i in
            Range(m.range(at: i), in: text).map { String(text[$0]) } ?? ""
        }
    }

    private static func firstGroup(_ pattern: String, _ text: String) -> String? {
        groups(pattern, text)?.first
    }

    private static func replacing(_ pattern: String, in text: String, with template: String) -> String {
        guard let re = try? NSRegularExpression(pattern: pattern) else { return text }
        return re.stringByReplacingMatches(in: text, range: NSRange(text.startIndex..., in: text), withTemplate: template)
    }

    /// Claude Code draws its "Update available!" banner at a fixed row that can land on the same
    /// visual row as flowing reply text once the alternate screen scrolls under it, so a clean read
    /// can show it concatenated onto real content. Best effort: drop everything from the banner on;
    /// it can't undo characters the two writes already interleaved on screen.
    private static func stripInlineBanner(_ line: String) -> String {
        guard let range = line.range(of: "Update available!") else { return line }
        return String(line[line.startIndex..<range.lowerBound])
    }
}
