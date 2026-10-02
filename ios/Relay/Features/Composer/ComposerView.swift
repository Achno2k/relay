import RelayKit
import SwiftUI

/// Floating glass composer: `+` (a sheet with attachments and new chat, see AttachmentSheet), growing field with an attachment tray
/// and a dictation mic, and a send button that morphs into stop.
struct ComposerView: View {
    @Binding var text: String
    let placeholder: String
    let workspaceName: String?
    let isWorking: Bool
    /// Picked files for the next message; nil hides the attachment items.
    var attachments: ComposerAttachments?
    let onSend: (String, [Attachment]) -> Void
    let onStop: () -> Void
    let onNewChat: () -> Void
    /// Set to true to focus the field (a just-created chat, "ready to type"); reset once focused.
    var focusRequest: Binding<Bool> = .constant(false)
    /// Why this agent can't be prompted right now ("Mock VM is offline"). Set, it replaces the placeholder
    /// and turns off typing, send and stop; a draft already typed stays.
    var unavailable: String?

    @FocusState private var focused: Bool
    @Namespace private var glass
    @State private var sends = 0
    @State private var showPlus = false
    @State private var dictation = Dictation()
    @Environment(\.scenePhase) private var scenePhase

    private var trimmed: String { text.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var hasAttachments: Bool { !(attachments?.isEmpty ?? true) }
    /// Send needs text or an attachment, and every upload finished.
    private var canSend: Bool {
        unavailable == nil
            && (!trimmed.isEmpty || !(attachments?.uploaded.isEmpty ?? true)) && !(attachments?.isBusy ?? false)
    }
    /// Typing while the agent works queues a prompt, so send wins over stop when there's something to send.
    private var showsStop: Bool { unavailable == nil && isWorking && trimmed.isEmpty && !hasAttachments }

    var body: some View {
        VStack(spacing: 8) {
            DictationNotice(dictation: dictation)
                .transition(.move(edge: .bottom).combined(with: .opacity))
            composer
        }
        .animation(.smooth, value: dictation.notice)
        .animation(.smooth, value: dictation.phase)
        // Dictation never outlives its chat, the foreground, or a usable composer.
        .onDisappear { dictation.cancel() }
        .onChange(of: scenePhase) { _, phase in
            if phase == .background { dictation.finish() }
        }
        .onChange(of: unavailable) { _, reason in
            if reason != nil { dictation.cancel() }
        }
        .onChange(of: text) { _, text in dictation.noteEdit(text) }
    }

    private var composer: some View {
        GlassEffectContainer(spacing: 10) {
            HStack(alignment: .bottom, spacing: 10) {
                plusMenu

                VStack(alignment: .leading, spacing: 0) {
                    if let attachments, !attachments.isEmpty {
                        ComposerAttachmentTray(attachments: attachments)
                            .transition(.move(edge: .bottom).combined(with: .opacity))
                    }
                    HStack(alignment: .bottom, spacing: 0) {
                        TextField(unavailable ?? placeholder, text: $text, axis: .vertical)
                            .lineLimit(1...6)
                            .focused($focused)
                            .disabled(unavailable != nil)
                            .accessibilityIdentifier("composerField")
                            .padding(.vertical, 13)
                            .padding(.leading, 18)
                            .padding(.trailing, unavailable == nil ? 0 : 18)
                        if unavailable == nil {
                            DictationButton(dictation: dictation) { dictation.toggle($text) }
                                .padding(.trailing, 4)
                                .padding(.bottom, 2)
                        }
                    }
                }
                // The tray is a horizontal scroll view; without this the field shrinks to its width.
                .frame(maxWidth: .infinity, minHeight: 48, alignment: .leading)
                .glassEffect(.regular.interactive(), in: .rect(cornerRadius: 24))

                actionButton
            }
        }
        .animation(.smooth(duration: 0.35), value: showsStop)
        .animation(.smooth, value: attachments?.items.count)
        .sensoryFeedback(.impact(weight: .medium), trigger: sends)
        .task(id: focusRequest.wrappedValue) {
            guard focusRequest.wrappedValue else { return }
            // After the New chat sheet has gone, or the keyboard can't come up.
            try? await Task.sleep(for: .milliseconds(450))
            focused = true
            focusRequest.wrappedValue = false
        }
        .composerPlusSheet(
            isPresented: $showPlus, attachments: attachments,
            newChatTitle: workspaceName.map { "New chat in \($0)" } ?? "New chat", onNewChat: onNewChat
        )
    }

    private var plusMenu: some View {
        Button {
            showPlus = true
        } label: {
            Image(systemName: "plus")
                .font(.title3.weight(.regular))
                .frame(width: 48, height: 48)
                .contentShape(.circle)
        }
        .buttonStyle(.plain)
        .glassEffect(.regular.interactive(), in: .circle)
        .accessibilityLabel("Add")
        .accessibilityIdentifier("composerPlus")
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
                guard canSend else { return }
                dictation.cancel()
                onSend(trimmed, attachments?.take() ?? [])
                text = ""
                sends += 1
            } label: {
                Image(systemName: "arrow.up")
                    .font(.system(size: 17, weight: .bold))
                    .foregroundStyle(canSend ? Color(.systemBackground) : Color.secondary)
                    .frame(width: 48, height: 48)
                    .contentShape(.circle)
            }
            .buttonStyle(.plain)
            .glassEffect(canSend ? .regular.tint(.primary).interactive() : .regular, in: .circle)
            .glassEffectID("action", in: glass)
            .disabled(!canSend)
            .accessibilityLabel("Send")
        }
    }
}
