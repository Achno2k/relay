import RelayKit
import SwiftUI
import UIKit

/// A file the agent read or changed: "Changes" draws the call's diff, "File" shows the file as it is now.
struct FileViewerSheet: View {
    let request: FileRequest
    let agentId: String
    let store: AppStore

    @State private var tab: Tab
    @State private var load: Load = .loading
    @State private var fileLines: [CodeLine] = []
    @State private var changeLines: [CodeLine] = []
    /// nil follows the file type: prose wraps, code scrolls sideways.
    @State private var wrapOverride: Bool?
    @State private var showSource = false

    enum Tab: Hashable { case changes, file }

    enum Load {
        case loading
        case text(FileContent)
        case image(UIImage, Data)
        case failed(FileLoadError)
    }

    init(request: FileRequest, agentId: String, store: AppStore) {
        self.request = request
        self.agentId = agentId
        self.store = store
        _tab = State(initialValue: request.change != nil ? .changes : .file)
    }

    private var hasBothTabs: Bool { request.change != nil && request.path != nil }

    private var loadedText: FileContent? {
        if case .text(let file) = load { file } else { nil }
    }

    private var isMarkdown: Bool {
        let ext = (request.path.map { ($0 as NSString).pathExtension.lowercased() }) ?? ""
        return ["md", "markdown"].contains(loadedText?.language ?? ext) || ["md", "markdown", "mdx"].contains(ext)
    }

    private var isProse: Bool {
        let ext = (request.path.map { ($0 as NSString).pathExtension.lowercased() }) ?? ""
        return isMarkdown || ["txt", "text", "rst", "log"].contains(ext)
    }

    private var wrap: Bool { wrapOverride ?? isProse }

    /// Big Markdown files stay source; laying out 1 MB of rendered blocks would stall the sheet.
    private var rendersMarkdown: Bool {
        guard isMarkdown, !showSource, let file = loadedText else { return false }
        return file.content.utf8.count < 256 * 1024
    }

    var body: some View {
        NavigationStack {
            Group {
                switch tab {
                case .changes: changes
                case .file: fileView
                }
            }
            .navigationTitle(request.fileName)
            .navigationSubtitle(request.folder ?? "")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { toolbar }
            .safeAreaInset(edge: .top, spacing: 0) {
                if hasBothTabs {
                    Picker("View", selection: $tab) {
                        Text("Changes").tag(Tab.changes)
                        Text("File").tag(Tab.file)
                    }
                    .pickerStyle(.segmented)
                    .padding(.horizontal, 16)
                    .padding(.vertical, 8)
                    .accessibilityIdentifier("fileViewerTabs")
                }
            }
        }
        .accessibilityIdentifier("fileViewer")
        .task { await fetch() }
    }

    // MARK: - Tabs

    @ViewBuilder
    private var changes: some View {
        if request.change != nil {
            CodeView(lines: changeLines, wrap: wrap, header: AnyView(changeNotices))
                .accessibilityIdentifier("fileViewerDiff")
                .onChange(of: fileLines.count, initial: true) { rebuildChangeLines() }
        }
    }

    @ViewBuilder
    private var changeNotices: some View {
        VStack(alignment: .leading, spacing: 8) {
            if request.failed {
                FileNotice(symbol: "exclamationmark.triangle.fill", tint: .red, text: "This change failed, so it wasn't applied.")
            }
            if request.changeTruncated {
                FileNotice(
                    symbol: "scissors", tint: .orange,
                    text: request.path != nil ? "The change is too long to show in full. The File tab has the whole file." : "The change is too long to show in full."
                )
            }
            if case .write = request.change {
                FileNotice(symbol: "doc.badge.plus", tint: .secondary, text: "The whole file as written.")
            }
            if request.path == nil, case .edits = request.change {
                FileNotice(symbol: "folder.badge.questionmark", tint: .secondary, text: "This file is outside the project, so only the change is shown.")
            }
        }
    }

    @ViewBuilder
    private var fileView: some View {
        switch load {
        case .loading:
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .accessibilityIdentifier("fileViewerLoading")
        case .failed(let error):
            ContentUnavailableView {
                Label(error.title, systemImage: error.symbol)
            } description: {
                Text(error.message)
            } actions: {
                if error.canRetry {
                    Button("Try again") { Task { await fetch() } }
                        .buttonStyle(.glass)
                }
            }
            .accessibilityIdentifier("fileViewerError")
        case .image(let image, _):
            ZoomableImage(image: image)
                .accessibilityIdentifier("fileViewerImage")
        case .text(let file):
            if rendersMarkdown {
                ScrollView {
                    VStack(alignment: .leading, spacing: 12) {
                        if file.truncated { truncatedNotice(file) }
                        MarkdownView(file.content)
                            .textSelection(.enabled)
                    }
                    .padding(16)
                }
                .accessibilityIdentifier("fileViewerMarkdown")
            } else {
                CodeView(
                    lines: fileLines, wrap: wrap,
                    header: file.truncated ? AnyView(truncatedNotice(file)) : nil
                )
                .accessibilityIdentifier("fileViewerText")
            }
        }
    }

    private func truncatedNotice(_ file: FileContent) -> some View {
        FileNotice(
            symbol: "scissors", tint: .orange,
            text: "Showing the first 1 MB of \(ByteCountFormatter.string(fromByteCount: Int64(file.size), countStyle: .file))."
        )
    }

    // MARK: - Toolbar

    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
        ToolbarItem(placement: .topBarLeading) {
            Menu {
                if tab == .file, isMarkdown, loadedText != nil {
                    Toggle("Show Source", systemImage: "chevron.left.forwardslash.chevron.right", isOn: $showSource)
                }
                Toggle("Wrap Lines", systemImage: "text.word.spacing", isOn: Binding(get: { wrap }, set: { wrapOverride = $0 }))
                if let text = copyableText {
                    Button("Copy", systemImage: "doc.on.doc") { UIPasteboard.general.string = text }
                    ShareLink(item: text, preview: SharePreview(request.fileName))
                }
                if case .image(let image, _) = load, tab == .file {
                    ShareLink(
                        item: Image(uiImage: image),
                        preview: SharePreview(request.fileName, image: Image(uiImage: image))
                    )
                }
            } label: {
                Image(systemName: "ellipsis")
            }
            .accessibilityLabel("More")
            .accessibilityIdentifier("fileViewerMenu")
        }
        ToolbarItem(placement: .topBarTrailing) {
            DoneButton()
        }
    }

    /// The file on the File tab; on Changes, the new text (what a Write wrote, or the edits' replacements).
    private var copyableText: String? {
        switch tab {
        case .file: return loadedText?.content
        case .changes:
            switch request.change {
            case .write(let content): return content
            case .diff(let diff): return diff
            case .edits(let replacements): return replacements.map(\.new).joined(separator: "\n\n")
            case nil: return nil
            }
        }
    }

    // MARK: - Loading

    private func fetch() async {
        guard let path = request.path else {
            load = .failed(.outsideProject)
            return
        }
        load = .loading
        do {
            switch try await store.backend.file(agentId: agentId, path: path) {
            case .text(let file):
                fileLines = FileDiff.fileLines(file.content)
                load = .text(file)
            case .image(let data, _):
                guard let image = UIImage(data: data) else {
                    load = .failed(.unsupported)
                    return
                }
                load = .image(image, data)
            }
        } catch {
            load = .failed(FileLoadError(error))
        }
    }

    private func rebuildChangeLines() {
        guard let change = request.change else { return }
        // Only an edit needs the file (to find its line numbers); a failed edit isn't in the file.
        let file: String? = if case .edits = change, !request.failed { loadedText?.content } else { nil }
        changeLines = FileDiff.lines(for: change, file: file)
    }
}

/// Why the File tab has no file.
enum FileLoadError: Equatable {
    case outsideProject
    case notFound
    case unsupported
    case tooLarge
    case other(String, retry: Bool)

    init(_ error: Error) {
        switch error as? RelayError {
        case .http(403, _, _): self = .outsideProject
        case .http(404, _, _): self = .notFound
        case .http(415, _, _): self = .unsupported
        case .http(413, _, _): self = .tooLarge
        case .unreachable, .http: self = .other(error.localizedDescription, retry: true)
        default: self = .other(error.localizedDescription, retry: false)
        }
    }

    var title: String {
        switch self {
        case .outsideProject: "Outside the project"
        case .notFound: "File not found"
        case .unsupported: "Can't show this file"
        case .tooLarge: "Image too large"
        case .other: "Couldn't load the file"
        }
    }

    var message: String {
        switch self {
        case .outsideProject: "Relay only opens files inside the agent's project folder."
        case .notFound: "It may have been moved or deleted since."
        case .unsupported: "Relay shows text files and images."
        case .tooLarge: "Relay shows images up to 20 MB."
        case .other(let message, _): message
        }
    }

    var symbol: String {
        switch self {
        case .outsideProject: "lock.doc"
        case .notFound: "questionmark.folder"
        case .unsupported, .tooLarge: "doc.questionmark"
        case .other: "wifi.exclamationmark"
        }
    }

    var canRetry: Bool {
        switch self {
        case .notFound: true
        case .other(_, let retry): retry
        default: false
        }
    }
}

private struct DoneButton: View {
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        Button("Done", systemImage: "xmark", role: .close) { dismiss() }
            .accessibilityIdentifier("fileViewerDone")
    }
}

/// A one-line note above the code: truncated, failed, outside the project.
struct FileNotice: View {
    let symbol: String
    let tint: Color
    let text: String

    var body: some View {
        Label {
            Text(text).foregroundStyle(.secondary)
        } icon: {
            Image(systemName: symbol).foregroundStyle(tint)
        }
        .font(.footnote)
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("fileNotice")
    }
}

/// Pinch or double-tap to zoom, like the attachment viewer.
private struct ZoomableImage: View {
    let image: UIImage
    @State private var scale: CGFloat = 1
    @GestureState private var pinch: CGFloat = 1

    var body: some View {
        Image(uiImage: image)
            .resizable()
            .scaledToFit()
            .scaleEffect(scale * pinch)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            .padding(16)
            .contentShape(.rect)
            .gesture(
                MagnifyGesture()
                    .updating($pinch) { value, state, _ in state = value.magnification }
                    .onEnded { value in withAnimation(.smooth) { scale = min(max(scale * value.magnification, 1), 6) } }
            )
            .onTapGesture(count: 2) { withAnimation(.smooth) { scale = scale > 1 ? 1 : 2.5 } }
            .accessibilityLabel("Image")
            .accessibilityAddTraits(.isImage)
    }
}
