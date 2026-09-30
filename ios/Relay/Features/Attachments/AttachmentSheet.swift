import PhotosUI
import SwiftUI
import UniformTypeIdentifiers

/// What the composer's `+` opens, ChatGPT style: a short sheet with big Camera, Photos and Files tiles and a
/// New chat row. It replaced a system menu whose bottom row (Photos) sat right on the `+` and the menu's
/// rounded corner, so it was easy to miss (B4). The sheet closes first; the picker opens once it has gone.
extension View {
    func composerPlusSheet(
        isPresented: Binding<Bool>,
        attachments: ComposerAttachments?,
        newChatTitle: String,
        onNewChat: @escaping () -> Void
    ) -> some View {
        modifier(ComposerPlusSheet(isPresented: isPresented, attachments: attachments, newChatTitle: newChatTitle, onNewChat: onNewChat))
    }
}

enum ComposerPlusChoice {
    case camera, photos, files, newChat
}

private struct ComposerPlusSheet: ViewModifier {
    @Binding var isPresented: Bool
    let attachments: ComposerAttachments?
    let newChatTitle: String
    let onNewChat: () -> Void

    /// Picked in the sheet, acted on once it has been dismissed: two presentations can't overlap.
    @State private var chosen: ComposerPlusChoice?
    @State private var showPhotos = false
    @State private var photoItems: [PhotosPickerItem] = []
    @State private var showFiles = false
    @State private var showCamera = false
    @State private var sheetHeight: CGFloat = 260

    func body(content: Content) -> some View {
        content
            .sheet(isPresented: $isPresented, onDismiss: openChosen) {
                AttachmentSheet(attachments: attachments, newChatTitle: newChatTitle) { choice in
                    chosen = choice
                    isPresented = false
                }
                .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { sheetHeight = $0 }
                .presentationDetents([.height(sheetHeight)])
                .presentationDragIndicator(.visible)
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

    private func openChosen() {
        defer { chosen = nil }
        switch chosen {
        case .camera: showCamera = true
        case .photos: showPhotos = true
        case .files: showFiles = true
        case .newChat: onNewChat()
        case nil: break
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

/// Camera, Photos and Files as 88 pt tiles (a whole tile is the target), then New chat as a full-width row.
/// Without attachments (nil) only New chat is offered.
struct AttachmentSheet: View {
    let attachments: ComposerAttachments?
    let newChatTitle: String
    let onChoose: (ComposerPlusChoice) -> Void

    @Environment(\.dismiss) private var dismiss
    @Environment(\.dynamicTypeSize) private var typeSize
    /// The icon column of the full-width rows, so their names line up.
    @ScaledMetric(relativeTo: .title3) private var iconWidth: CGFloat = 28

    var body: some View {
        VStack(spacing: 12) {
            if let attachments {
                // At accessibility sizes three tiles side by side would truncate their names; they stack.
                let layout = typeSize.isAccessibilitySize ? AnyLayout(VStackLayout(spacing: 10)) : AnyLayout(HStackLayout(spacing: 10))
                layout {
                    tile("Camera", systemImage: "camera", choice: .camera, enabled: CameraPicker.isAvailable)
                    tile("Photos", systemImage: "photo.on.rectangle", choice: .photos)
                    tile("Files", systemImage: "folder", choice: .files)
                }
                #if DEBUG
                if LaunchOptions.current.testAttachments {
                    HStack(spacing: 10) {
                        testButton("Test image (RELAY)") { attachments.add(data: TestAttachments.relayImage(), name: "relay.png", type: .png) }
                        testButton("Test PDF") { attachments.add(data: TestAttachments.codewordPDF(), name: "codeword.pdf", type: .pdf) }
                    }
                }
                #endif
            }
            Button {
                onChoose(.newChat)
            } label: {
                HStack(spacing: 14) {
                    Image(systemName: "plus.bubble")
                        .font(.title3)
                        .frame(width: iconWidth)
                    Text(newChatTitle)
                        .font(.body.weight(.medium))
                        .multilineTextAlignment(.leading)
                    Spacer(minLength: 0)
                }
                .padding(.horizontal, 18)
                .padding(.vertical, 12)
                .frame(maxWidth: .infinity, minHeight: 56)
                .background(Color(.tertiarySystemFill), in: .rect(cornerRadius: 20))
                .contentShape(.rect(cornerRadius: 20))
            }
            .buttonStyle(.plain)
            .accessibilityIdentifier("plusNewChat")
        }
        .padding(.horizontal, 20)
        .padding(.top, 32)
        .padding(.bottom, 12)
        .fixedSize(horizontal: false, vertical: true)
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("plusSheet")
    }

    private func tile(_ title: String, systemImage: String, choice: ComposerPlusChoice, enabled: Bool = true) -> some View {
        Button {
            onChoose(choice)
        } label: {
            // Stacked at accessibility sizes, a tile reads like the New chat row: icon, then name.
            let stacked = typeSize.isAccessibilitySize
            let layout = stacked ? AnyLayout(HStackLayout(spacing: 14)) : AnyLayout(VStackLayout(spacing: 8))
            layout {
                Image(systemName: systemImage)
                    .font(stacked ? .title3 : .title2)
                    .frame(width: stacked ? iconWidth : nil)
                Text(title)
                    .font(stacked ? .body.weight(.medium) : .subheadline.weight(.medium))
                if stacked { Spacer(minLength: 0) }
            }
            .padding(.horizontal, stacked ? 18 : 0)
            .padding(.vertical, stacked ? 12 : 0)
            .frame(maxWidth: .infinity, minHeight: stacked ? 56 : 88)
            .background(Color(.tertiarySystemFill), in: .rect(cornerRadius: 20))
            .contentShape(.rect(cornerRadius: 20))
        }
        .buttonStyle(AttachmentTileStyle())
        .disabled(!enabled)
        .opacity(enabled ? 1 : 0.4)
        .accessibilityLabel(title)
    }

    #if DEBUG
    private func testButton(_ title: String, add: @escaping () -> Void) -> some View {
        Button(title) {
            add()
            dismiss()
        }
        .font(.footnote)
        .buttonStyle(.bordered)
    }
    #endif
}

/// Dims and shrinks a tile a little while pressed, so a touch visibly lands.
private struct AttachmentTileStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .scaleEffect(configuration.isPressed ? 0.96 : 1)
            .opacity(configuration.isPressed ? 0.7 : 1)
            .animation(.smooth(duration: 0.15), value: configuration.isPressed)
    }
}
