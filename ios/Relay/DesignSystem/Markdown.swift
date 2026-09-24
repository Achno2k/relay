import SwiftUI
import UIKit

/// Block-level markdown on top of `AttributedString`'s inline parser: headings, lists, quotes,
/// rules and fenced code. Enough for agent output; no tables.
enum MarkdownBlock: Hashable {
    case heading(level: Int, text: String)
    case paragraph(String)
    case listItem(marker: String, indent: Int, text: String)
    case quote(String)
    case code(language: String?, code: String)
    case rule
}

enum MarkdownParser {
    static func parse(_ source: String) -> [MarkdownBlock] {
        var blocks: [MarkdownBlock] = []
        var paragraph: [String] = []
        var lines = source.components(separatedBy: "\n")[...]

        func flush() {
            if !paragraph.isEmpty { blocks.append(.paragraph(paragraph.joined(separator: "\n"))) }
            paragraph = []
        }

        while let line = lines.popFirst() {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if trimmed.hasPrefix("```") || trimmed.hasPrefix("~~~") {
                flush()
                let fence = String(trimmed.prefix(3))
                let language = trimmed.dropFirst(3).trimmingCharacters(in: .whitespaces)
                var code: [String] = []
                // An unclosed fence (still streaming) runs to the end.
                while let next = lines.popFirst() {
                    if next.trimmingCharacters(in: .whitespaces).hasPrefix(fence) { break }
                    code.append(next)
                }
                blocks.append(.code(language: language.isEmpty ? nil : language, code: code.joined(separator: "\n")))
            } else if trimmed.isEmpty {
                flush()
            } else if let h = heading(trimmed) {
                flush()
                blocks.append(h)
            } else if ["---", "***", "___"].contains(trimmed) {
                flush()
                blocks.append(.rule)
            } else if let item = listItem(line) {
                flush()
                blocks.append(item)
            } else if trimmed.hasPrefix(">") {
                flush()
                let text = trimmed.dropFirst().trimmingCharacters(in: .whitespaces)
                if case .quote(let prev) = blocks.last {
                    blocks[blocks.count - 1] = .quote(prev + "\n" + text)
                } else {
                    blocks.append(.quote(text))
                }
            } else if paragraph.isEmpty, case .listItem(let marker, let indent, let text) = blocks.last,
                      line.first == " " {
                // Indented continuation of a list item.
                blocks[blocks.count - 1] = .listItem(marker: marker, indent: indent, text: text + " " + trimmed)
            } else {
                paragraph.append(line)
            }
        }
        flush()
        return blocks
    }

    private static func heading(_ line: String) -> MarkdownBlock? {
        let hashes = line.prefix { $0 == "#" }.count
        guard (1...6).contains(hashes), line.dropFirst(hashes).first == " " else { return nil }
        return .heading(level: hashes, text: line.dropFirst(hashes + 1).trimmingCharacters(in: .whitespaces))
    }

    private static func listItem(_ line: String) -> MarkdownBlock? {
        let spaces = line.prefix { $0 == " " }.count
        let rest = line.dropFirst(spaces)
        if let first = rest.first, "-*+".contains(first), rest.dropFirst().first == " " {
            return .listItem(marker: "•", indent: spaces / 2, text: String(rest.dropFirst(2)))
        }
        let digits = rest.prefix { $0.isNumber }
        if !digits.isEmpty, digits.count < 4 {
            let after = rest.dropFirst(digits.count)
            if let dot = after.first, dot == "." || dot == ")", after.dropFirst().first == " " {
                return .listItem(marker: "\(digits).", indent: spaces / 2, text: String(after.dropFirst(2)))
            }
        }
        return nil
    }

    /// Inline markdown (bold, italic, code, links). Inline code gets a subtle fill; `codeFont`
    /// overrides its font where the surrounding text is large or bold.
    static func inline(_ text: String, codeFont: Font? = nil, codeBackground: Color = Color(.tertiarySystemFill)) -> AttributedString {
        let options = AttributedString.MarkdownParsingOptions(
            interpretedSyntax: .inlineOnlyPreservingWhitespace,
            failurePolicy: .returnPartiallyParsedIfPossible
        )
        var result = (try? AttributedString(markdown: text, options: options)) ?? AttributedString(text)
        let codeRanges = result.runs.compactMap { run in
            run.inlinePresentationIntent?.contains(.code) == true ? run.range : nil
        }
        for range in codeRanges {
            result[range].backgroundColor = codeBackground
            if let codeFont { result[range].font = codeFont }
        }
        return result
    }
}

struct MarkdownView: View, Equatable {
    /// Parsed in `body`, which SwiftUI skips while `source` is unchanged.
    let source: String

    init(_ source: String) {
        self.source = source
    }

    var body: some View {
        let blocks = MarkdownParser.parse(source)
        VStack(alignment: .leading, spacing: 12) {
            ForEach(Array(blocks.enumerated()), id: \.offset) { _, block in
                view(for: block)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    @ViewBuilder
    private func view(for block: MarkdownBlock) -> some View {
        switch block {
        case .heading(let level, let text):
            Text(MarkdownParser.inline(text))
                .font(level == 1 ? .title2.bold() : level == 2 ? .title3.bold() : .headline)
                .padding(.top, 4)
        case .paragraph(let text):
            Text(MarkdownParser.inline(text))
                .lineSpacing(3)
        case .listItem(let marker, let indent, let text):
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text(marker)
                    .monospacedDigit()
                    .foregroundStyle(marker == "•" ? .secondary : .primary)
                    .frame(minWidth: 14, alignment: .leading)
                Text(MarkdownParser.inline(text))
                    .lineSpacing(3)
            }
            .padding(.leading, CGFloat(indent) * 18)
        case .quote(let text):
            Text(MarkdownParser.inline(text))
                .foregroundStyle(.secondary)
                .padding(.leading, 12)
                .overlay(alignment: .leading) {
                    Capsule().fill(.quaternary).frame(width: 3)
                }
        case .code(let language, let code):
            CodeBlockView(language: language, code: code)
        case .rule:
            Divider()
        }
    }
}

struct CodeBlockView: View {
    let language: String?
    let code: String
    @State private var copied = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack {
                Text(language ?? "code")
                    .font(.caption.weight(.medium))
                    .foregroundStyle(.secondary)
                Spacer()
                Button {
                    UIPasteboard.general.string = code
                    copied = true
                    Task {
                        try? await Task.sleep(for: .seconds(1.5))
                        copied = false
                    }
                } label: {
                    Label(copied ? "Copied" : "Copy", systemImage: copied ? "checkmark" : "doc.on.doc")
                        .font(.caption.weight(.medium))
                        .contentTransition(.symbolEffect(.replace))
                        .padding(.vertical, 14)
                        .contentShape(.rect)
                }
                .buttonStyle(.plain)
                .foregroundStyle(.secondary)
                .sensoryFeedback(.success, trigger: copied) { _, new in new }
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 10)

            ScrollView(.horizontal, showsIndicators: false) {
                Text(code)
                    .font(.system(.callout, design: .monospaced))
                    .lineSpacing(2)
                    .textSelection(.enabled)
                    .fixedSize(horizontal: true, vertical: false)
                    .padding(.horizontal, 14)
                    .padding(.bottom, 14)
            }
        }
        .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 16))
        .overlay {
            RoundedRectangle(cornerRadius: 16).strokeBorder(Color(.separator).opacity(0.4), lineWidth: 0.5)
        }
    }
}
