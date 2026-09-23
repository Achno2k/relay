import SwiftUI

/// Floating glass composer: `+` menu, growing field, and a send button that morphs into stop.
struct ComposerView: View {
    @Binding var text: String
    let placeholder: String
    let workspaceName: String?
    let isWorking: Bool
    let onSend: (String) -> Void
    let onStop: () -> Void
    let onNewChat: () -> Void

    @FocusState private var focused: Bool
    @Namespace private var glass
    @State private var sends = 0

    private var trimmed: String { text.trimmingCharacters(in: .whitespacesAndNewlines) }
    /// Typing while the agent works queues a prompt, so send wins over stop when there's text.
    private var showsStop: Bool { isWorking && trimmed.isEmpty }

    var body: some View {
        GlassEffectContainer(spacing: 10) {
            HStack(alignment: .bottom, spacing: 10) {
                Menu {
                    Button {
                        onNewChat()
                    } label: {
                        Label(workspaceName.map { "New chat in \($0)" } ?? "New chat", systemImage: "plus.bubble")
                    }
                } label: {
                    Image(systemName: "plus")
                        .font(.title3.weight(.regular))
                        .frame(width: 48, height: 48)
                        .contentShape(.circle)
                }
                .tint(.primary)
                .glassEffect(.regular.interactive(), in: .circle)
                .accessibilityLabel("More")

                TextField(placeholder, text: $text, axis: .vertical)
                    .lineLimit(1...6)
                    .focused($focused)
                    .padding(.horizontal, 18)
                    .padding(.vertical, 13)
                    .frame(minHeight: 48)
                    .glassEffect(.regular.interactive(), in: .rect(cornerRadius: 24))

                actionButton
            }
        }
        .animation(.smooth(duration: 0.35), value: showsStop)
        .sensoryFeedback(.impact(weight: .medium), trigger: sends)
    }

    @ViewBuilder
    private var actionButton: some View {
        if showsStop {
            Button(action: onStop) {
                Image(systemName: "stop.fill")
                    .font(.system(size: 15, weight: .bold))
                    .foregroundStyle(Color(.systemBackground))
                    .frame(width: 48, height: 48)
                    .contentShape(.circle)
            }
            .buttonStyle(.plain)
            .glassEffect(.regular.tint(.primary).interactive(), in: .circle)
            .glassEffectID("action", in: glass)
            .accessibilityLabel("Stop")
        } else {
            Button {
                guard !trimmed.isEmpty else { return }
                onSend(trimmed)
                text = ""
                sends += 1
            } label: {
                Image(systemName: "arrow.up")
                    .font(.system(size: 17, weight: .bold))
                    .foregroundStyle(trimmed.isEmpty ? Color.secondary : Color(.systemBackground))
                    .frame(width: 48, height: 48)
                    .contentShape(.circle)
            }
            .buttonStyle(.plain)
            .glassEffect(trimmed.isEmpty ? .regular : .regular.tint(.primary).interactive(), in: .circle)
            .glassEffectID("action", in: glass)
            .disabled(trimmed.isEmpty)
            .accessibilityLabel("Send")
        }
    }
}
