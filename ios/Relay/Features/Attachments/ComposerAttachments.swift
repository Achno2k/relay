import Foundation
import RelayKit
import Observation
import UIKit
import UniformTypeIdentifiers

/// Files picked for the next message in one chat: each is prepared, then uploaded straight away with progress.
@MainActor
@Observable
final class ComposerAttachments {
    enum State: Equatable {
        case preparing
        case uploading(Double)
        case uploaded(Attachment)
        case failed(String)
    }

    struct Item: Identifiable, Equatable {
        let id = UUID()
        var name: String
        var kind: AttachmentKind
        var preview: UIImage?
        var state: State = .preparing
    }

    private(set) var items: [Item] = []
    let agentId: String
    private let store: AppStore

    init(agentId: String, store: AppStore) {
        self.agentId = agentId
        self.store = store
    }

    var isEmpty: Bool { items.isEmpty }
    var isBusy: Bool { items.contains { if case .preparing = $0.state { true } else if case .uploading = $0.state { true } else { false } } }
    var uploaded: [Attachment] { items.compactMap { if case .uploaded(let a) = $0.state { a } else { nil } } }
    var remainingSlots: Int { AttachmentProcessing.maxCount - items.count }

    /// Takes loaded file data; prepares and uploads it in the background.
    func add(data: Data, name: String, type: UTType?) {
        guard remainingSlots > 0 else {
            store.errorMessage = "Up to \(AttachmentProcessing.maxCount) files per message."
            return
        }
        let isImage = (type ?? UTType(filenameExtension: (name as NSString).pathExtension))?.conforms(to: .image) == true
        let item = Item(name: name, kind: isImage ? .image : .file)
        items.append(item)
        let id = item.id
        let agentId = agentId
        let backend = store.backend
        Task {
            do {
                let prepared = try await Task.detached(priority: .userInitiated) {
                    try AttachmentProcessing.prepare(data: data, name: name, type: type)
                }.value
                update(id) {
                    $0.name = prepared.name
                    $0.kind = prepared.kind
                    if prepared.kind == .image { $0.preview = UIImage(data: prepared.data)?.preparingThumbnail(of: CGSize(width: 160, height: 160)) }
                    $0.state = .uploading(0)
                }
                let attachment = try await backend.uploadAttachment(
                    agentId: agentId, data: prepared.data, filename: prepared.name, contentType: prepared.contentType
                ) { fraction in
                    Task { @MainActor [weak self] in
                        self?.update(id) { if case .uploading = $0.state { $0.state = .uploading(fraction) } }
                    }
                }
                store.cacheAttachment(attachment.id, data: prepared.data)
                update(id) { $0.state = .uploaded(attachment) }
            } catch {
                update(id) { $0.state = .failed(error.localizedDescription) }
                store.errorMessage = "\(name): \(error.localizedDescription)"
            }
        }
    }

    /// Files picked in Files, in order. Read off the main thread: ten 20 MB files read inline froze the
    /// composer, and one huge file was loaded in full before being turned down as over 20 MB.
    func add(files urls: [URL]) {
        Task {
            for url in urls {
                guard remainingSlots > 0 else {
                    store.errorMessage = "Up to \(AttachmentProcessing.maxCount) files per message."
                    return
                }
                do {
                    let file = try await Task.detached(priority: .userInitiated) { try AttachmentProcessing.read(url) }.value
                    add(data: file.data, name: url.lastPathComponent, type: file.type)
                } catch let failure as AttachmentProcessing.Failure {
                    store.errorMessage = failure.errorDescription
                } catch {
                    store.errorMessage = "Couldn't read \(url.lastPathComponent)."
                }
            }
        }
    }

    func remove(_ id: UUID) {
        items.removeAll { $0.id == id }
    }

    /// Hands the uploaded attachments to the message being sent and empties the tray.
    func take() -> [Attachment] {
        let done = uploaded
        items.removeAll()
        return done
    }

    private func update(_ id: UUID, _ change: (inout Item) -> Void) {
        guard let i = items.firstIndex(where: { $0.id == id }) else { return }
        change(&items[i])
    }
}
