import Foundation
import Testing
@testable import RelayCore

@Suite struct ApprovalParserTests {
    @Test func editPrompt() throws {
        let a = ApprovalParser.parse(try Fixture.text("approval-edit.txt"), agentId: "w1:p2")
        #expect(a == Approval(agentId: "w1:p2", question: "Do you want to make this edit to api.md?", options: [
            .init(label: "Yes", keys: ["1"]),
            .init(label: "Yes, allow all edits during this session", keys: ["2"]),
            .init(label: "No, and tell Claude what to do differently", keys: ["esc"]),
        ]))
    }

    @Test func boxedBashPromptScrubsPaths() throws {
        let a = ApprovalParser.parse(try Fixture.text("approval-bash-boxed.txt"), agentId: "w1:p2",
                                     scrubber: PathScrubber(cwd: "/Users/dev/shop-api"))
        #expect(a?.question == "Do you want to proceed?")
        #expect(a?.options.map(\.label) == ["Yes", "Yes, and don't ask again for npm test commands in .", "No, tell Claude what to do differently"])
        #expect(a?.options.map(\.keys) == [["1"], ["2"], ["esc"]])
    }

    @Test func askUserQuestionIgnoresEarlierListAndDescriptions() throws {
        let a = ApprovalParser.parse(try Fixture.text("approval-question.txt"), agentId: "w2:p1")
        #expect(a?.question == "Which invalidation strategy should I use?")
        #expect(a?.options == [
            .init(label: "SIGHUP", keys: ["1"]),
            .init(label: "mtime", keys: ["2"]),
            .init(label: "Type something.", keys: ["down", "down"], freeText: true),
        ])
    }

    @Test func freeTextKeysAreRelativeToCursor() throws {
        let a = try #require(ApprovalParser.parse(try Fixture.text("approval-multi.txt"), agentId: "x"))
        #expect(a.question == "When do you focus best?")
        #expect(a.options == [
            .init(label: "Morning", keys: ["1"]),
            .init(label: "Night", keys: ["2"]),
            .init(label: "Type something.", keys: ["down"], freeText: true),
            .init(label: "Chat about this", keys: ["4"]),
        ])
        // Cursor already on the row: a no-op move still gives the app something to send.
        let on = ApprovalParser.parse("Pick?\n  1. A\n❯ 2. Type something.", agentId: "x")
        #expect(on?.options.last == .init(label: "Type something.", keys: ["up", "down"], freeText: true))
        // Without a visible cursor, fall back to the number.
        let none = ApprovalParser.parse("Pick?\n  1. A\n  2. Type something.", agentId: "x")
        #expect(none?.options.last == .init(label: "Type something.", keys: ["2"], freeText: true))
    }

    @Test func codexCommandApproval() throws {
        let a = try #require(ApprovalParser.parse(try Fixture.text("codex-approval.txt"), agentId: "w14:p5", kind: "codex"))
        #expect(a.question == "Would you like to run the following command?\n`curl -sI https://example.com | head -1`")
        #expect(a.options == [
            .init(label: "Yes, proceed", keys: ["enter"]),
            .init(label: "Yes, and don't ask again for commands that start with `curl -sI https://example.com`", keys: ["down", "enter"]),
            .init(label: "No, and tell Codex what to do differently", keys: ["esc"]),
        ])
    }

    @Test func codexEditApprovalWithCursorOnSecondRow() throws {
        let a = try #require(ApprovalParser.parse(try Fixture.text("codex-approval-edit.txt"), agentId: "w14:p5",
                                                  scrubber: PathScrubber(cwd: "/Users/dev/project"), kind: "codex"))
        #expect(a.question == "Would you like to make the following edits?")
        #expect(a.options.map(\.keys) == [["up", "enter"], ["enter"], ["esc"]])
        #expect(a.options[1].label == "Yes, and don't ask again for these files")
    }

    @Test func claudeParsingUnchangedWithoutKind() throws {
        // The same codex screen parsed the Claude way keeps digit keys (regression guard for other kinds).
        let a = try #require(ApprovalParser.parse(try Fixture.text("codex-approval.txt"), agentId: "x"))
        #expect(a.options.first?.keys == ["1"])
    }

    @Test func trustFolderCursorMenu() throws {
        let a = ApprovalParser.parse(try Fixture.text("approval-trust.txt"), agentId: "w15:p2",
                                     scrubber: PathScrubber(cwd: "/Users/dev/fresh-project"), cwdName: "fresh-project")
        #expect(a == Approval(agentId: "w15:p2", question: "Trust this folder? fresh-project", options: [
            .init(label: "No, exit", keys: ["enter"]),
            .init(label: "Yes, I trust this folder", keys: ["down", "enter"]),
        ]))
    }

    @Test func genericCursorMenuMovesUp() throws {
        let a = ApprovalParser.parse(try Fixture.text("approval-cursor-generic.txt"), agentId: "x")
        #expect(a?.question == "Resume a previous session?")
        #expect(a?.options == [
            .init(label: "Start fresh", keys: ["up", "up", "enter"]),
            .init(label: "Resume \"fix flaky tests\"", keys: ["up", "enter"]),
            .init(label: "Resume \"landing hero\"", keys: ["enter"]),
        ])
    }

    @Test func inputBoxIsNotAMenu() throws {
        #expect(ApprovalParser.parse(try Fixture.text("input-restored.txt"), agentId: "x") == nil)
        #expect(ApprovalParser.parse(try Fixture.text("input-empty.txt"), agentId: "x") == nil)
    }

    @Test func stepFromHighlightedTab() throws {
        let screen = try Fixture.text("approval-multi.txt")
        #expect(ApprovalParser.step(screen, ansi: try Fixture.text("approval-multi.ansi")) == ApprovalStep(index: 2, count: 3, title: "Focus"))
        // Without colours: the first unanswered tab.
        #expect(ApprovalParser.step(screen) == ApprovalStep(index: 2, count: 3, title: "Focus"))
        let submit = "←  ☒ Delivery  ☒ Focus  ✔ Submit  →\nReady to submit your answers?"
        #expect(ApprovalParser.step(submit) == ApprovalStep(index: 3, count: 3, title: "Submit"))
        #expect(ApprovalParser.step(try Fixture.text("approval-edit.txt")) == nil)
    }

    @Test func tabBarIsNeverTheQuestion() {
        let a = ApprovalParser.parse("←  ☐ A  ✔ Submit  →\n\n❯ 1. Yes\n  2. No", agentId: "x")
        #expect(a?.question == "Waiting for your input")
    }

    @Test func encodesOptionalFieldsOnlyWhenSet() throws {
        let enc = JSONEncoder()
        enc.outputFormatting = .sortedKeys
        let plain = String(decoding: try enc.encode(Approval(agentId: "a", question: "q", options: [.init(label: "Yes", keys: ["1"])])), as: UTF8.self)
        #expect(plain == #"{"agentId":"a","options":[{"keys":["1"],"label":"Yes"}],"question":"q"}"#)
        let full = Approval(agentId: "a", question: "q", options: [.init(label: "Type something.", keys: ["down"], freeText: true)],
                            step: .init(index: 2, count: 3, title: "Focus"))
        #expect(String(decoding: try enc.encode(full), as: UTF8.self)
            == #"{"agentId":"a","options":[{"freeText":true,"keys":["down"],"label":"Type something."}],"question":"q","step":{"count":3,"index":2,"title":"Focus"}}"#)
    }

    @Test func noOptions() throws {
        #expect(ApprovalParser.parse(try Fixture.text("approval-none.txt"), agentId: "x") == nil)
        #expect(ApprovalParser.parse("", agentId: "x") == nil)
    }

    @Test func brokenSequenceIsNotAnApproval() {
        #expect(ApprovalParser.parse("Pick one?\n 2. B\n 3. C", agentId: "x") == nil)
    }

    @Test func trailingNoWithoutEscHintKeepsNumber() {
        let a = ApprovalParser.parse("Continue?\n❯ 1. Yes\n  2. No", agentId: "x")
        #expect(a?.options.map(\.keys) == [["1"], ["2"]])
    }

    @Test func fallbackUsesLastLine() {
        let a = ApprovalParser.fallback("some output\nAllow network access?\n", agentId: "x")
        #expect(a.question == "Allow network access?")
        #expect(a.options.isEmpty)
    }

    @Test func decodesContractFixture() throws {
        let url = URL(fileURLWithPath: #filePath)
            .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
            .appendingPathComponent("docs/fixtures/approval.json")
        let a = try JSONDecoder().decode(Approval.self, from: Data(contentsOf: url))
        #expect(a.options.last?.keys == ["esc"])
    }
}
