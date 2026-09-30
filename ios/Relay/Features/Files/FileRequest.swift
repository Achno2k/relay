import RelayKit
import SwiftUI

/// A file to show from a tool row: its path (to fetch it) and/or what the call changed (to draw a diff).
struct FileRequest: Hashable, Identifiable {
    /// The tool call's id.
    var id: String
    /// cwd-relative (`toolCall.path`). nil when the file is outside the project: then only the change can be shown.
    var path: String?
    /// The row's summary, the title when there's no path (e.g. "Edited 3 files").
    var summary: String
    var change: FileChange?
    /// The bridge cut the change's strings (api.md caps); the whole file is one tab away.
    var changeTruncated = false
    /// The call failed, so the change wasn't applied.
    var failed = false

    var fileName: String {
        guard let path else { return summary }
        return path.split(separator: "/").last.map(String.init) ?? path
    }

    /// The folder part of `path`, for the subtitle; nil at the project root.
    var folder: String? {
        guard let path, let slash = path.lastIndex(of: "/") else { return nil }
        return String(path[..<slash])
    }
}

/// Opens a `FileRequest` in the viewer sheet. Set by `.fileViewer(store:)`; tool rows read it from the environment.
struct OpenFileAction {
    let open: @MainActor (FileRequest) -> Void

    @MainActor func callAsFunction(_ request: FileRequest) { open(request) }
}

extension EnvironmentValues {
    @Entry var openFile: OpenFileAction?
}

extension View {
    /// Lets tool rows inside open files of the selected agent in a sheet.
    func fileViewer(store: AppStore) -> some View {
        modifier(FileViewerPresenter(store: store))
    }
}

private struct FileViewerPresenter: ViewModifier {
    let store: AppStore
    @State private var presented: Presented?
    /// An approval arrived while a file was open; it's shown when the file sheet has gone.
    @State private var heldApproval = false

    struct Presented: Identifiable {
        let agentId: String
        let request: FileRequest
        var id: String { agentId + "|" + request.id }
    }

    func body(content: Content) -> some View {
        content
            .environment(\.openFile, OpenFileAction { request in
                guard let agentId = store.selectedAgentId else { return }
                presented = Presented(agentId: agentId, request: request)
            })
            .sheet(item: $presented, onDismiss: presentHeldApproval) { item in
                FileViewerSheet(request: item.request, agentId: item.agentId, store: store)
            }
            .onChange(of: store.isApprovalSheetPresented) { _, shown in
                // One sheet at a time: an approval outranks a file, so close the file and show the approval
                // once it's gone (presenting both at once would drop the approval).
                guard shown, presented != nil else { return }
                heldApproval = true
                store.isApprovalSheetPresented = false
                presented = nil
            }
            .task(id: store.messages(for: store.selectedAgentId ?? "").count) { openDemo() }
    }

    private func presentHeldApproval() {
        guard heldApproval else { return }
        heldApproval = false
        if store.approval != nil { store.isApprovalSheetPresented = true }
    }

    /// `-demo file:<toolCallId>` opens that call's file once the chat has loaded (screenshots, UI tests).
    private func openDemo() {
        guard presented == nil, !store.isApprovalSheetPresented, let demo = LaunchOptions.current.demo, demo.hasPrefix("file:"),
              let agentId = store.selectedAgentId else { return }
        let id = String(demo.dropFirst("file:".count))
        for case .tools(_, let steps, _) in ChatItem.build(from: store.messages(for: agentId)) {
            if let step = steps.first(where: { $0.id == id }), let request = FileRequest(step: step) {
                presented = Presented(agentId: agentId, request: request)
                return
            }
        }
    }
}

extension FileRequest {
    /// nil for rows with nothing to open: no file inside the project and no change to draw.
    init?(step: ToolStep) {
        let change = step.edit.flatMap(FileChange.init)
        guard step.path != nil || change != nil else { return nil }
        self.init(
            id: step.id, path: step.path, summary: step.summary, change: change,
            changeTruncated: step.edit?.truncated ?? false, failed: step.isError
        )
    }
}

extension FileChange {
    /// nil for a kind this app doesn't know, or one with nothing in it.
    init?(_ edit: ToolEdit) {
        switch edit.kind {
        case .edit where !edit.changes.isEmpty:
            self = .edits(edit.changes.map { Replacement(old: $0.old, new: $0.new, replaceAll: $0.replaceAll) })
        case .write:
            self = .write(edit.content ?? "")
        case .diff where !(edit.diff ?? "").isEmpty:
            self = .diff(edit.diff ?? "")
        default:
            return nil
        }
    }
}
