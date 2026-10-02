import RelayKit
import SwiftUI
import UIKit

/// One file's diff: an uncommitted file (fetched) or a file in a commit (already in the commit's detail).
/// "Open file" shows the file as it is now in the file viewer, unless the file was deleted.
struct ChangeDiffView: View {
    let agentId: String
    let store: AppStore
    let file: ChangedFile
    let source: Source

    enum Source: Hashable {
        case uncommitted
        case commit(CommitFile)
    }

    @State private var load: Load
    @State private var wrap = false
    @State private var openRequest: FileRequest?

    enum Load {
        case loading
        case loaded(diff: String, binary: Bool, truncated: Bool, lines: [CodeLine])
        case failed(ChangesError)
    }

    init(agentId: String, store: AppStore, file: ChangedFile, source: Source) {
        self.agentId = agentId
        self.store = store
        self.file = file
        self.source = source
        if case .commit(let commitFile) = source {
            _load = State(initialValue: Self.loaded(commitFile.diff, binary: commitFile.file.binary, truncated: commitFile.truncated))
        } else {
            _load = State(initialValue: .loading)
        }
    }

    var body: some View {
        content
            .navigationTitle(file.fileName)
            .navigationBarTitleDisplayMode(.inline)
            .toolbar { toolbar }
            .sheet(item: $openRequest) { request in
                FileViewerSheet(request: request, agentId: agentId, store: store)
            }
            .task {
                // Not again when coming back from the file viewer.
                if case .loading = load { await fetch() }
            }
    }

    @ViewBuilder
    private var content: some View {
        switch load {
        case .loading:
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed(let error):
            ChangesErrorView(error: error) { Task { await fetch() } }
        case .loaded(_, let binary, _, _) where binary:
            ContentUnavailableView {
                Label("Binary file", systemImage: "doc.zipper")
            } description: {
                Text(file.status == .deleted ? "There's no text diff for this file." : "There's no text diff. Open the file to see it.")
            } actions: {
                if ChangesDisplay.canOpen(file) {
                    Button("Open file") { openRequest = file.openRequest }
                        .buttonStyle(.glass)
                }
            }
            .accessibilityIdentifier("changesBinary")
        case .loaded(let diff, _, let truncated, let lines):
            let notices = ChangesDisplay.notices(for: file, diff: diff, truncated: truncated)
            CodeView(lines: lines, wrap: wrap, header: notices.isEmpty ? nil : AnyView(noticeStack(notices)))
                .accessibilityIdentifier("changesDiff")
                .refreshable {
                    if case .uncommitted = source { await fetch() }
                }
        }
    }

    private func noticeStack(_ notices: [DiffNotice]) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            ForEach(notices, id: \.self) { FileNotice(symbol: $0.symbol, tint: $0.tint, text: $0.text) }
        }
    }

    @ToolbarContentBuilder
    private var toolbar: some ToolbarContent {
        // A bare file name ("Hero.tsx") isn't a human-readable label, so the title reads as a phrase.
        ToolbarItem(placement: .principal) {
            VStack(spacing: 0) {
                Text(file.fileName)
                    .font(.headline)
                    .lineLimit(1)
                    .truncationMode(.middle)
                if let folder = file.folder {
                    Text(folder)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .truncationMode(.head)
                }
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("Changes to \(file.fileName)" + (file.folder.map { " in \($0)" } ?? ""))
            .accessibilityAddTraits(.isHeader)
        }
        ToolbarItem(placement: .topBarTrailing) {
            Menu {
                Toggle("Wrap Lines", systemImage: "text.word.spacing", isOn: $wrap)
                if case .loaded(let diff, _, _, _) = load, !diff.isEmpty {
                    Button("Copy Diff", systemImage: "doc.on.doc") { UIPasteboard.general.string = diff }
                }
            } label: {
                Image(systemName: "ellipsis")
            }
            .accessibilityLabel("More")
            .accessibilityIdentifier("changesDiffMenu")
        }
        if ChangesDisplay.canOpen(file) {
            ToolbarItem(placement: .topBarTrailing) {
                Button("Open file", systemImage: "doc.text") { openRequest = file.openRequest }
                    .accessibilityIdentifier("changesOpenFile")
            }
        }
    }

    private func fetch() async {
        guard case .uncommitted = source else { return }
        if case .failed = load { load = .loading }
        do {
            let text = try await store.backend.changesDiff(agentId: agentId, path: file.path)
            load = Self.loaded(text.diff, binary: text.binary, truncated: text.truncated)
        } catch is CancellationError {
        } catch {
            // A refresh that fails keeps the diff on screen.
            if case .loaded = load { return }
            load = .failed(ChangesError(error))
        }
    }

    private static func loaded(_ diff: String, binary: Bool, truncated: Bool) -> Load {
        .loaded(diff: diff, binary: binary, truncated: truncated, lines: ChangesDisplay.diffLines(diff))
    }
}

/// A commit's message and files; each file opens its diff.
struct CommitDetailView: View {
    let agentId: String
    let store: AppStore
    let summary: CommitSummary

    @State private var load: Load = .loading
    @State private var refreshError: ChangesError?

    enum Load {
        case loading
        case loaded(CommitDetail)
        case failed(ChangesError)
    }

    var body: some View {
        content
            .navigationTitle(summary.shortSha)
            .navigationBarTitleDisplayMode(.inline)
            .task {
                // Not again when coming back from a file's diff.
                if case .loading = load { await fetch() }
            }
            .accessibilityIdentifier("commitDetail")
    }

    @ViewBuilder
    private var content: some View {
        switch load {
        case .loading:
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        case .failed(let error):
            List {
                ChangesErrorView(error: error) { Task { await fetch() } }
                    .listRowBackground(Color.clear)
            }
            .refreshable { await fetch() }
        case .loaded(let detail):
            list(detail)
        }
    }

    private func list(_ detail: CommitDetail) -> some View {
        List {
            if let refreshError {
                Section {
                    ChangesInlineError(error: refreshError) { Task { await fetch() } }
                }
            }
            Section {
                VStack(alignment: .leading, spacing: 8) {
                    Text(detail.subject)
                        .font(.headline)
                        .textSelection(.enabled)
                    if !detail.body.isEmpty {
                        Text(detail.body)
                            .font(.subheadline)
                            .foregroundStyle(.secondary)
                            .textSelection(.enabled)
                    }
                }
                .padding(.vertical, 4)
                .accessibilityIdentifier("commitMessage")
                HStack(spacing: 6) {
                    Text(detail.shortSha).monospaced()
                    Text("·")
                    Text(detail.time, format: .relative(presentation: .named))
                    Text("·")
                    Text(ChangesDisplay.counts(additions: summary.additions, deletions: summary.deletions)).monospacedDigit()
                }
                .font(.subheadline)
                .lineLimit(1)
                .foregroundStyle(.secondary)
                .contextMenu {
                    Button("Copy Commit Hash", systemImage: "doc.on.doc") { UIPasteboard.general.string = detail.sha }
                }
            }
            let groups = ChangesDisplay.groups(detail.files) { $0.file }
            ForEach(groups, id: \.group) { group in
                Section {
                    ForEach(group.items, id: \.file.path) { file in
                        NavigationLink(value: ChangesRoute.commitFile(file)) {
                            ChangedFileRow(file: file.file)
                        }
                        .accessibilityIdentifier("commitFile")
                    }
                } header: {
                    ChangesSectionHeader(title: group.group.rawValue, count: group.items.count)
                } footer: {
                    if detail.truncated, group.group == groups.last?.group {
                        Text("Some diffs are too long to show in full. Open a file to see all of it.")
                    }
                }
            }
        }
        .listStyle(.insetGrouped)
        .refreshable { await fetch() }
    }

    private func fetch() async {
        do {
            let detail = try await store.backend.commit(agentId: agentId, sha: summary.sha)
            refreshError = nil
            load = .loaded(detail)
        } catch is CancellationError {
        } catch {
            if case .loaded = load {
                refreshError = ChangesError(error)
            } else {
                load = .failed(ChangesError(error))
            }
        }
    }
}
