import XCTest

/// New chat: folder, kind, then that kind's Model and Effort. Create opens the empty chat, ready to type.
@MainActor
final class NewChatUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() async throws {
        continueAfterFailure = false
        app = XCUIApplication()
        app.launchArguments = ["-uitest", "-mock", "-replay", "off", "-resetSidebar", "-agent", "w1:p1"]
        app.launch()
    }

    private func openSheet() {
        let newChat = app.navigationBars.buttons["New chat"].firstMatch
        XCTAssertTrue(newChat.waitForExistence(timeout: 10))
        newChat.tap()
        XCTAssertTrue(app.navigationBars["New chat"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.textFields["What should it work on?"].exists, "no first-message field any more")
        XCTAssertFalse(app.textViews["What should it work on?"].exists)
    }

    func testCodexWithModelAndEffort() throws {
        openSheet()
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'website,'")).firstMatch.tap()
        // Not the sidebar's "Codex" chat row behind the sheet.
        app.buttons.matching(NSPredicate(format: "label == 'Codex' AND NOT (identifier BEGINSWITH 'session-')")).firstMatch.tap()
        let model = app.buttons["newChatModel"]
        XCTAssertTrue(model.waitForExistence(timeout: 5))
        XCTAssertTrue(model.label.contains("Default (GPT-5.6-Terra)"), "model reads \(model.label)")
        shot("newchat-codex")
        model.tap()
        app.buttons["GPT-6-Luna"].tap()

        let effort = app.buttons["newChatEffort"]
        effort.tap()
        XCTAssertTrue(app.buttons["Extra high"].waitForExistence(timeout: 3))
        XCTAssertFalse(app.buttons["Ultra"].exists, "GPT-6-Luna doesn't offer Ultra")
        shot("newchat-codex-efforts")
        app.buttons["Extra high"].tap()
        app.buttons["Create"].tap()

        let empty = app.descendants(matching: .any)["emptyAgentChat"]
        XCTAssertTrue(empty.waitForExistence(timeout: 10))
        XCTAssertTrue(empty.label.contains("New Codex chat in website"), "empty state reads \(empty.label)")
        XCTAssertTrue(empty.label.contains("GPT-6-Luna"), "the chosen model isn't shown")
        XCTAssertTrue(app.keyboards.firstMatch.waitForExistence(timeout: 5), "not ready to type")
        shot("newchat-created")
    }

    func testPiModelListAndEffortsFollowTheModel() throws {
        openSheet()
        app.buttons["Pi"].tap()
        let model = app.buttons["newChatModel"]
        XCTAssertTrue(model.waitForExistence(timeout: 5))
        model.tap()
        // A long list opens as its own screen.
        let opus = app.buttons["Claude Opus 4.1"]
        XCTAssertTrue(opus.waitForExistence(timeout: 5))
        opus.tap()
        let effort = app.buttons["newChatEffort"]
        XCTAssertTrue(effort.waitForExistence(timeout: 5))
        effort.tap()
        XCTAssertTrue(app.buttons["Max"].waitForExistence(timeout: 3))
        XCTAssertFalse(app.buttons["Off"].exists, "Claude models on pi can't turn thinking off")
    }

    private func shot(_ name: String) {
        guard let dir = ProcessInfo.processInfo.environment["RELAY_SHOTS"] else { return }
        sleep(1)
        try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("\(name).png"))
    }
}

/// Agent kinds that can't start on the machine (`GET /kinds`): greyed out with the bridge's hint, never picked.
/// A `409` from Create (the bridge knew better) shows as an alert. Mock only; runs headless.
@MainActor
final class NewChatKindsUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() async throws {
        continueAfterFailure = false
    }

    private func launch(_ extra: [String]) {
        app = XCUIApplication()
        app.launchArguments = ["-uitest", "-mock", "-replay", "off", "-resetSidebar", "-agent", "w1:p1"] + extra
        app.launch()
        let newChat = app.navigationBars.buttons["New chat"].firstMatch
        XCTAssertTrue(newChat.waitForExistence(timeout: 10))
        newChat.tap()
        XCTAssertTrue(app.navigationBars["New chat"].waitForExistence(timeout: 5))
    }

    private func row(_ kind: String) -> XCUIElement { app.buttons["newChatKind-\(kind)"] }

    func testSignedOutAndMissingKindsAreDisabledWithTheirHints() throws {
        launch(["-mockSignedOut", "codex", "-mockNotInstalled", "pi"])
        let codex = row("codex")
        XCTAssertTrue(codex.waitForExistence(timeout: 5))
        let hinted = NSPredicate(format: "isEnabled == false AND label CONTAINS 'codex login'")
        expectation(for: hinted, evaluatedWith: codex)
        waitForExpectations(timeout: 5)
        XCTAssertFalse(row("pi").isEnabled)
        XCTAssertTrue(row("pi").label.contains("isn't installed on this machine"), "pi reads \(row("pi").label)")
        XCTAssertTrue(row("claude").isEnabled)
        XCTAssertTrue(row("claude").isSelected, "Claude stays picked")
        shot("newchat-kinds-disabled")

        codex.tap()
        XCTAssertFalse(codex.isSelected, "a signed-out kind can't be picked")
        XCTAssertTrue(row("claude").isSelected)

        app.buttons["Create"].tap()
        let empty = app.descendants(matching: .any)["emptyAgentChat"]
        XCTAssertTrue(empty.waitForExistence(timeout: 10))
        XCTAssertTrue(empty.label.contains("New Claude chat"), "empty state reads \(empty.label)")
    }

    func testOnlySignedInKindIsPickedForYou() throws {
        launch(["-mockSignedOut", "claude,pi"])
        let codex = row("codex")
        XCTAssertTrue(codex.waitForExistence(timeout: 5))
        expectation(for: NSPredicate(format: "isSelected == true"), evaluatedWith: codex)
        waitForExpectations(timeout: 5)
        XCTAssertFalse(row("claude").isEnabled)
        XCTAssertTrue(row("claude").label.contains("claude auth login"), "claude reads \(row("claude").label)")
        XCTAssertTrue(app.buttons["newChatModel"].waitForExistence(timeout: 5), "codex's models load")
        XCTAssertTrue(app.buttons["Create"].isEnabled)
    }

    func testRefusedCreateShowsTheHintAsAnAlert() throws {
        launch(["-mockSignedOut", "codex", "-mockKindsStale"])
        let codex = row("codex")
        XCTAssertTrue(codex.waitForExistence(timeout: 5))
        sleep(1) // let the (stale) /kinds answer land first
        XCTAssertTrue(codex.isEnabled, "/kinds says it can start")
        codex.tap()
        XCTAssertTrue(app.buttons["newChatModel"].waitForExistence(timeout: 5))
        app.buttons["Create"].tap()

        let alert = app.alerts["Can't start Codex"]
        XCTAssertTrue(alert.waitForExistence(timeout: 5))
        XCTAssertTrue(alert.staticTexts.containing(NSPredicate(format: "label CONTAINS 'codex login'")).firstMatch.exists,
                      "the bridge's hint, not a generic error")
        shot("newchat-kinds-alert")
        alert.buttons["OK"].tap()
        XCTAssertTrue(app.navigationBars["New chat"].exists, "the sheet stays open")
        XCTAssertFalse(app.descendants(matching: .any)["emptyAgentChat"].exists, "no chat was started")
    }

    private func shot(_ name: String) {
        guard let dir = ProcessInfo.processInfo.environment["RELAY_SHOTS"] else { return }
        sleep(1)
        try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("\(name).png"))
    }
}

