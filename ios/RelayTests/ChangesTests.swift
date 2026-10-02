import Foundation
import RelayKit
import Testing
@testable import Relay

/// Round 13: the read-only Changes screen (api.md "Changes"): fixtures, the three calls, wording and diff rows.
@Suite("Round 13 changes")
struct ChangesTests {
    // MARK: - Fixtures

    @Test func changesFixtureDecodes() throws {
        let changes = try FixtureFiles.decode(Changes.self, "changes.json")
        #expect(changes.repo)
        #expect(changes.branch == "feat/voice")
        #expect(changes.upstream == "origin/feat/voice")
        #expect(changes.ahead == 2 && changes.behind == 0)
        #expect(changes.files.map(\.path) == changes.files.map(\.path).sorted())
        #expect(Set(changes.files.map(\.status)).isSuperset(of: [.modified, .deleted, .untracked, .renamed, .added]))
        let renamed = try #require(changes.files.first { $0.status == .renamed })
        #expect(renamed.oldPath != nil)
        let binary = try #require(changes.files.first { $0.binary })
        #expect(binary.additions == 0 && binary.deletions == 0)
        #expect(!changes.moreFiles && !changes.moreCommits)
        #expect(changes.commits.count == 2)
        let newest = changes.commits[0]
        #expect(newest.sha.count == 40 && newest.sha.hasPrefix(newest.shortSha))
        #expect(newest.time > changes.commits[1].time)
        #expect(newest.time == RelayJSON.date(from: "2026-10-03T09:41:00Z"))
    }

    @Test func diffFixtureDecodesAndParses() throws {
        let diff = try FixtureFiles.decode(FileDiffText.self, "changes-diff.json")
        #expect(diff.path == "Sources/App.swift")
        #expect(!diff.binary && !diff.truncated)
        let lines = ChangesDisplay.diffLines(diff.diff)
        // The file header row is left out: the screen's title names the file.
        #expect(!lines.contains { $0.kind == .file })
        #expect(lines.first?.kind == .hunk)
        #expect(lines.filter { $0.kind == .added }.count == 3)
        #expect(lines.filter { $0.kind == .removed }.count == 1)
        // Line numbers follow the hunk header: the added `import Speech` is new line 2.
        #expect(lines.first { $0.kind == .added }?.number == 2)
    }

    @Test func commitFixtureDecodes() throws {
        let commit = try FixtureFiles.decode(CommitDetail.self, "changes-commit.json")
        #expect(commit.shortSha == "7d3e9a1")
        #expect(commit.body.hasPrefix("Switching chats"))
        #expect(!commit.truncated)
        #expect(commit.files.map(\.file.status) == [.modified, .added])
        #expect(commit.files.allSatisfy { !$0.diff.isEmpty && !$0.truncated })
        // An added file diffs against /dev/null: every row is an addition.
        let added = ChangesDisplay.diffLines(commit.files[1].diff).filter { $0.kind != .hunk }
        #expect(added.allSatisfy { $0.kind == .added })
        // The summary in changes.json matches the detail.
        let summary = try #require(try FixtureFiles.decode(Changes.self, "changes.json").commits.first)
        #expect(summary.sha == commit.sha && summary.fileCount == commit.files.count)
    }

    /// `git diff` puts mode and rename lines between `diff --git` and `---`; they aren't code.
    @Test func gitExtendedHeadersAreNotContext() {
        let diff = """
        diff --git a/old.sh b/run.sh
        old mode 100644
        new mode 100755
        similarity index 90%
        rename from old.sh
        rename to run.sh
        index 1111111..2222222
        --- a/old.sh
        +++ b/run.sh
        @@ -1,2 +1,2 @@
         #!/bin/sh
        -echo old
        +echo new
        diff --git a/gone.txt b/gone.txt
        deleted file mode 100644
        --- a/gone.txt
        +++ /dev/null
        @@ -1 +0,0 @@
        -bye

        """
        let lines = ChangesDisplay.diffLines(diff)
        #expect(lines.map(\.kind) == [.hunk, .context, .removed, .added, .hunk, .removed])
        #expect(lines[1].text == "#!/bin/sh")
    }

    @Test func notARepoHasOnlyRepo() throws {
        let changes = try RelayJSON.decoder().decode(Changes.self, from: Data(#"{"repo":false}"#.utf8))
        #expect(!changes.repo && changes.files.isEmpty && changes.commits.isEmpty && changes.branch == nil)
    }

    @Test func unknownStatusDoesNotFailTheList() throws {
        let json = #"{"path":"a","status":"typechange","additions":0,"deletions":0,"binary":false}"#
        #expect(try RelayJSON.decoder().decode(ChangedFile.self, from: Data(json.utf8)).status == .unknown)
    }

    // MARK: - APIClient

    private func client(_ base: String) -> APIClient {
        APIClient(baseURL: URL(string: base)!, token: "t", session: StubURLProtocol.session())
    }

    @Test func changesCallsTheAgentsChanges() async throws {
        let api = client("http://changes-list.test")
        StubURLProtocol.stub(
            URL(string: "http://changes-list.test/agents/w1%3Ap1/changes")!,
            .init(data: try FixtureFiles.data("changes.json"))
        )
        #expect(try await api.changes(agentId: "w1:p1").branch == "feat/voice")
    }

    @Test func diffPathIsPercentEncoded() async throws {
        let api = client("http://changes-diff.test")
        StubURLProtocol.stub(
            URL(string: "http://changes-diff.test/agents/w1%3Ap1/changes/diff?path=c%2B%2B/a%20b%26c.swift")!,
            .init(data: Data(#"{"path":"c++/a b&c.swift","diff":"","binary":true,"truncated":false}"#.utf8))
        )
        let diff = try await api.changesDiff(agentId: "w1:p1", path: "c++/a b&c.swift")
        #expect(diff.binary && diff.path == "c++/a b&c.swift")
    }

    /// 403 on a diff is "outside the project", not a bad token: it must not send the user to re-pair.
    @Test func diffForbiddenIsNotUnauthorized() async throws {
        let api = client("http://changes-403.test")
        StubURLProtocol.stub(
            URL(string: "http://changes-403.test/agents/w1%3Ap1/changes/diff?path=../x")!,
            .init(data: Data(#"{"error":{"code":"forbidden","message":"outside"}}"#.utf8), statusCode: 403)
        )
        await #expect(throws: RelayError.http(status: 403, code: "forbidden", message: "outside")) {
            try await api.changesDiff(agentId: "w1:p1", path: "../x")
        }
    }

    @Test func commitCallsTheShaPath() async throws {
        let api = client("http://changes-commit.test")
        StubURLProtocol.stub(
            URL(string: "http://changes-commit.test/agents/w1%3Ap1/changes/commits/7d3e9a1")!,
            .init(data: try FixtureFiles.data("changes-commit.json"))
        )
        #expect(try await api.commit(agentId: "w1:p1", sha: "7d3e9a1").files.count == 2)
    }

    @Test func gitErrorsMapToScreenStates() {
        #expect(ChangesError(RelayError.http(status: 503, code: "unavailable", message: nil)).title == "git isn't installed")
        #expect(ChangesError(RelayError.http(status: 504, code: "timeout", message: nil)).canRetry)
        #expect(!ChangesError(RelayError.http(status: 403, code: "forbidden", message: nil)).canRetry)
        #expect(ChangesError(RelayError.http(status: 404, code: "not_found", message: nil)).title == "Nothing to show")
        #expect(ChangesError(RelayError.unreachable(timedOut: true)).canRetry)
    }

    /// Multi-machine: the keyed agent id goes out raw.
    @Test func namespacedBackendStripsTheMachineKey() async throws {
        actor Recorder: Backend {
            var ids: [String] = []
            func changes(agentId: String) async throws -> Changes { ids.append(agentId); return Changes(repo: false) }
            func changesDiff(agentId: String, path: String) async throws -> FileDiffText {
                ids.append(agentId); return FileDiffText(path: path, diff: "")
            }
            func commit(agentId: String, sha: String) async throws -> CommitDetail {
                ids.append(agentId); return CommitDetail(sha: sha, shortSha: sha, subject: "", time: .now, files: [])
            }
            func workspaces() async throws -> [Workspace] { [] }
            func agents() async throws -> [Agent] { [] }
            func messages(agentId: String, before: String?, limit: Int) async throws -> MessagePage { MessagePage(messages: [], hasMore: false) }
            func prompt(agentId: String, text: String, attachments: [String]) async throws {}
            func uploadAttachment(
                agentId: String, data: Data, filename: String, contentType: String, progress: @escaping @Sendable (Double) -> Void
            ) async throws -> RelayKit.Attachment { throw RelayError.badResponse }
            func attachmentData(agentId: String, attachmentId: String) async throws -> Data { Data() }
            func machine() async throws -> Machine { throw RelayError.badResponse }
            func sendKeys(agentId: String, keys: [String]) async throws {}
            func sendText(agentId: String, text: String, submit: Bool) async throws {}
            func approval(agentId: String) async throws -> Approval? { nil }
            func createAgent(_ request: CreateAgentRequest) async throws -> Agent { throw RelayError.badResponse }
            func controls() async throws -> ControlsCatalog { ControlsCatalog(models: [], modes: [], efforts: []) }
            func kindControls(kind: String) async throws -> AgentControlsInfo { throw RelayError.badResponse }
            func agentControls(agentId: String) async throws -> AgentControlsInfo { throw RelayError.badResponse }
            func control(agentId: String, _ request: ControlRequest) async throws -> Agent { throw RelayError.badResponse }
            nonisolated func events() -> AsyncStream<ConnectionEvent> { AsyncStream { $0.finish() } }
            func usage() async throws -> UsageSnapshot { UsageSnapshot(providers: []) }
            func refreshUsage() async throws {}
        }
        let inner = Recorder()
        let backend = NamespacedBackend(machineId: "m1", inner: inner)
        let key = MachineKey.make("m1", "w1:p1")
        _ = try await backend.changes(agentId: key)
        _ = try await backend.changesDiff(agentId: key, path: "a")
        _ = try await backend.commit(agentId: key, sha: "abcd")
        #expect(await inner.ids == ["w1:p1", "w1:p1", "w1:p1"])
    }

    // MARK: - Wording

    @Test func aheadBehind() {
        var changes = Changes(repo: true, branch: "main", upstream: "origin/main", ahead: 2, behind: 1)
        #expect(ChangesDisplay.aheadBehind(changes) == "↑2 ↓1")
        #expect(ChangesDisplay.aheadBehindLabel(changes) == "2 ahead, 1 behind")
        changes.behind = 0
        #expect(ChangesDisplay.aheadBehind(changes) == "↑2")
        changes.ahead = 0
        #expect(ChangesDisplay.aheadBehind(changes) == "Up to date")
        changes.upstream = nil
        changes.ahead = nil
        changes.behind = nil
        #expect(ChangesDisplay.aheadBehind(changes) == nil)
        #expect(ChangesDisplay.upstreamText(changes) == "No upstream branch")
        changes.branch = nil
        #expect(ChangesDisplay.branchTitle(changes) == "Detached HEAD")
    }

    @Test func deletedFilesCannotBeOpened() {
        #expect(!ChangesDisplay.canOpen(ChangedFile(path: "a", status: .deleted)))
        for status in [ChangeStatus.modified, .added, .renamed, .untracked, .conflicted] {
            #expect(ChangesDisplay.canOpen(ChangedFile(path: "a", status: status)))
        }
        let request = ChangedFile(path: "Sources/App.swift", status: .modified).openRequest
        // The viewer opens on the File tab: a path and no change of its own.
        #expect(request.path == "Sources/App.swift" && request.change == nil && request.fileName == "App.swift")
    }

    @Test func notices() {
        let pureRename = ChangedFile(path: "b.swift", oldPath: "a.swift", status: .renamed)
        #expect(ChangesDisplay.notices(for: pureRename, diff: "", truncated: false) == [.renamed(from: "a.swift"), .sameContent])
        let modeOnly = ChangedFile(path: "run.sh", status: .modified)
        #expect(ChangesDisplay.notices(for: modeOnly, diff: "", truncated: false) == [.noLineChanges])
        let big = ChangedFile(path: "big.json", status: .modified, additions: 9000)
        #expect(ChangesDisplay.notices(for: big, diff: "@@ -1 +1 @@\n-a\n+b\n", truncated: true) == [.truncated(canOpen: true)])
        let gone = ChangedFile(path: "big.json", status: .deleted, deletions: 9000)
        #expect(ChangesDisplay.notices(for: gone, diff: "x", truncated: true) == [.truncated(canOpen: false)])
        #expect(ChangesDisplay.notices(for: ChangedFile(path: "a.png", status: .added, binary: true), diff: "", truncated: false).isEmpty)
    }

    @Test func fileRowText() {
        let file = ChangedFile(path: "Sources/Recorder.swift", oldPath: "Sources/Audio.swift", status: .renamed, additions: 1, deletions: 2)
        #expect(file.fileName == "Recorder.swift" && file.folder == "Sources")
        #expect(file.accessibilityText == "Recorder.swift, Renamed, 1 addition, 2 deletions, in Sources, from Sources/Audio.swift")
        #expect(ChangedFile(path: "README.md", status: .untracked).folder == nil)
    }

    /// Update 2: sections by status, in a fixed order, empty ones left out; New is added + untracked.
    @Test func filesGroupByStatus() throws {
        let changes = try FixtureFiles.decode(Changes.self, "changes.json")
        let groups = ChangesDisplay.groups(changes.files) { $0 }
        #expect(groups.map(\.group) == [.modified, .new, .deleted, .renamed])
        #expect(groups[0].items.map(\.path) == ["Assets/icon.png", "Sources/App.swift"])
        #expect(groups[1].items.map(\.path) == ["Sources/Dictation.swift", "Sources/Settings.swift"])
        let conflicted = ChangesDisplay.groups([ChangedFile(path: "a", status: .conflicted), ChangedFile(path: "b", status: .unknown)]) { $0 }
        #expect(conflicted.map(\.group) == [.modified, .conflicts])
        #expect(ChangeGroup.allCases.map(\.rawValue) == ["Modified", "New", "Deleted", "Renamed", "Conflicts"])
        let commit = try FixtureFiles.decode(CommitDetail.self, "changes-commit.json")
        #expect(ChangesDisplay.groups(commit.files) { $0.file }.map(\.group) == [.modified, .new])
    }

    @Test func rowText() {
        let inPlace = ChangedFile(path: "Sources/Recorder.swift", oldPath: "Sources/Audio.swift", status: .renamed, additions: 2, deletions: 2)
        #expect(ChangesDisplay.renameTitle(inPlace) == "Audio.swift → Recorder.swift")
        #expect(ChangesDisplay.detail(inPlace) == "Sources · +2 −2")
        let moved = ChangedFile(path: "src/b/x.swift", oldPath: "src/a/x.swift", status: .renamed)
        #expect(ChangesDisplay.renameTitle(moved) == "src/a/x.swift → src/b/x.swift")
        // The paths already show the folders.
        #expect(ChangesDisplay.detail(moved) == "+0 −0")
        #expect(ChangesDisplay.detail(ChangedFile(path: "Assets/icon.png", status: .modified, binary: true)) == "Assets · Binary")
        #expect(ChangesDisplay.detail(ChangedFile(path: "README.md", status: .deleted, deletions: 14)) == "+0 −14")
        #expect(ChangesDisplay.renameTitle(ChangedFile(path: "a", status: .modified)) == nil)
    }
}
