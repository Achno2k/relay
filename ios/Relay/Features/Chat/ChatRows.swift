import RelayKit
import SwiftUI

struct UserBubble: View {
    let text: String
    let pending: Bool
    let maxWidth: CGFloat
    var attachments: [AttachmentRef] = []
    var agentId: String = ""
    var store: AppStore?
    /// The send failed (offline, bridge down): say so and offer retry and delete, never a silent dim.
    var failed = false
    var onRetry: () -> Void = {}
    var onDiscard: () -> Void = {}

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
            if failed {
                Button(action: onRetry) {
                    Label("Not sent. Tap to retry.", systemImage: "exclamationmark.circle.fill")
                        .font(.footnote.weight(.medium))
                        .foregroundStyle(.red)
                }
                .buttonStyle(.plain)
                .accessibilityIdentifier("retrySend")
            }
        }
        .opacity(pending && !failed ? 0.6 : 1)
        .contextMenu {
            if failed {
                Button("Retry", systemImage: "arrow.clockwise", action: onRetry)
                Button("Delete", systemImage: "trash", role: .destructive, action: onDiscard)
            }
        }
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("userBubble")
        .accessibilityValue(failed ? "not sent" : pending ? "pending" : "sent")
        .accessibilityActions {
            if failed {
                Button("Retry", action: onRetry)
                Button("Delete", action: onDiscard)
            }
        }
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
    /// For fetching result images; without a store the rows keep their `[image]` previews.
    var agentId = ""
    var store: AppStore?
    @State private var expanded: Bool

    init(steps: [ToolStep], isLive: Bool, duration: TimeInterval?, expanded: Bool = false, agentId: String = "", store: AppStore? = nil) {
        self.steps = steps
        self.isLive = isLive
        self.duration = duration
        self.agentId = agentId
        self.store = store
        _expanded = State(initialValue: expanded)
    }

    private var title: String {
        if isLive, let current = steps.last(where: { !$0.finished }) ?? steps.last {
            // The bridge names a tool it can't recognise "Tool".
            return current.name == "Tool" ? "Running a tool…" : "Running \(current.name)…"
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
                        ToolStepRow(step: step, isLast: i == steps.count - 1, isLive: isLive, agentId: agentId, store: store)
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
    let agentId: String
    let store: AppStore?
    @State private var showPreview = false
    /// The bridge answered 404 for the images (older bridge): back to the `[image]` preview.
    @State private var imagesUnavailable = false
    @Environment(\.openFile) private var openFile

    private var showsImages: Bool { !step.images.isEmpty && store != nil && !imagesUnavailable }

    /// Shown images replace their `[image]` placeholder lines.
    private var preview: String? { showsImages ? step.textPreview : step.preview }

    /// Error previews always show; only a togglable preview needs a tap affordance.
    private var isToggleable: Bool { !step.isError && !(preview ?? "").isEmpty }

    /// A Read/Write/Edit row opens the file viewer instead; its output moves to the context menu.
    private var file: FileRequest? { openFile == nil ? nil : FileRequest(step: step) }

    private var fileHint: String { file?.change != nil ? "Double tap to show the changes" : "Double tap to open the file" }

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            VStack(spacing: 4) {
                Image(systemName: ToolIcon.symbol(for: step.name))
                    .font(.footnote.weight(.medium))
                    .frame(width: 28, height: 28)
                    .background(step.isError ? Color.red.opacity(0.12) : Color(.tertiarySystemFill), in: .circle)
                    .foregroundStyle(step.isError ? .red : .secondary)
                    .accessibilityHidden(true)
                if !isLast {
                    Rectangle().fill(.quaternary).frame(width: 1.5).frame(maxHeight: .infinity)
                }
            }
            VStack(alignment: .leading, spacing: 8) {
                header
                if showsImages, let store {
                    ToolImagesView(
                        toolCallId: step.id, images: step.images, title: step.summary.isEmpty ? step.name : step.summary,
                        agentId: agentId, store: store, onUnavailable: { imagesUnavailable = true }
                    )
                }
            }
            .padding(.top, 5)
            .padding(.bottom, isLast ? 0 : 14)
        }
        .contentShape(.rect)
        .onTapGesture {
            if let file, let openFile {
                openFile(file)
            } else if isToggleable {
                withAnimation(.snappy) { showPreview.toggle() }
            }
        }
        .contextMenu {
            if let file, let openFile {
                Button(file.change != nil ? "Show Changes" : "Open File", systemImage: "doc.text") { openFile(file) }
                if isToggleable {
                    Button(showPreview ? "Hide Output" : "Show Output", systemImage: "text.alignleft") {
                        withAnimation(.snappy) { showPreview.toggle() }
                    }
                }
            }
        }
    }

    /// The summary and its output. One accessibility element, so the thumbnails below stay their own buttons.
    private var header: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                Text(step.summary.isEmpty ? step.name : step.summary)
                    .font(.subheadline)
                    .foregroundStyle(step.isError ? .red : .primary)
                    .lineLimit(2)
                if !step.finished && isLive {
                    ProgressView().controlSize(.mini).accessibilityHidden(true)
                }
                if file != nil {
                    Image(systemName: "chevron.right")
                        .font(.caption2.weight(.semibold))
                        .foregroundStyle(.tertiary)
                        .accessibilityHidden(true)
                }
            }
            if showPreview || step.isError, let preview, !preview.isEmpty {
                Text(preview)
                    .font(.system(.caption, design: .monospaced))
                    .foregroundStyle(step.isError ? .red.opacity(0.85) : .secondary)
                    .lineLimit(6)
                    .padding(10)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(Color(.secondarySystemBackground), in: .rect(cornerRadius: 10))
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(.rect)
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(file != nil || isToggleable ? .isButton : [])
        .accessibilityHint(
            file != nil ? fileHint : isToggleable ? (showPreview ? "Double tap to hide output" : "Double tap to show output") : ""
        )
        .accessibilityIdentifier(file != nil ? "toolStepFile" : "toolStep")
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
