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

    /// A failed test can leave the agent at a question; cancel it so the next test starts clean.
    override func tearDown() async throws {
        guard let link = env["HERD_E2E_LINK"].flatMap(URLComponents.init(string:)),
              let base = link.queryItems?.first(where: { $0.name == "url" })?.value,
              let token = link.queryItems?.first(where: { $0.name == "token" })?.value,
              let id = env["HERD_E2E_AGENT"]?.replacingOccurrences(of: ":", with: "%3A"),
              let url = URL(string: "\(base)/agents/\(id)")
        else { return }
        var get = URLRequest(url: url.appendingPathComponent("approval"))
        get.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        guard let (_, response) = try? await URLSession.shared.data(for: get),
              (response as? HTTPURLResponse)?.statusCode == 200 else { return }
        var esc = URLRequest(url: url.appendingPathComponent("keys"))
        esc.httpMethod = "POST"
        esc.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        esc.setValue("application/json", forHTTPHeaderField: "Content-Type")
        esc.httpBody = Data(#"{"keys":["esc"]}"#.utf8)
        _ = try? await URLSession.shared.data(for: esc)
    }

    func testPromptApproveAndStop() throws {
        let isComposer = NSPredicate(format: "placeholderValue BEGINSWITH 'Message'")
        let composer = app.descendants(matching: .any).matching(isComposer).firstMatch
        XCTAssertTrue(composer.waitForExistence(timeout: 15), "composer never appeared")
        sleep(2)
        XCTAssertFalse(app.buttons["Scroll to bottom"].exists, "chat opened scrolled up")
        shot("1-opened")

        // Auto mode skips permission prompts, so block on a question instead: same sheet, same path.
        send("Use the AskUserQuestion tool to ask me: Tea or coffee? Options: Tea, Coffee. Then write my answer to answer.txt.", via: composer)
        let yes = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Tea'")).firstMatch
        XCTAssertTrue(yes.waitForExistence(timeout: 90), "approval sheet never appeared")
        assertKeyboardHidden()
        XCTAssertFalse(app.buttons["Scroll to bottom"].exists, "approval card pushed the chat off the latest message")
        shot("2-approval")
        yes.tap()

        // Back to idle: the send button returns once the turn ends.
        XCTAssertTrue(app.buttons["Send"].waitForExistence(timeout: 90), "turn never finished")
        sleep(2)
        shot("3-approved-done")

        // Stop mid-turn.
        // A long tool call, not an essay: Claude writes a text block to the transcript only once it's complete,
        // but the tool_use line lands before the command runs. That gives a reliable moment to stop.
        send("Run this exact Bash command and nothing else: sleep 45 && echo slept. Then reply with the single word: done.", via: composer)
        let stop = app.buttons["Stop"].firstMatch
        XCTAssertTrue(stop.waitForExistence(timeout: 30), "stop button never appeared")
        let running = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'Running Bash'")).firstMatch
        XCTAssertTrue(running.waitForExistence(timeout: 60), "the Bash call never showed up")
        sleep(2)
        shot("4-working")
        stop.tap()
        XCTAssertTrue(app.buttons["Send"].waitForExistence(timeout: 30), "stop didn't end the turn")
        // Claude's "[Request interrupted by user]" line renders as a marker, not a bubble.
        let marker = app.descendants(matching: .any)["stoppedMarker"]
        XCTAssertTrue(marker.waitForExistence(timeout: 15), "no Stopped marker after a stop")
        XCTAssertFalse(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH '[Request interrupted'")).firstMatch.exists)
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
        XCTAssertTrue(app.staticTexts["approvalStep"].label.hasPrefix("Question 1 of 3"), "no step header on question 1")
        shot("mq-1")
        tea.tap()

        let night = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Night'")).firstMatch
        XCTAssertTrue(night.waitForExistence(timeout: 15), "second question never appeared")
        XCTAssertTrue(app.staticTexts["approvalStep"].label.hasPrefix("Question 2 of 3"), "no step header on question 2")
        shot("mq-2")
        night.tap()

        let submit = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Submit'")).firstMatch
        if submit.waitForExistence(timeout: 8) {
            // count includes Claude's Submit tab; the app labels it as a review.
            XCTAssertEqual(app.staticTexts["approvalStep"].label, "Review answers")
            shot("mq-3-submit")
            submit.tap()
        }
        XCTAssertTrue(app.buttons["Send"].waitForExistence(timeout: 90), "turn never finished")
        sleep(2)
        shot("mq-4-done")
    }

    /// Claude's "Type something." row: the typed answer must reach the agent.
    func testFreeTextAnswer() throws {
        let isComposer = NSPredicate(format: "placeholderValue BEGINSWITH 'Message'")
        let composer = app.descendants(matching: .any).matching(isComposer).firstMatch
        XCTAssertTrue(composer.waitForExistence(timeout: 15), "composer never appeared")

        send("Use AskUserQuestion to ask me 'Favourite colour?' with options Red and Blue. Then reply with exactly: Colour is <my answer>. No tools after that.", via: composer)
        let typeSomething = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Type something'")).firstMatch
        XCTAssertTrue(typeSomething.waitForExistence(timeout: 90), "question never appeared")
        assertKeyboardHidden()
        shot("ft-1")
        typeSomething.tap()

        let field = app.textFields["approvalAnswer"].exists ? app.textFields["approvalAnswer"] : app.textViews["approvalAnswer"]
        XCTAssertTrue(field.waitForExistence(timeout: 5), "no answer field")
        field.typeText("Green")
        shot("ft-2")
        app.buttons["Send answer"].tap()

        let reply = app.staticTexts.matching(NSPredicate(format: "label CONTAINS 'Colour is Green'")).firstMatch
        XCTAssertTrue(reply.waitForExistence(timeout: 90), "typed answer never reached the agent")
        XCTAssertTrue(app.buttons["Send"].waitForExistence(timeout: 60), "turn never finished")
        shot("ft-3-done")
    }

    /// Model and mode from the title pill: Sonnet, then Plan, then back to Auto. The pill must follow the bridge.
    func testControls() throws {
        let subtitle = app.staticTexts["titleSubtitle"]
        XCTAssertTrue(subtitle.waitForExistence(timeout: 15), "no model/mode line under the title")
        let originalModel = subtitle.label.components(separatedBy: " · ").first ?? ""
        shot("ctl-1-start")

        pick("Model", "Sonnet 5")
        XCTAssertTrue(subtitle.label.hasPrefix("Sonnet 5"), "pill shows \(subtitle.label), not Sonnet")

        pick("Mode", "Plan")
        XCTAssertTrue(subtitle.label.hasSuffix("· Plan"), "pill shows \(subtitle.label), not Plan")
        XCTAssertTrue(app.buttons["modeChip"].exists, "no Plan chip")
        shot("ctl-2-sonnet-plan")

        pick("Mode", "Auto")
        XCTAssertTrue(subtitle.label.hasSuffix("· Auto"), "pill shows \(subtitle.label), not Auto")
        XCTAssertTrue(subtitle.label.hasPrefix("Sonnet 5"))
        shot("ctl-3-auto")

        if !originalModel.isEmpty, originalModel != "Sonnet 5" {
            pick("Model", originalModel)
            XCTAssertTrue(subtitle.label.hasPrefix(originalModel))
        }
    }

    /// Opens the title menu, a submenu, then the choice, and waits for the bridge to confirm.
    private func pick(_ submenu: String, _ choice: String) {
        app.buttons["titleMenu"].tap()
        let row = app.buttons.matching(NSPredicate(format: "label == %@ OR label BEGINSWITH %@", submenu, submenu + ",")).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 5), "no \(submenu) row")
        row.tap()
        let item = app.buttons.matching(NSPredicate(format: "label == %@", choice)).firstMatch
        XCTAssertTrue(item.waitForExistence(timeout: 5), "no \(choice) in \(submenu)")
        item.tap()
        let idle = expectation(for: NSPredicate(format: "value == 'idle'"), evaluatedWith: app.buttons["titleMenu"])
        wait(for: [idle], timeout: 30)
        let toast = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH \"Couldn't\"")).firstMatch
        XCTAssertFalse(toast.exists, "bridge refused \(choice): \(toast.exists ? toast.label : "")")
    }

    private func assertKeyboardHidden() {
        let gone = expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: app.keyboards.firstMatch)
        wait(for: [gone], timeout: 5)
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
