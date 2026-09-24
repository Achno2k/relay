import XCTest

/// New agents open on a clean empty state; screen-read agents show a Live screen card, never a raw fence.
@MainActor
final class EmptyStateUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() async throws {
        continueAfterFailure = false
        app = XCUIApplication()
    }

    private func launch(_ args: String...) {
        app.launchArguments = ["-uitest", "-mock", "-replay", "off", "-resetSidebar"] + args
        app.launch()
    }

    func testNewAgentWithoutMessageShowsEmptyState() throws {
        launch("-agent", "w1:p1")
        // The toolbar's button; the hidden sidebar has a "New chat" capsule too.
        let newChat = app.navigationBars.buttons["New chat"].firstMatch
        XCTAssertTrue(newChat.waitForExistence(timeout: 10))
        newChat.tap()
        // The sheet's row reads "website, 2 agents"; the hidden sidebar has a plain "website" row too.
        let website = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'website,'")).firstMatch
        XCTAssertTrue(website.waitForExistence(timeout: 5))
        website.tap()
        app.buttons["Create"].tap()

        let empty = app.descendants(matching: .any)["emptyAgentChat"]
        XCTAssertTrue(empty.waitForExistence(timeout: 10), "no empty state for a new agent")
        XCTAssertTrue(empty.label.contains("New Claude chat in website"), "empty state reads \(empty.label)")
        XCTAssertTrue(empty.label.contains("Opus 5.5"), "no model line")
        XCTAssertFalse(app.staticTexts.matching(NSPredicate(format: "label CONTAINS '```'")).firstMatch.exists)
        XCTAssertFalse(app.descendants(matching: .any)["liveScreen"].exists)
        shot("empty-new-agent")

        // The first message replaces it.
        let composer = app.descendants(matching: .any).matching(NSPredicate(format: "placeholderValue BEGINSWITH 'Message'")).firstMatch
        composer.tap()
        composer.typeText("Hello there")
        app.buttons["Send"].tap()
        let gone = expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: empty)
        wait(for: [gone], timeout: 5)
        XCTAssertTrue(app.descendants(matching: .any).matching(NSPredicate(format: "identifier == 'userBubble' AND label CONTAINS 'Hello there'")).firstMatch.exists)
    }

    func testUnsupportedAgentShowsLiveScreenCard() throws {
        launch("-agent", "w2:p5")
        let card = app.descendants(matching: .any)["liveScreen"]
        XCTAssertTrue(card.waitForExistence(timeout: 10), "no Live screen card")
        XCTAssertTrue(card.staticTexts["Live screen"].exists)
        let screen = card.staticTexts.matching(NSPredicate(format: "label CONTAINS 'Updated package.json'")).firstMatch
        XCTAssertTrue(screen.exists, "screen text not readable in the card")
        XCTAssertFalse(screen.label.contains("```"), "the fence leaked into the card")
        XCTAssertFalse(app.descendants(matching: .any)["emptyAgentChat"].exists)
        shot("live-screen-card")
    }

    /// The fixture codex agent is `pending`: an empty state, not a screen dump.
    func testPendingCodexShowsEmptyState() throws {
        launch("-agent", "w2:p3")
        let empty = app.descendants(matching: .any)["emptyAgentChat"]
        XCTAssertTrue(empty.waitForExistence(timeout: 10))
        XCTAssertTrue(empty.label.contains("New Codex chat in website"), "empty state reads \(empty.label)")
        XCTAssertFalse(app.descendants(matching: .any)["liveScreen"].exists)
    }

    private func shot(_ name: String) {
        guard let dir = ProcessInfo.processInfo.environment["RELAY_SHOTS"] else { return }
        sleep(1)
        try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("\(name).png"))
    }
}
