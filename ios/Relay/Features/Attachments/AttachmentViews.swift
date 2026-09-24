import RelayKit
import QuickLook
import SwiftUI
import UIKit

// MARK: - Composer tray

/// Picked files above the text field, inside the glass composer: thumbnails and file chips with progress.
struct ComposerAttachmentTray: View {
    let attachments: ComposerAttachments

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                ForEach(attachments.items) { item in
                    TrayItem(item: item) { withAnimation(.smooth) { attachments.remove(item.id) } }
                        .transition(.scale.combined(with: .opacity))
                }
            }
            .padding(.horizontal, 10)
            .padding(.top, 10)
        }
        .scrollClipDisabled()
        .accessibilityIdentifier("attachmentTray")
    }
}

private struct TrayItem: View {
    let item: ComposerAttachments.Item
    let onRemove: () -> Void

    var body: some View {
        content
            .overlay(alignment: .topTrailing) {
                Button(action: onRemove) {
                    Image(systemName: "xmark")
                        .font(.system(size: 9, weight: .bold))
                        .foregroundStyle(.white)
                        .frame(width: 20, height: 20)
                        .background(.black.opacity(0.7), in: .circle)
                }
                .buttonStyle(.plain)
                .offset(x: 6, y: -6)
                .accessibilityLabel("Remove \(item.name)")
            }
            .accessibilityElement(children: .contain)
            .accessibilityIdentifier("trayItem")
            .accessibilityValue(stateLabel)
    }

    @ViewBuilder
    private var content: some View {
        if item.kind == .image {
            Group {
                if let preview = item.preview {
                    Image(uiImage: preview).resizable().scaledToFill()
                } else {
                    Color(.tertiarySystemFill)
                }
            }
            .frame(width: 58, height: 58)
            .clipShape(.rect(cornerRadius: 14))
            .overlay { progressOverlay.clipShape(.rect(cornerRadius: 14)) }
        } else {
            HStack(spacing: 8) {
                Image(systemName: FileSymbol.name(for: item.kind))
                    .font(.title3)
                    .foregroundStyle(item.kind == .pdf ? .red : .blue)
                VStack(alignment: .leading, spacing: 3) {
                    Text(item.name)
                        .font(.footnote.weight(.medium))
                        .lineLimit(1)
                    if case .uploading(let p) = item.state {
                        ProgressView(value: p).progressViewStyle(.linear).frame(width: 90)
                    } else if case .failed = item.state {
                        Text("Failed").font(.caption2).foregroundStyle(.red)
                    } else {
                        Text(item.kind == .pdf ? "PDF" : "File").font(.caption2).foregroundStyle(.secondary)
                    }
                }
                .frame(maxWidth: 130, alignment: .leading)
            }
            .padding(.horizontal, 10)
            .frame(height: 58)
            .background(Color(.tertiarySystemFill), in: .rect(cornerRadius: 14))
        }
    }

    @ViewBuilder
    private var progressOverlay: some View {
        switch item.state {
        case .preparing:
            ZStack { Color.black.opacity(0.35); ProgressView().tint(.white) }
        case .uploading(let p):
            ZStack {
                Color.black.opacity(0.35)
                Circle().stroke(.white.opacity(0.3), lineWidth: 3).frame(width: 24, height: 24)
                Circle().trim(from: 0, to: max(0.05, p)).stroke(.white, style: StrokeStyle(lineWidth: 3, lineCap: .round))
                    .rotationEffect(.degrees(-90)).frame(width: 24, height: 24)
                    .animation(.linear(duration: 0.2), value: p)
            }
        case .failed:
            ZStack { Color.red.opacity(0.45); Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.white) }
        case .uploaded:
            EmptyView()
        }
    }

    private var stateLabel: String {
        switch item.state {
        case .preparing: "preparing"
        case .uploading: "uploading"
        case .uploaded: "uploaded"
        case .failed: "failed"
        }
    }
}

enum FileSymbol {
    static func name(for kind: AttachmentKind) -> String {
        switch kind {
        case .image: "photo"
        case .pdf: "doc.richtext.fill"
        case .file: "doc.text.fill"
        }
    }
}

// MARK: - In the transcript

/// Attachments above a user bubble: image thumbnails (tap for full screen) and file chips (tap for QuickLook).
struct MessageAttachments: View {
    let attachments: [AttachmentRef]
    let agentId: String
    let store: AppStore

    @State private var viewing: ViewedImage?
    @State private var previewURL: URL?

    var body: some View {
        let images = attachments.filter { $0.kind == .image }
        let files = attachments.filter { $0.kind != .image }
        VStack(alignment: .trailing, spacing: 6) {
            if !images.isEmpty {
                HStack(spacing: 6) {
                    ForEach(images) { ref in
                        AttachmentThumbnail(ref: ref, agentId: agentId, store: store) { image in
                            viewing = ViewedImage(image: image, name: ref.name)
                        }
                    }
                }
            }
            ForEach(files) { ref in
                FileChip(ref: ref) { await openInQuickLook(ref) }
            }
        }
        .fullScreenCover(item: $viewing) { ImageViewer(image: $0.image, name: $0.name) }
        .quickLookPreview($previewURL)
    }

    private func openInQuickLook(_ ref: AttachmentRef) async {
        do {
            let data = try await store.attachmentData(agentId: agentId, attachmentId: ref.id)
            let dir = FileManager.default.temporaryDirectory.appending(path: "attachments/\(ref.id)", directoryHint: .isDirectory)
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            let url = dir.appending(path: ref.name)
            try data.write(to: url, options: .atomic)
            previewURL = url
        } catch {
            store.errorMessage = "\(ref.name): \(error.localizedDescription)"
        }
    }
}

private struct ViewedImage: Identifiable {
    let id = UUID()
    let image: UIImage
    let name: String
}

struct AttachmentThumbnail: View {
    let ref: AttachmentRef
    let agentId: String
    let store: AppStore
    let onOpen: (UIImage) -> Void

    @State private var image: UIImage?
    @State private var failed = false

    var body: some View {
        Button {
            if let image { onOpen(image) }
        } label: {
            ZStack {
                Color(.secondarySystemFill)
                if let image {
                    Image(uiImage: image).resizable().scaledToFill()
                } else if failed {
                    VStack(spacing: 4) {
                        Image(systemName: "photo.badge.exclamationmark")
                        Text("Expired").font(.caption2)
                    }
                    .foregroundStyle(.secondary)
                } else {
                    ProgressView()
                }
            }
            .frame(width: 116, height: 116)
            .clipShape(.rect(cornerRadius: 18))
        }
        .buttonStyle(.plain)
        .accessibilityLabel(ref.name)
        .accessibilityIdentifier("attachmentImage")
        .task(id: ref.id) {
            guard image == nil else { return }
            do {
                let data = try await store.attachmentData(agentId: agentId, attachmentId: ref.id)
                image = await Task.detached { UIImage(data: data)?.preparingThumbnail(of: CGSize(width: 348, height: 348)) }.value
                if image == nil { failed = true }
            } catch {
                failed = true
            }
        }
    }
}

private struct FileChip: View {
    let ref: AttachmentRef
    let open: () async -> Void
    @State private var loading = false

    var body: some View {
        Button {
            loading = true
            Task {
                await open()
                loading = false
            }
        } label: {
            HStack(spacing: 10) {
                Image(systemName: FileSymbol.name(for: ref.kind))
                    .font(.title3)
                    .foregroundStyle(ref.kind == .pdf ? .red : .blue)
                    .frame(width: 34, height: 34)
                    .background(Color(.tertiarySystemFill), in: .rect(cornerRadius: 9))
                VStack(alignment: .leading, spacing: 1) {
                    Text(ref.name).font(.subheadline.weight(.medium)).lineLimit(1)
                    Text(ref.kind == .pdf ? "PDF" : "File").font(.caption).foregroundStyle(.secondary)
                }
                if loading { ProgressView().controlSize(.small) }
            }
            .padding(8)
            .padding(.trailing, 6)
            .background(Color(.secondarySystemFill), in: .rect(cornerRadius: 16))
        }
        .buttonStyle(.plain)
        .accessibilityIdentifier("attachmentFile")
        .accessibilityLabel(ref.name)
    }
}

/// Full-screen image with pinch to zoom.
private struct ImageViewer: View {
    let image: UIImage
    let name: String
    @Environment(\.dismiss) private var dismiss
    @State private var scale: CGFloat = 1
    @GestureState private var pinch: CGFloat = 1

    var body: some View {
        ZStack {
            Color.black.ignoresSafeArea()
            Image(uiImage: image)
                .resizable()
                .scaledToFit()
                .scaleEffect(scale * pinch)
                .gesture(
                    MagnifyGesture()
                        .updating($pinch) { value, state, _ in state = value.magnification }
                        .onEnded { value in withAnimation(.smooth) { scale = min(max(scale * value.magnification, 1), 5) } }
                )
                .onTapGesture(count: 2) { withAnimation(.smooth) { scale = scale > 1 ? 1 : 2.5 } }
        }
        .overlay(alignment: .topTrailing) {
            Button {
                dismiss()
            } label: {
                Image(systemName: "xmark").font(.body.weight(.semibold)).frame(width: 44, height: 44)
            }
            .buttonStyle(.plain)
            .foregroundStyle(.white)
            .glassEffect(.regular.interactive(), in: .circle)
            .padding()
            .accessibilityLabel("Close")
        }
        .overlay(alignment: .bottom) {
            Text(name)
                .font(.footnote)
                .foregroundStyle(.white.opacity(0.8))
                .padding(.bottom, 24)
        }
        .preferredColorScheme(.dark)
    }
}

// MARK: - Camera

struct CameraPicker: UIViewControllerRepresentable {
    let onImage: (UIImage) -> Void
    @Environment(\.dismiss) private var dismiss

    static var isAvailable: Bool { UIImagePickerController.isSourceTypeAvailable(.camera) }

    func makeUIViewController(context: Context) -> UIImagePickerController {
        let picker = UIImagePickerController()
        picker.sourceType = .camera
        picker.delegate = context.coordinator
        return picker
    }

    func updateUIViewController(_ controller: UIImagePickerController, context: Context) {}

    func makeCoordinator() -> Coordinator { Coordinator(parent: self) }

    @MainActor
    final class Coordinator: NSObject, UIImagePickerControllerDelegate, UINavigationControllerDelegate {
        let parent: CameraPicker

        init(parent: CameraPicker) {
            self.parent = parent
        }

        func imagePickerController(_ picker: UIImagePickerController, didFinishPickingMediaWithInfo info: [UIImagePickerController.InfoKey: Any]) {
            if let image = info[.originalImage] as? UIImage { parent.onImage(image) }
            parent.dismiss()
        }

        func imagePickerControllerDidCancel(_ picker: UIImagePickerController) {
            parent.dismiss()
        }
    }
}

#if DEBUG
/// Generated files for UI tests (`-uitestAttachments`): a PNG that reads "RELAY" and a one-page PDF.
enum TestAttachments {
    static func relayImage(_ word: String = LaunchOptions.current.testWord ?? "RELAY") -> Data {
        let renderer = UIGraphicsImageRenderer(size: CGSize(width: 640, height: 320), format: {
            let f = UIGraphicsImageRendererFormat()
            f.scale = 1
            return f
        }())
        return renderer.pngData { ctx in
            UIColor.white.setFill()
            ctx.fill(CGRect(x: 0, y: 0, width: 640, height: 320))
            let text = NSAttributedString(string: word, attributes: [
                .font: UIFont.systemFont(ofSize: word.count > 5 ? 96 : 150, weight: .black),
                .foregroundColor: UIColor.black,
            ])
            let size = text.size()
            text.draw(at: CGPoint(x: (640 - size.width) / 2, y: (320 - size.height) / 2))
        }
    }

    static func codewordPDF(_ word: String = LaunchOptions.current.testWord ?? "PELICAN") -> Data {
        UIGraphicsPDFRenderer(bounds: CGRect(x: 0, y: 0, width: 612, height: 792)).pdfData { ctx in
            ctx.beginPage()
            NSAttributedString(string: "Relay test document\n\nThe code word is \(word).", attributes: [
                .font: UIFont.systemFont(ofSize: 28, weight: .semibold),
            ]).draw(in: CGRect(x: 60, y: 80, width: 492, height: 400))
        }
    }
}
#endif
