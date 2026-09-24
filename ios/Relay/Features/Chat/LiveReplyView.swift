import SwiftUI

/// The growing tail of the current assistant reply, shown in place of the pulsing dot while an
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
