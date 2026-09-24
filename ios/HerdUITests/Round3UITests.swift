import XCTest

/// Per-agent controls against the mock: pi (long model list, no modes/compact/clear) and codex (short list).
@MainActor
final class Round3UITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() async throws {
        continueAfterFailure = false
        app = XCUIApplication()
    }

    private func launch(_ args: String...) {
        app.launchArguments = ["-uitest", "-mock", "-replay", "off", "-resetSidebar"] + args
        app.launch()
    }

    func testPiModelSearchAndEffort() throws {
        launch("-agent", "w2:p4")
        let subtitle = app.staticTexts["titleSubtitle"]
        XCTAssertTrue(subtitle.waitForExistence(timeout: 10))
        XCTAssertEqual(subtitle.label, "Claude Sonnet 4.5 · Medium")
        waitForIdle()

        app.buttons["titleMenu"].tap()
        XCTAssertFalse(menuRow("Mode").exists, "pi has no modes")
        XCTAssertTrue(app.buttons["Compact context"].exists, "pi has /compact")
        XCTAssertTrue(app.buttons["Clear conversation"].exists, "and /new")
        menuRow("Model").tap()
        XCTAssertTrue(app.buttons["Claude Sonnet 4.5"].waitForExistence(timeout: 3), "current model first")
        shot("round3-pi-model-menu")
        let all = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'All models'")).firstMatch
        XCTAssertTrue(all.exists, "long lists offer a search")
        all.tap()

        let search = app.searchFields["Search models"]
        XCTAssertTrue(search.waitForExistence(timeout: 5))
        search.tap()
        search.typeText("grok")
        let grok = app.buttons["model-xai/grok-4"]
        XCTAssertTrue(grok.waitForExistence(timeout: 3))
        XCTAssertFalse(app.buttons["model-openai/gpt-5.1"].exists, "search filters the list")
        shot("round3-pi-model-search")
        grok.tap()
        XCTAssertTrue(waitForLabel(subtitle, "Grok 4 · Medium"), "pi resets effort on a model switch")
        waitForIdle()

        app.buttons["titleMenu"].tap()
        menuRow("Effort").tap()
        XCTAssertTrue(app.buttons["Off"].waitForExistence(timeout: 3), "pi's own effort levels")
        app.buttons["Extra high"].tap()
        XCTAssertTrue(waitForLabel(subtitle, "Grok 4 · Extra high"))
    }

    func testCodexModelEffortAndMode() throws {
        launch("-agent", "w2:p3")
        let subtitle = app.staticTexts["titleSubtitle"]
        XCTAssertTrue(subtitle.waitForExistence(timeout: 10))
        XCTAssertEqual(subtitle.label, "GPT-5.6-Terra · Approve for me")
        XCTAssertFalse(app.buttons["modeChip"].exists, "mode lives in the title pill only")
        waitForIdle()

        app.buttons["titleMenu"].tap()
        XCTAssertTrue(app.buttons["Compact context"].waitForExistence(timeout: 3))
        XCTAssertTrue(app.buttons["Clear conversation"].exists)
        shot("round3-codex-menu")
        menuRow("Model").tap()
        XCTAssertFalse(app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'All models'")).firstMatch.exists, "short list")
        app.buttons["GPT-5.5"].tap()
        XCTAssertTrue(waitForLabel(subtitle, "GPT-5.5 · Approve for me"))
        waitForIdle()

        app.buttons["titleMenu"].tap()
        menuRow("Effort").tap()
        XCTAssertTrue(app.buttons["Ultra"].waitForExistence(timeout: 3), "codex's own effort levels")
        app.buttons["Max"].tap()
        waitForIdle()

        app.buttons["titleMenu"].tap()
        menuRow("Mode").tap()
        XCTAssertTrue(app.buttons["Full Access"].waitForExistence(timeout: 3), "codex's own modes")
        app.buttons["Ask for approval"].tap()
        XCTAssertTrue(waitForLabel(subtitle, "GPT-5.5 · Ask for approval"))
    }

    // MARK: - Helpers

    private func menuRow(_ title: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "label == %@ OR label BEGINSWITH %@", title, title + ",")).firstMatch
    }

    private func waitForIdle() {
        let idle = expectation(for: NSPredicate(format: "value == 'idle'"), evaluatedWith: app.buttons["titleMenu"])
        wait(for: [idle], timeout: 10)
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
