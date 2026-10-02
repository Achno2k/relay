import RelayKit
import SwiftUI

/// Read-only review of what the agent's project has that no remote has yet: the branch, uncommitted files and
/// unpushed commits (api.md "Changes"). Fetched on open and on pull to refresh; no git actions.
struct ChangesSheet: View {
    let agentId: String
    let projectName: String
    let store: AppStore

    @State private var load: Load = .loading
    /// A refresh failed while the last good result is still on screen.
    @State private var refreshError: ChangesError?

    enum Load {
        case loading
        case loaded(Changes)
        case failed(ChangesError)
    }

    var body: some View {
        NavigationStack {
            content
                .navigationTitle("Changes")
                .navigationSubtitle(projectName)
                .navigationBarTitleDisplayMode(.inline)
                .toolbar {
                    ToolbarItem(placement: .topBarTrailing) { ChangesCloseButton() }
                }
                .navigationDestination(for: ChangesRoute.self) { route in
                    switch route {
                    case .file(let file):
                        ChangeDiffView(agentId: agentId, store: store, file: file, source: .uncommitted)
                    case .commit(let commit):
                        CommitDetailView(agentId: agentId, store: store, summary: commit)
                    case .commitFile(let file):
                        ChangeDiffView(agentId: agentId, store: store, file: file.file, source: .commit(file))
                    }
                }
        }
        .accessibilityIdentifier("changesSheet")
        .task { await fetch() }
    }

    @ViewBuilder
    private var content: some View {
        switch load {
        case .loading:
            ProgressView()
                .frame(maxWidth: .infinity, maxHeight: .infinity)
                .accessibilityIdentifier("changesLoading")
        case .failed(let error):
            List {
                ChangesErrorView(error: error) { Task { await fetch() } }
                    .listRowBackground(Color.clear)
            }
            .refreshable { await fetch() }
        case .loaded(let changes) where !changes.repo:
            List {
                ContentUnavailableView(
                    "Not a git repository", systemImage: "folder.badge.questionmark",
                    description: Text("\(projectName) isn't inside a git repository, so there's nothing to review.")
                )
                .listRowBackground(Color.clear)
                .accessibilityIdentifier("changesNotRepo")
            }
            .refreshable { await fetch() }
        case .loaded(let changes):
            list(changes)
        }
    }

    private func list(_ changes: Changes) -> some View {
        List {
            if let refreshError {
                Section {
                    ChangesInlineError(error: refreshError) { Task { await fetch() } }
                }
            }
            Section {
                BranchRow(changes: changes)
                    .listRowBackground(Color.clear)
                    .listRowInsets(EdgeInsets(top: 4, leading: 20, bottom: 0, trailing: 20))
            }
            if changes.files.isEmpty {
                Section {
                    ChangesEmptyRow(text: "No uncommitted changes", symbol: "checkmark.circle")
                        .accessibilityIdentifier("changesNoFiles")
                } header: {
                    ChangesSectionHeader(title: "Uncommitted", count: 0)
                }
            } else {
                let groups = ChangesDisplay.groups(changes.files) { $0 }
                ForEach(groups, id: \.group) { group in
                    Section {
                        ForEach(group.items, id: \.path) { file in
                            NavigationLink(value: ChangesRoute.file(file)) {
                                ChangedFileRow(file: file)
                            }
                            .accessibilityIdentifier("changesFile")
                        }
                    } header: {
                        ChangesSectionHeader(title: group.group.rawValue, count: group.items.count)
                    } footer: {
                        if changes.moreFiles, group.group == groups.last?.group {
                            Text("Showing the first \(changes.files.count) files.")
                        }
                    }
                }
            }
            Section {
                if changes.commits.isEmpty {
                    ChangesEmptyRow(text: "Everything is pushed", symbol: "checkmark.icloud")
                        .accessibilityIdentifier("changesNoCommits")
                } else {
                    ForEach(changes.commits) { commit in
                        NavigationLink(value: ChangesRoute.commit(commit)) {
                            CommitRow(commit: commit)
                        }
                        .accessibilityIdentifier("changesCommit")
                    }
                }
            } header: {
                ChangesSectionHeader(title: "Unpushed commits", count: changes.commits.count)
            } footer: {
                if changes.moreCommits {
                    Text("Showing the latest \(changes.commits.count) commits.")
                } else if changes.upstream == nil, !changes.commits.isEmpty {
                    Text("Commits that aren't on any remote branch yet.")
                }
            }
        }
        .listStyle(.insetGrouped)
        .refreshable { await fetch() }
        .accessibilityIdentifier("changesList")
    }

    private func fetch() async {
        do {
            let changes = try await store.backend.changes(agentId: agentId)
            refreshError = nil
            load = .loaded(changes)
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

enum ChangesRoute: Hashable {
    case file(ChangedFile)
    case commit(CommitSummary)
    case commitFile(CommitFile)
}

// MARK: - Rows

/// The branch, its upstream and ahead/behind, quiet above the lists.
private struct BranchRow: View {
    let changes: Changes

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Label(ChangesDisplay.branchTitle(changes), systemImage: "arrow.triangle.branch")
                .font(.subheadline.weight(.semibold))
                .lineLimit(1)
                .truncationMode(.middle)
            Text([ChangesDisplay.upstreamText(changes), ChangesDisplay.aheadBehind(changes)].compactMap { $0 }.joined(separator: " · "))
                .font(.footnote.monospacedDigit())
                .lineLimit(1)
                .truncationMode(.middle)
        }
        .foregroundStyle(.secondary)
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(
            [
                changes.branch.map { "Branch \($0)" } ?? "Detached HEAD",
                changes.upstream.map { "tracking \($0)" } ?? "no upstream branch",
                ChangesDisplay.aheadBehindLabel(changes),
            ].compactMap { $0 }.joined(separator: ", ")
        )
        .accessibilityIdentifier("changesBranch")
    }
}

/// File name, then folder and +/− in secondary. The section says what happened to it.
struct ChangedFileRow: View {
    let file: ChangedFile
    @Environment(\.dynamicTypeSize) private var typeSize

    var body: some View {
        // One line each, as in the sidebar; accessibility sizes wrap instead of cutting the name.
        let lines = typeSize.isAccessibilitySize ? 3 : 1
        VStack(alignment: .leading, spacing: 2) {
            Text(ChangesDisplay.renameTitle(file) ?? file.fileName)
                .font(.body)
                .lineLimit(lines)
                .truncationMode(.middle)
            Text(ChangesDisplay.detail(file))
                .font(.subheadline.monospacedDigit())
                .foregroundStyle(.secondary)
                .lineLimit(lines)
                .truncationMode(.head)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(file.accessibilityText)
    }
}

private struct CommitRow: View {
    let commit: CommitSummary
    @Environment(\.dynamicTypeSize) private var typeSize

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(commit.subject)
                .font(.body)
                .lineLimit(2)
            HStack(spacing: 6) {
                Text(commit.shortSha).monospaced()
                Text("·")
                Text(commit.time, format: .relative(presentation: .named, unitsStyle: .abbreviated))
                Text("·")
                Text(ChangesDisplay.counts(additions: commit.additions, deletions: commit.deletions)).monospacedDigit()
            }
            .font(.subheadline)
            .foregroundStyle(.secondary)
            .lineLimit(typeSize.isAccessibilitySize ? 2 : 1)
        }
        .padding(.vertical, 2)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(
            "\(commit.subject), \(commit.shortSha), \(commit.time.formatted(.relative(presentation: .named))), "
                + "\(commit.fileCount) \(commit.fileCount == 1 ? "file" : "files"), "
                + ChangesDisplay.countLabel(additions: commit.additions, deletions: commit.deletions)
        )
    }
}

/// A section title with its count in tertiary.
struct ChangesSectionHeader: View {
    let title: String
    let count: Int

    var body: some View {
        HStack(spacing: 6) {
            Text(title)
            if count > 0 { Text("\(count)").foregroundStyle(.tertiary) }
        }
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isHeader)
    }
}

private struct ChangesEmptyRow: View {
    let text: String
    let symbol: String

    var body: some View {
        Label(text, systemImage: symbol)
            .foregroundStyle(.secondary)
    }
}

/// A whole-screen error with Try again.
struct ChangesErrorView: View {
    let error: ChangesError
    let retry: () -> Void

    var body: some View {
        ContentUnavailableView {
            Label(error.title, systemImage: error.symbol)
        } description: {
            Text(error.message)
        } actions: {
            if error.canRetry {
                Button("Try again", action: retry)
                    .buttonStyle(.glass)
            }
        }
        .accessibilityIdentifier("changesError")
    }
}

/// A failed refresh above content that's still shown.
struct ChangesInlineError: View {
    let error: ChangesError
    let retry: () -> Void

    var body: some View {
        HStack(spacing: 12) {
            Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(.orange)
            VStack(alignment: .leading, spacing: 2) {
                Text(error.title).font(.subheadline.weight(.semibold))
                Text(error.message).font(.caption).foregroundStyle(.secondary)
            }
            Spacer(minLength: 8)
            if error.canRetry {
                Button("Retry", action: retry)
                    .buttonStyle(.glass)
                    .controlSize(.small)
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityIdentifier("changesRefreshError")
    }
}

struct ChangesCloseButton: View {
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        Button("Done", systemImage: "xmark", role: .close) { dismiss() }
            .accessibilityIdentifier("changesDone")
    }
}

// MARK: - Presenting

/// The chat toolbar's Changes button opens this sheet for the selected agent.
struct ChangesTarget: Identifiable, Hashable {
    let agentId: String
    let projectName: String
    var id: String { agentId }
}

extension View {
    /// Shows the Changes sheet while `target` is set. An approval outranks it, as it does the file viewer.
    func changesSheet(_ target: Binding<ChangesTarget?>, store: AppStore) -> some View {
        modifier(ChangesPresenter(store: store, target: target))
    }
}

private struct ChangesPresenter: ViewModifier {
    let store: AppStore
    @Binding var target: ChangesTarget?
    @State private var heldApproval = false

    func body(content: Content) -> some View {
        content
            .sheet(item: $target, onDismiss: presentHeldApproval) { target in
                ChangesSheet(agentId: target.agentId, projectName: target.projectName, store: store)
            }
            .onChange(of: store.isApprovalSheetPresented) { _, shown in
                // One sheet at a time: close Changes, show the approval once it's gone.
                guard shown, target != nil else { return }
                heldApproval = true
                store.isApprovalSheetPresented = false
                target = nil
            }
            .task(id: store.selectedAgentId) { openDemo() }
    }

    private func presentHeldApproval() {
        guard heldApproval else { return }
        heldApproval = false
        if store.approval != nil { store.isApprovalSheetPresented = true }
    }

    /// `-demo changes` opens the selected agent's Changes (screenshots, UI tests).
    private func openDemo() {
        guard target == nil, LaunchOptions.current.isDemo("changes"), let agent = store.selectedAgent else { return }
        target = ChangesTarget(agentId: agent.id, projectName: agent.cwdName)
    }
}
