import XCTest

/// Runs against the bundled mock backend, so it needs no bridge.
@MainActor
final class MockUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() async throws {
        continueAfterFailure = false
        app = XCUIApplication()
    }

    private func launch(_ args: String...) {
        app.launchArguments = ["-mock", "-replay", "off"] + args
        app.launch()
    }

    private var composer: XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "placeholderValue BEGINSWITH 'Message'")).firstMatch
    }

    private var scrollToBottom: XCUIElement { app.buttons["Scroll to bottom"] }

    /// A long chat, fetched slowly, opens on its latest message; so does switching to it from the sidebar.
    func testLongChatOpensAtBottom() throws {
        launch("-agent", "w2:p1", "-latency", "700")
        XCTAssertTrue(app.staticTexts["Done"].waitForExistence(timeout: 10), "latest message not visible")
        XCTAssertFalse(scrollToBottom.exists, "opened scrolled up")

        app.buttons["Open sidebar"].tap()
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Refactor auth middleware'")).firstMatch.tap()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS 'Option 2'")).firstMatch.waitForExistence(timeout: 10))

        app.buttons["Open sidebar"].tap()
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Landing page hero redesign'")).firstMatch.tap()
        XCTAssertTrue(app.staticTexts["Done"].waitForExistence(timeout: 10), "latest message not visible after switching")
        sleep(1)
        XCTAssertFalse(scrollToBottom.exists, "switched chat opened scrolled up")
    }

    /// The composer's keyboard must not cover the options.
    func testApprovalSheetDismissesKeyboard() throws {
        launch("-agent", "w1:p2", "-demo", "card")
        XCTAssertTrue(composer.waitForExistence(timeout: 10))
        composer.tap()
        composer.typeText("hold on")
        XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 5))

        app.buttons["approvalCard"].tap()
        let yes = app.buttons["Yes"]
        XCTAssertTrue(yes.waitForExistence(timeout: 5))
        let keyboardGone = expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: app.keyboards.firstMatch)
        wait(for: [keyboardGone], timeout: 5)
        XCTAssertTrue(yes.isHittable)
        shot("approval-sheet")
        XCTAssertEqual(app.staticTexts["approvalStep"].label, "Question 1 of 2 · Tests")
    }

    /// "Type something." shows a field; the answer goes to the agent after the option's keys.
    func testFreeTextAnswer() throws {
        launch("-agent", "w1:p2")
        let typeSomething = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Type something.'")).firstMatch
        XCTAssertTrue(typeSomething.waitForExistence(timeout: 10), "sheet never appeared")
        typeSomething.tap()

        let field = app.textFields["approvalAnswer"].exists ? app.textFields["approvalAnswer"] : app.textViews["approvalAnswer"]
        XCTAssertTrue(field.waitForExistence(timeout: 5))
        field.typeText("Use a fresh cart per test")
        shot("approval-free-text")
        app.buttons["Send answer"].tap()

        let reply = app.staticTexts.matching(NSPredicate(format: "label CONTAINS 'Got it: Use a fresh cart per test'")).firstMatch
        XCTAssertTrue(reply.waitForExistence(timeout: 10), "answer never reached the agent")
    }

    /// Model, mode (with the composer chip), a refused mode, and /clear, all from the title pill.
    func testControls() throws {
        launch("-agent", "w2:p1")
        let subtitle = app.staticTexts["titleSubtitle"]
        XCTAssertTrue(subtitle.waitForExistence(timeout: 10))
        XCTAssertEqual(subtitle.label, "Opus 5.5 · Default")
        XCTAssertFalse(app.buttons["modeChip"].exists, "no chip in default mode")

        app.buttons["titleMenu"].tap()
        shot("controls-menu")
        menuItem("Model").tap()
        shot("controls-model")
        menuItem("Sonnet 5").tap()
        XCTAssertTrue(waitForLabel(subtitle, "Sonnet 5 · Default"), "pill didn't follow the model")
        shot("controls-switching")
        waitForIdle()

        app.buttons["titleMenu"].tap()
        menuItem("Mode").tap()
        menuItem("Plan").tap()
        XCTAssertTrue(waitForLabel(subtitle, "Sonnet 5 · Plan"), "pill didn't follow the mode")
        waitForIdle()
        let chip = app.buttons["modeChip"]
        XCTAssertTrue(chip.waitForExistence(timeout: 5), "no Plan chip")
        shot("controls-plan-chip")

        // A mode outside Claude's cycle is refused; the pill reverts and a toast explains.
        chip.tap()
        shot("controls-chip-menu")
        menuItem("Bypass permissions").tap()
        let toast = app.staticTexts.matching(NSPredicate(format: "label CONTAINS 'Couldn\u{2019}t switch' OR label CONTAINS \"Couldn't switch\"")).firstMatch
        XCTAssertTrue(toast.waitForExistence(timeout: 5), "no error toast")
        shot("controls-error")
        XCTAssertTrue(waitForLabel(subtitle, "Sonnet 5 · Plan"), "pill didn't revert")
        waitForIdle()

        chip.tap()
        menuItem("Default").tap()
        XCTAssertTrue(waitForLabel(subtitle, "Sonnet 5 · Default"))
        waitForIdle()
        XCTAssertFalse(chip.waitForExistence(timeout: 2) && chip.isHittable, "chip should go away in default mode")

        // Clear asks first, then the chat starts over.
        app.buttons["titleMenu"].tap()
        menuItem("Clear conversation").tap()
        let confirm = app.buttons.matching(NSPredicate(format: "label == 'Clear conversation'")).element(boundBy: 0)
        XCTAssertTrue(confirm.waitForExistence(timeout: 5))
        shot("controls-clear-confirm")
        confirm.tap()
        let gone = expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: app.staticTexts["Done"])
        wait(for: [gone], timeout: 10)
    }

    /// While the agent works, the controls are disabled with a caption.
    func testControlsDisabledWhileWorking() throws {
        launch("-agent", "w1:p1")
        let subtitle = app.staticTexts["titleSubtitle"]
        XCTAssertTrue(subtitle.waitForExistence(timeout: 10))
        XCTAssertEqual(subtitle.label, "Opus 5.5 · Auto")
        XCTAssertTrue(app.buttons["modeChip"].exists, "Auto isn't default, so the chip shows")
        app.buttons["titleMenu"].tap()
        XCTAssertTrue(app.staticTexts["Agent is working"].waitForExistence(timeout: 5))
        shot("controls-busy")
        // XCUITest reports disabled menu pickers as enabled; check the submenu doesn't open instead.
        menuItem("Model").tap()
        XCTAssertFalse(app.buttons["Sonnet 5"].waitForExistence(timeout: 2), "Model opened while working")
    }

    private func waitForIdle() {
        let idle = expectation(for: NSPredicate(format: "value == 'idle'"), evaluatedWith: app.buttons["titleMenu"])
        wait(for: [idle], timeout: 10)
    }

    /// A menu row by title; submenu rows read "Title, value".
    private func menuItem(_ prefix: String) -> XCUIElement {
        let item = app.buttons.matching(NSPredicate(format: "label == %@ OR label BEGINSWITH %@", prefix, prefix + ",")).firstMatch
        XCTAssertTrue(item.waitForExistence(timeout: 5), "no menu item \(prefix)")
        return item
    }

    private func waitForLabel(_ element: XCUIElement, _ label: String, timeout: TimeInterval = 8) -> Bool {
        let match = expectation(for: NSPredicate(format: "label == %@", label), evaluatedWith: element)
        return XCTWaiter().wait(for: [match], timeout: timeout) == .completed
    }

    /// Saved to `HERD_SHOTS` when set (pass it as `TEST_RUNNER_HERD_SHOTS`).
    private func shot(_ name: String) {
        guard let dir = ProcessInfo.processInfo.environment["HERD_SHOTS"] else { return }
        sleep(1)
        let png = XCUIScreen.main.screenshot().pngRepresentation
        try? png.write(to: URL(fileURLWithPath: dir).appendingPathComponent("\(name).png"))
    }
}
