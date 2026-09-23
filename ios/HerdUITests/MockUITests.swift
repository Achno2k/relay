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

    /// Saved to `HERD_SHOTS` when set (pass it as `TEST_RUNNER_HERD_SHOTS`).
    private func shot(_ name: String) {
        guard let dir = ProcessInfo.processInfo.environment["HERD_SHOTS"] else { return }
        sleep(1)
        let png = XCUIScreen.main.screenshot().pngRepresentation
        try? png.write(to: URL(fileURLWithPath: dir).appendingPathComponent("\(name).png"))
    }
}
