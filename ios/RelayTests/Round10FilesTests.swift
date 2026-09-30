import Foundation
import RelayKit
import Testing
@testable import Relay

/// Round 10 B2: the file viewer ("tap files in chat"): the file call, the tap targets and the diff rows.
@Suite("Round 10 files")
struct Round10FilesTests {
    // MARK: - APIClient.file

    private func client(_ base: String) -> APIClient {
        APIClient(baseURL: URL(string: base)!, token: "t", session: StubURLProtocol.session())
    }

    @Test func textFileDecodesFileContent() async throws {
        let api = client("http://files-text.test")
        let json = #"{"path":"src/a.swift","content":"let a = 1\n","size":10,"truncated":false,"language":"swift"}"#
        StubURLProtocol.stub(URL(string: "http://files-text.test/agents/w1%3Ap1/file?path=src/a.swift")!, .init(data: Data(json.utf8)))
        let file = try await api.file(agentId: "w1:p1", path: "src/a.swift")
        #expect(file == .text(FileContent(path: "src/a.swift", content: "let a = 1\n", size: 10, language: "swift")))
    }

    @Test func imageComesBackAsBytes() async throws {
        let api = client("http://files-image.test")
        let bytes = Data([0x89, 0x50, 0x4E, 0x47])
        StubURLProtocol.stub(
            URL(string: "http://files-image.test/agents/w1%3Ap1/file?path=a.png")!,
            .init(data: bytes, contentType: "image/png")
        )
        #expect(try await api.file(agentId: "w1:p1", path: "a.png") == .image(bytes, contentType: "image/png"))
    }

    /// `+`, `&`, `#`, `?` and spaces must reach the bridge percent-encoded; `/` stays readable.
    @Test func pathIsPercentEncoded() async throws {
        let api = client("http://files-enc.test")
        let json = #"{"path":"c++/a b&c#d?.md","content":"","size":0,"truncated":false}"#
        StubURLProtocol.stub(
            URL(string: "http://files-enc.test/agents/w1%3Ap1/file?path=c%2B%2B/a%20b%26c%23d%3F.md")!,
            .init(data: Data(json.utf8))
        )
        let file = try await api.file(agentId: "w1:p1", path: "c++/a b&c#d?.md")
        #expect(file == .text(FileContent(path: "c++/a b&c#d?.md", content: "", size: 0)))
    }

    /// 403 here is "outside the project", not a bad token: it must not send the user to re-pair.
    @Test func forbiddenIsNotUnauthorized() async throws {
        let api = client("http://files-403.test")
        let body = #"{"error":{"code":"forbidden","message":"outside"}}"#
        StubURLProtocol.stub(
            URL(string: "http://files-403.test/agents/w1%3Ap1/file?path=../x")!,
            .init(data: Data(body.utf8), statusCode: 403)
        )
        await #expect(throws: RelayError.http(status: 403, code: "forbidden", message: "outside")) {
            try await api.file(agentId: "w1:p1", path: "../x")
        }
    }

    @Test func errorsMapToViewerStates() {
        #expect(FileLoadError(RelayError.http(status: 403, code: "forbidden", message: nil)) == .outsideProject)
        #expect(FileLoadError(RelayError.http(status: 404, code: "not_found", message: nil)) == .notFound)
        #expect(FileLoadError(RelayError.http(status: 415, code: "unsupported", message: nil)) == .unsupported)
        #expect(FileLoadError(RelayError.http(status: 413, code: "too_large", message: nil)) == .tooLarge)
        #expect(FileLoadError(RelayError.unreachable(timedOut: false)).canRetry)
    }

    // MARK: - Tap targets

    private func step(_ path: String?, _ edit: ToolEdit?, isError: Bool = false) -> ToolStep {
        ToolStep(id: "t", name: "Edit", summary: "Edited x", isError: isError, preview: nil, finished: true, path: path, edit: edit)
    }

    @Test func rowsWithoutPathOrEditAreNotTappable() {
        #expect(FileRequest(step: step(nil, nil)) == nil)
        // An edit kind this app doesn't know, with no path, has nothing to show.
        #expect(FileRequest(step: step(nil, ToolEdit(kind: .unknown))) == nil)
    }

    @Test func readRowOpensTheFile() throws {
        let request = try #require(FileRequest(step: step("src/app/Main.swift", nil)))
        #expect(request.change == nil)
        #expect(request.fileName == "Main.swift")
        #expect(request.folder == "src/app")
    }

    @Test func editRowCarriesItsChange() throws {
        let edit = ToolEdit(kind: .edit, changes: [ToolEditChange(old: "a", new: "b")], truncated: true)
        let request = try #require(FileRequest(step: step("a.txt", edit, isError: true)))
        #expect(request.change == .edits([.init(old: "a", new: "b")]))
        #expect(request.changeTruncated)
        #expect(request.failed)
        #expect(request.folder == nil)
    }

    /// codex's multi-file edit has no path, only a diff: still tappable, Changes only.
    @Test func diffWithoutPathIsTappable() throws {
        let request = try #require(FileRequest(step: step(nil, ToolEdit(kind: .diff, diff: "--- a/x\n+++ b/x\n"))))
        #expect(request.path == nil)
        #expect(request.fileName == "Edited x")
    }

    @Test func fixtureEditsDecodeIntoRequests() throws {
        let url = URL(fileURLWithPath: #filePath).deletingLastPathComponent().appending(path: "../../docs/fixtures/messages-edits.json")
        let page = try RelayJSON.decoder().decode(MessagePage.self, from: Data(contentsOf: url))
        let steps = ChatItem.build(from: page.messages).flatMap { item -> [ToolStep] in
            if case .tools(_, let steps, _) = item { steps } else { [] }
        }
        let requests = Dictionary(uniqueKeysWithValues: steps.compactMap { s in FileRequest(step: s).map { (s.id, $0) } })
        #expect(requests["t1"]?.path == "src/auth.py")
        #expect(requests["t1"]?.change == nil)
        if case .edits(let r) = requests["t5"]?.change { #expect(r.count == 2) } else { Issue.record("t5 has no edits") }
        if case .write = requests["t6"]?.change {} else { Issue.record("t6 isn't a write") }
        if case .diff = requests["t7"]?.change {} else { Issue.record("t7 isn't a diff") }
        #expect(requests["t7"]?.path == nil)
        // ExitPlanMode has neither a path nor an edit.
        #expect(requests["t3"] == nil)
    }

    // MARK: - Diff rows

    @Test func splitIgnoresTrailingNewline() {
        #expect(FileDiff.split("") == [])
        #expect(FileDiff.split("a\nb\n") == ["a", "b"])
        #expect(FileDiff.split("a\r\n\nb") == ["a", "", "b"])
    }

    @Test func lineDiffKeepsContextAndOrdersRemovalsFirst() {
        let lines = FileDiff.lineDiff(old: ["a", "b", "c"], new: ["a", "B", "c", "d"], start: 10)
        #expect(lines.map(\.kind) == [.context, .removed, .added, .context, .added])
        #expect(lines.map(\.text) == ["a", "b", "B", "c", "d"])
        #expect(lines.map(\.number) == [10, 11, 11, 12, 13])
    }

    @Test func editFindsItsLineInTheFile() {
        let change = FileChange.edits([.init(old: "x = 1", new: "x = 2")])
        let lines = FileDiff.lines(for: change, file: "one\ntwo\nx = 2\n")
        #expect(lines.first?.kind == .hunk)
        #expect(lines.first?.text == "Line 3")
        #expect(lines.dropFirst().map(\.number) == [3, 3])
        // Without the file there's nothing to place it by: no header, no numbers.
        let bare = FileDiff.lines(for: change)
        #expect(bare.map(\.kind) == [.removed, .added])
        #expect(bare.allSatisfy { $0.number == nil })
    }

    @Test func multiEditLabelsEachChange() {
        let change = FileChange.edits([.init(old: "a", new: "b"), .init(old: "c", new: "d")])
        let hunks = FileDiff.lines(for: change).filter { $0.kind == .hunk }.map(\.text)
        #expect(hunks == ["Change 1 of 2", "Change 2 of 2"])
    }

    @Test func writeIsAllAdded() {
        let lines = FileDiff.lines(for: .write("a\nb\n"))
        #expect(lines.map(\.kind) == [.added, .added])
        #expect(lines.map(\.number) == [1, 2])
    }

    @Test func unifiedDiffSplitsFilesAndNumbersLines() {
        let diff = """
        --- a/README.md
        +++ b/README.md
        @@ -1,3 +1,3 @@ intro
         # auth
        -old line
        +new line
        --- /dev/null
        +++ b/new.txt
        @@ -0,0 +1,1 @@
        +hello
        \\ No newline at end of file
        """
        let lines = FileDiff.parseUnified(diff)
        #expect(lines.map(\.kind) == [.file, .hunk, .context, .removed, .added, .file, .hunk, .added])
        #expect(lines[0].text == "README.md")
        #expect(lines[1].text == "Line 1 · intro")
        #expect(lines[2...4].map(\.number) == [1, 2, 2])
        #expect(lines[5].text == "new.txt")
        #expect(lines[7].number == 1)
    }

    /// A removed line that itself starts with "-- " must not be read as a file header.
    @Test func removedDashLineIsNotAHeader() {
        let lines = FileDiff.parseUnified("@@ -1,2 +1,1 @@\n--- a comment\n keep\n")
        #expect(lines.map(\.kind) == [.hunk, .removed, .context])
        #expect(lines[1].text == "-- a comment")
    }

    @Test func longLinesAreClipped() {
        let lines = FileDiff.fileLines(String(repeating: "x", count: FileDiff.maxLineLength + 50))
        #expect(lines[0].text.count == FileDiff.maxLineLength + 2)
    }

    // MARK: - Mock

    @Test func mockServesFilesAndErrors() throws {
        if case .text(let file) = try MockFiles.file("src/components/Hero.tsx") {
            #expect(file.language == "tsx")
        } else {
            Issue.record("Hero.tsx isn't text")
        }
        #expect(throws: RelayError.http(status: 403, code: "forbidden", message: "path is outside the agent's folder")) {
            try MockFiles.file("../secrets.txt")
        }
        #expect(throws: RelayError.self) { try MockFiles.file("nope.txt") }
    }
}
