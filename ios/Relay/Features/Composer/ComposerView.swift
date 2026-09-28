import RelayKit
import PhotosUI
import SwiftUI
import UniformTypeIdentifiers

/// Floating glass composer: `+` menu (attachments, new chat), growing field with an attachment tray,
/// and a send button that morphs into stop.
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

    @FocusState private var focused: Bool
    @Namespace private var glass
    @State private var sends = 0
    @State private var showPhotos = false
    @State private var photoItems: [PhotosPickerItem] = []
    @State private var showFiles = false
    @State private var showCamera = false

    private var trimmed: String { text.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var hasAttachments: Bool { !(attachments?.isEmpty ?? true) }
    /// Send needs text or an attachment, and every upload finished.
    private var canSend: Bool {
        (!trimmed.isEmpty || !(attachments?.uploaded.isEmpty ?? true)) && !(attachments?.isBusy ?? false)
    }
    /// Typing while the agent works queues a prompt, so send wins over stop when there's something to send.
    private var showsStop: Bool { isWorking && trimmed.isEmpty && !hasAttachments }

    var body: some View {
        GlassEffectContainer(spacing: 10) {
            HStack(alignment: .bottom, spacing: 10) {
                plusMenu

                VStack(alignment: .leading, spacing: 0) {
                    if let attachments, !attachments.isEmpty {
                        ComposerAttachmentTray(attachments: attachments)
                            .transition(.move(edge: .bottom).combined(with: .opacity))
                    }
                    TextField(placeholder, text: $text, axis: .vertical)
                        .lineLimit(1...6)
                        .focused($focused)
                        .padding(.vertical, 13)
                        .padding(.horizontal, 18)
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
        .photosPicker(
            isPresented: $showPhotos, selection: $photoItems,
            maxSelectionCount: max(1, attachments?.remainingSlots ?? 1), matching: .images
        )
        .onChange(of: photoItems) { _, items in
            guard !items.isEmpty else { return }
            photoItems = []
            loadPhotos(items)
        }
        .fileImporter(
            isPresented: $showFiles,
            allowedContentTypes: [.image, .pdf, .plainText, .sourceCode, .json, .data],
            allowsMultipleSelection: true
        ) { result in
            if case .success(let urls) = result { attachments?.add(files: urls) }
        }
        .fullScreenCover(isPresented: $showCamera) {
            CameraPicker { image in
                if let data = image.jpegData(compressionQuality: 0.95) {
                    attachments?.add(data: data, name: "photo.jpg", type: .jpeg)
                }
            }
            .ignoresSafeArea()
        }
    }

    private var plusMenu: some View {
        Menu {
            if attachments != nil {
                Section {
                    Button { showPhotos = true } label: { Label("Photos", systemImage: "photo.on.rectangle") }
                    Button { showCamera = true } label: { Label("Camera", systemImage: "camera") }
                        .disabled(!CameraPicker.isAvailable)
                    Button { showFiles = true } label: { Label("Files", systemImage: "folder") }
                }
                #if DEBUG
                if LaunchOptions.current.testAttachments {
                    Section("Test files") {
                        Button("Test image (RELAY)") { attachments?.add(data: TestAttachments.relayImage(), name: "relay.png", type: .png) }
                        Button("Test PDF") { attachments?.add(data: TestAttachments.codewordPDF(), name: "codeword.pdf", type: .pdf) }
                    }
                }
                #endif
            }
            Section {
                Button {
                    onNewChat()
                } label: {
                    Label(workspaceName.map { "New chat in \($0)" } ?? "New chat", systemImage: "plus.bubble")
                }
            }
        } label: {
            Image(systemName: "plus")
                .font(.title3.weight(.regular))
                .frame(width: 48, height: 48)
                .contentShape(.circle)
        }
        .tint(.primary)
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

    private func loadPhotos(_ items: [PhotosPickerItem]) {
        for (i, item) in items.enumerated() {
            Task {
                guard let data = try? await item.loadTransferable(type: Data.self) else { return }
                let type = item.supportedContentTypes.first { $0.conforms(to: .image) } ?? .jpeg
                attachments?.add(data: data, name: "photo-\(i + 1).\(type.preferredFilenameExtension ?? "jpg")", type: type)
            }
        }
    }
}
