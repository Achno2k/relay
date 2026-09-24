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
        app.buttons["Codex"].tap()
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
