import SwiftUI

/// The growing tail of the current assistant reply, shown in place of "Thinking…" while an
/// agent is working and the bridge has streamed some text. Renders in the same markdown style as
/// a landed `.text` row, and is replaced by the real row with no layout jump once the transcript
/// message lands (both occupy the same slot at the end of the list).
struct LiveReplyView: View {
    let markdown: String

    var body: some View {
        MarkdownView(markdown)
            .textSelection(.enabled)
            .transition(.opacity.animation(.easeOut(duration: 0.2)))
            .animation(.easeOut(duration: 0.2), value: markdown)
            .accessibilityIdentifier("liveReply")
    }
}

/// The assistant's side of the chat while it's busy and nothing else says so: shimmering "Thinking…"
/// from the moment a prompt is sent. It sits where the reply will appear, so streamed text takes its
/// place; a running tool shows as its own shimmering "Running Bash…" row instead.
struct WorkingBubble: View {
    var title = "Thinking…"

    var body: some View {
        Text(title)
            .font(.body.weight(.medium))
            .shimmer()
            .contentTransition(.opacity)
            .frame(maxWidth: .infinity, alignment: .leading)
            .transition(.opacity.animation(.easeOut(duration: 0.2)))
            .accessibilityLabel(title)
            .accessibilityIdentifier("workingIndicator")
    }
}
