import SwiftUI
import UIKit

/// Numbered monospaced lines, with diff colours. Scrolls sideways unless `wrap`; lazy, so a 1 MB file opens fast.
struct CodeView: View {
    let lines: [CodeLine]
    let wrap: Bool
    /// Notices above the first line (truncated, failed, …).
    var header: AnyView?

    /// Digits of the largest line number, so the gutter doesn't jump while scrolling.
    private var gutterDigits: Int {
        max(2, String(lines.compactMap(\.number).max() ?? 0).count)
    }

    private var hasSigns: Bool {
        lines.contains { $0.kind == .added || $0.kind == .removed }
    }

    var body: some View {
        GeometryReader { proxy in
            let width = wrap ? proxy.size.width : max(proxy.size.width, contentWidth)
            ScrollView(wrap ? .vertical : [.vertical, .horizontal]) {
                LazyVStack(alignment: .leading, spacing: 0) {
                    if let header {
                        header
                            .padding(.horizontal, 16)
                            .padding(.vertical, 10)
                            .frame(width: proxy.size.width, alignment: .leading)
                    }
                    ForEach(lines) { line in
                        CodeLineRow(line: line, gutterDigits: gutterDigits, showsSign: hasSigns, wrap: wrap)
                            .frame(width: width, alignment: .leading)
                    }
                }
                .textSelection(.enabled)
                .padding(.vertical, 8)
                // A 2D scroll view centres content smaller than itself; code starts at the top left.
                .frame(minHeight: proxy.size.height, alignment: .topLeading)
            }
            .scrollBounceBehavior(.basedOnSize, axes: .horizontal)
        }
    }

    /// Every row gets the longest line's width, so diff colours run the full width and the gutter never
    /// slides off screen. Measured from the monospaced font the rows use, at the current text size.
    private var contentWidth: CGFloat {
        let size = UIFont.preferredFont(forTextStyle: .caption1).pointSize
        let digit = ("0" as NSString).size(withAttributes: [.font: UIFont.monospacedSystemFont(ofSize: size, weight: .regular)]).width
        let longest = lines.lazy.filter { $0.kind != .file && $0.kind != .hunk }.map { $0.text.count + $0.text.filter { $0 == "\t" }.count * 3 }.max() ?? 0
        let gutter = CGFloat(gutterDigits) * digit + 10 + (hasSigns ? digit + 10 : 0)
        return 24 + gutter + CGFloat(longest) * digit + digit
    }
}

private struct CodeLineRow: View {
    let line: CodeLine
    let gutterDigits: Int
    let showsSign: Bool
    let wrap: Bool

    var body: some View {
        switch line.kind {
        case .file:
            Label(line.text, systemImage: "doc.text")
                .font(.footnote.weight(.semibold))
                .lineLimit(1)
                .padding(.horizontal, 16)
                .padding(.top, line.id == 0 ? 4 : 18)
                .padding(.bottom, 6)
                .frame(maxWidth: .infinity, alignment: .leading)
                .accessibilityIdentifier("diffFile")
        case .hunk:
            Text(line.text)
                .font(.caption.monospaced())
                .foregroundStyle(.secondary)
                .lineLimit(1)
                .padding(.horizontal, 16)
                .padding(.vertical, 5)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Color(.tertiarySystemFill))
                .padding(.vertical, 4)
                .accessibilityIdentifier("diffHunk")
        case .context, .added, .removed:
            HStack(alignment: .firstTextBaseline, spacing: 10) {
                Text(number)
                    .foregroundStyle(.tertiary)
                    .accessibilityHidden(true)
                if showsSign {
                    Text(sign)
                        .foregroundStyle(signColor)
                        .accessibilityHidden(true)
                }
                Text(display)
                    .foregroundStyle(.primary)
                    .lineLimit(wrap ? nil : 1)
                    .fixedSize(horizontal: false, vertical: true)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            .font(.caption.monospaced())
            .padding(.horizontal, 12)
            .padding(.vertical, 1)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(background)
            .accessibilityElement(children: .combine)
            .accessibilityLabel(accessibilityText)
            .accessibilityIdentifier(line.kind == .context ? "codeLine" : "diffLine")
        }
    }

    /// Right-aligned in a fixed width of figure spaces, so every row's code starts in the same column.
    private var number: String {
        let n = line.number.map(String.init) ?? ""
        return String(repeating: "\u{2007}", count: max(0, gutterDigits - n.count)) + n
    }

    private var sign: String {
        switch line.kind {
        case .added: "+"
        case .removed: "−"
        default: " "
        }
    }

    private var signColor: Color { line.kind == .added ? .green : .red }

    /// Tabs as four spaces, and an empty line still one line tall.
    private var display: String {
        line.text.isEmpty ? " " : line.text.replacingOccurrences(of: "\t", with: "    ")
    }

    private var background: Color {
        switch line.kind {
        case .added: .green.opacity(0.14)
        case .removed: .red.opacity(0.14)
        default: .clear
        }
    }

    private var accessibilityText: String {
        let prefix = switch line.kind {
        case .added: "Added"
        case .removed: "Removed"
        default: line.number.map { "Line \($0)" } ?? ""
        }
        return "\(prefix): \(line.text)"
    }
}
