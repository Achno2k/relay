import Foundation
import Testing
@testable import HerdCore

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
            .init(label: "Type something.", keys: ["3"]),
        ])
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
