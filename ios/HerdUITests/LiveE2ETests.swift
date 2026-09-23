import XCTest

/// Drives the real app against a live bridge and a real, throwaway agent.
/// Skipped unless the runner passes `TEST_RUNNER_HERD_E2E_LINK` (a herd://pair link) and
/// `TEST_RUNNER_HERD_E2E_AGENT` (the agent's pane id). `TEST_RUNNER_HERD_E2E_SHOTS` is an optional
/// directory for screenshots.
@MainActor
final class LiveE2ETests: XCTestCase {
    private var env: [String: String] { ProcessInfo.processInfo.environment }
    private var app: XCUIApplication!

    override func setUp() async throws {
        continueAfterFailure = false
        guard let link = env["HERD_E2E_LINK"], let agent = env["HERD_E2E_AGENT"] else {
            throw XCTSkip("HERD_E2E_LINK / HERD_E2E_AGENT not set")
        }
        app = XCUIApplication()
        app.launchArguments = ["-pair", link, "-agent", agent]
        app.launch()
    }

    func testPromptApproveAndStop() throws {
        let isComposer = NSPredicate(format: "placeholderValue BEGINSWITH 'Message'")
        let composer = app.descendants(matching: .any).matching(isComposer).firstMatch
        XCTAssertTrue(composer.waitForExistence(timeout: 15), "composer never appeared")
        shot("1-opened")

        // Auto mode skips permission prompts, so block on a question instead: same sheet, same path.
        send("Use the AskUserQuestion tool to ask me: Tea or coffee? Options: Tea, Coffee. Then write my answer to answer.txt.", via: composer)
        let yes = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Tea'")).firstMatch
        XCTAssertTrue(yes.waitForExistence(timeout: 90), "approval sheet never appeared")
        shot("2-approval")
        yes.tap()

        // Back to idle: the send button returns once the turn ends.
        XCTAssertTrue(app.buttons["Send"].waitForExistence(timeout: 90), "turn never finished")
        sleep(2)
        shot("3-approved-done")

        // Stop mid-turn.
        send("Write a 1500 word essay about the history of terminals. Do not use any tools.", via: composer)
        let stop = app.buttons["Stop"].firstMatch
        XCTAssertTrue(stop.waitForExistence(timeout: 30), "stop button never appeared")
        sleep(3)
        shot("4-working")
        stop.tap()
        XCTAssertTrue(app.buttons["Send"].waitForExistence(timeout: 30), "stop didn't end the turn")
        sleep(2)
        shot("5-stopped")
    }

    /// One AskUserQuestion with two questions: the sheet must come back for the second one
    /// (and for Claude's final "Submit answers" review step) while the agent stays blocked.
    func testMultipleQuestions() throws {
        let isComposer = NSPredicate(format: "placeholderValue BEGINSWITH 'Message'")
        let composer = app.descendants(matching: .any).matching(isComposer).firstMatch
        XCTAssertTrue(composer.waitForExistence(timeout: 15), "composer never appeared")

        send("Use AskUserQuestion once with two questions: Q1 'Tea or coffee?' (Tea, Coffee) and Q2 'Morning or night?' (Morning, Night). Then write both answers to answers.txt.", via: composer)
        let tea = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Tea'")).firstMatch
        XCTAssertTrue(tea.waitForExistence(timeout: 90), "first question never appeared")
        shot("mq-1")
        tea.tap()

        let night = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Night'")).firstMatch
        XCTAssertTrue(night.waitForExistence(timeout: 15), "second question never appeared")
        shot("mq-2")
        night.tap()

        let submit = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Submit'")).firstMatch
        if submit.waitForExistence(timeout: 8) {
            shot("mq-3-submit")
            submit.tap()
        }
        XCTAssertTrue(app.buttons["Send"].waitForExistence(timeout: 90), "turn never finished")
        sleep(2)
        shot("mq-4-done")
    }

    private func send(_ text: String, via composer: XCUIElement) {
        composer.tap()
        composer.typeText(text)
        app.buttons["Send"].tap()
    }

    private func shot(_ name: String) {
        let png = XCUIScreen.main.screenshot().pngRepresentation
        let attachment = XCTAttachment(data: png, uniformTypeIdentifier: "public.png")
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
        if let dir = env["HERD_E2E_SHOTS"] {
            try? png.write(to: URL(fileURLWithPath: dir).appendingPathComponent("\(name).png"))
        }
    }
}
