import RelayKit
import SwiftUI

struct UserBubble: View {
    let text: String
    let pending: Bool
    let maxWidth: CGFloat
    var attachments: [AttachmentRef] = []
    var agentId: String = ""
    var store: AppStore?

    var body: some View {
        VStack(alignment: .trailing, spacing: 6) {
            if !attachments.isEmpty, let store {
                MessageAttachments(attachments: attachments, agentId: agentId, store: store)
            }
            if !text.isEmpty {
                Text(MarkdownParser.inline(text))
                    .padding(.horizontal, 16)
                    .padding(.vertical, 10)
                    .background(Color(.secondarySystemFill), in: .rect(cornerRadius: 22))
                    .textSelection(.enabled)
            }
        }
        .opacity(pending ? 0.6 : 1)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("userBubble")
        .accessibilityValue(pending ? "pending" : "sent")
        .frame(maxWidth: maxWidth, alignment: .trailing)
        .frame(maxWidth: .infinity, alignment: .trailing)
    }
}

/// Collapsed "Thought for a few seconds ›", like ChatGPT's reasoning row.
struct ThinkingRow: View {
    let text: String
    @State private var expanded = false

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            DisclosureLabel(title: "Thought for a few seconds", expanded: expanded) {
                withAnimation(.snappy) { expanded.toggle() }
            }
            if expanded {
                Text(text)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .padding(.leading, 12)
                    .overlay(alignment: .leading) { Capsule().fill(.quaternary).frame(width: 2) }
                    .transition(.opacity.combined(with: .move(edge: .top)))
            }
        }
    }
}

/// One row for a run of tool calls: "Worked for 42s ›", or "Running Bash…" while live.
struct ToolGroupView: View {
    let steps: [ToolStep]
    let isLive: Bool
    let duration: TimeInterval?
    @State private var expanded: Bool

    init(steps: [ToolStep], isLive: Bool, duration: TimeInterval?, expanded: Bool = false) {
        self.steps = steps
        self.isLive = isLive
        self.duration = duration
        _expanded = State(initialValue: expanded)
    }

    private var title: String {
        if isLive, let current = steps.last(where: { !$0.finished }) ?? steps.last {
            return "Running \(current.name)…"
        }
        if let duration { return "Worked for \(RelativeTime.duration(duration))" }
        return steps.count == 1 ? "Used 1 tool" : "Used \(steps.count) tools"
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            DisclosureLabel(title: title, expanded: expanded, live: isLive) {
                withAnimation(.snappy) { expanded.toggle() }
            }
            if expanded {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(Array(steps.enumerated()), id: \.element.id) { i, step in
                        ToolStepRow(step: step, isLast: i == steps.count - 1, isLive: isLive)
                    }
                }
                .transition(.opacity.combined(with: .move(edge: .top)))
            }
        }
        .sensoryFeedback(.selection, trigger: expanded)
    }
}

private struct ToolStepRow: View {
    let step: ToolStep
    let isLast: Bool
    let isLive: Bool
    @State private var showPreview = false

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            VStack(spacing: 4) {
                Image(systemName: ToolIcon.symbol(for: step.name))
                    .font(.footnote.weight(.medium))
                    .frame(width: 28, height: 28)
                    .background(step.isError ? Color.red.opacity(0.12) : Color(.tertiarySystemFill), in: .circle)
                    .foregroundStyle(step.isError ? .red : .secondary)
                if !isLast {
                    Rectangle().fill(.quaternary).frame(width: 1.5).frame(maxHeight: .infinity)
                }
            }
            VStack(alignment: .leading, spacing: 6) {
                HStack(spacing: 6) {
                    Text(step.summary.isEmpty ? step.name : step.summary)
                        .font(.subheadline)
                        .foregroundStyle(step.isError ? .red : .primary)
                        .lineLimit(2)
                    if !step.finished && isLive {
                        ProgressView().controlSize(.mini)
                    }
                }
                if showPreview || step.isError, let preview = step.preview, !preview.isEmpty {
                    Text(preview)
                        .font(.system(.caption, design: .monospaced))
                        .foregroundStyle(step.isError ? .red.opacity(0.85) : .secondary)
                        .lineLimit(6)
                        .padding(10)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 10))
                }
            }
            .padding(.top, 5)
            .padding(.bottom, isLast ? 0 : 14)
        }
        .contentShape(.rect)
        .onTapGesture { withAnimation(.snappy) { showPreview.toggle() } }
    }
}

/// Secondary label with a chevron that rotates when expanded.
struct DisclosureLabel: View {
    let title: String
    let expanded: Bool
    var live = false
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 4) {
                if live {
                    Text(title).shimmer()
                } else {
                    Text(title).foregroundStyle(.secondary)
                }
                Image(systemName: "chevron.right")
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(.tertiary)
                    .rotationEffect(.degrees(expanded ? 90 : 0))
            }
            .font(.subheadline.weight(.medium))
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
    }
}

/// Small centred marker where a turn was stopped.
struct StoppedMarker: View {
    var body: some View {
        HStack(spacing: 10) {
            line
            Label("Stopped", systemImage: "stop.circle")
                .font(.caption.weight(.medium))
                .foregroundStyle(.secondary)
                .fixedSize()
            line
        }
        .frame(maxWidth: .infinity)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("stoppedMarker")
    }

    private var line: some View {
        Rectangle().fill(.quaternary).frame(height: 0.5).frame(maxWidth: 48)
    }
}
