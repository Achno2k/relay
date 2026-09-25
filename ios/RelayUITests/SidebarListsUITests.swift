import XCTest

/// Round 7, design B against the mock: Now with Show all, completed folds, the all-in-Now project row,
/// needs-input and ready-for-review rows, archive from a card, new chat per project.
/// Mock (MockSidebar): 7 running, website has 2 completed, analytics is all running, shop-api has a blocked chat.
@MainActor
final class SidebarListsUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() async throws {
        continueAfterFailure = false
        app = XCUIApplication()
    }

    private func launch(reset: Bool = true, _ extra: String...) {
        app.launchArguments = ["-uitest", "-mock", "-replay", "off", "-agent", "w2:p1"] + (reset ? ["-resetSidebar"] : []) + extra
        app.launch()
        let open = app.buttons["Open sidebar"]
        XCTAssertTrue(open.waitForExistence(timeout: 10))
        open.tap()
        XCTAssertTrue(app.descendants(matching: .any)["nowHeader"].waitForExistence(timeout: 5), "Now card never appeared")
    }

    func testNowShowsFourThenAll() throws {
        launch()
        let showAll = app.buttons["nowShowAll"]
        XCTAssertTrue(showAll.waitForExistence(timeout: 5))
        XCTAssertEqual(showAll.label, "Show all 7 running")
        XCTAssertEqual(showAll.value as? String, "collapsed")
        XCTAssertTrue(row("Weekly report export").exists, "newest running chat is in Now")
        XCTAssertTrue((row("Weekly report export").value as? String ?? "").hasPrefix("Working, analytics"))
        XCTAssertFalse(row("Retry failed crawls").exists, "the 5th running chat waits for Show all")
        shot("round7-now-collapsed")

        showAll.tap()
        XCTAssertTrue(row("Retry failed crawls").waitForExistence(timeout: 3))
        XCTAssertEqual(showAll.value as? String, "expanded")
        shot("round7-now-expanded")

        // Remembered across launches.
        app.terminate()
        launch(reset: false)
        XCTAssertEqual(app.buttons["nowShowAll"].value as? String, "expanded")
        XCTAssertTrue(row("Retry failed crawls").exists)
        app.buttons["nowShowAll"].tap()
        let gone = expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: row("Retry failed crawls"))
        wait(for: [gone], timeout: 5)
    }

    func testCompletedFoldExpands() throws {
        launch()
        let completed = app.buttons["completed-website"]
        scroll(to: completed)
        XCTAssertEqual(completed.label, "2 completed")
        XCTAssertEqual(completed.value as? String, "collapsed")
        XCTAssertFalse(row("Update footer links").exists, "seen, finished chats are folded")

        completed.tap()
        XCTAssertTrue(row("Update footer links").waitForExistence(timeout: 3))
        XCTAssertTrue(row("Add a sitemap route").exists)
        XCTAssertEqual(completed.value as? String, "expanded")
        shot("round7-completed-expanded")
        completed.tap()
        let gone = expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: row("Update footer links"))
        wait(for: [gone], timeout: 5)
    }

    func testProjectAllInNowIsOneRow() throws {
        launch()
        let analytics = app.buttons["project-analytics"]
        scroll(to: analytics)
        XCTAssertEqual(analytics.value as? String, "2 running, in Now")
        // Its older chat is 5th in Now, so behind Show all; folded, the project card doesn't list it either.
        XCTAssertFalse(row("Retry failed crawls").exists)
        shot("round7-all-in-now")

        analytics.tap()
        XCTAssertTrue(app.buttons["newChat-analytics"].waitForExistence(timeout: 3), "opened into a full section")
        XCTAssertTrue(row("Retry failed crawls").waitForExistence(timeout: 3), "opened, the card lists its running chats")
        app.buttons["project-analytics"].tap()
        XCTAssertTrue(app.buttons["project-analytics"].waitForExistence(timeout: 3))
        XCTAssertEqual(app.buttons["project-analytics"].value as? String, "2 running, in Now")
    }

    func testStatusRowsAndNewChatInProject() throws {
        launch()
        let blocked = row("Fix flaky checkout tests")
        XCTAssertTrue(blocked.waitForExistence(timeout: 3), "a blocked chat sits in its project card, not folded")
        XCTAssertTrue((blocked.value as? String ?? "").hasPrefix("Needs input"))
        let review = row("Codex")
        scroll(to: review)
        XCTAssertTrue((review.value as? String ?? "").hasPrefix("Ready for review"))

        let plus = app.buttons["newChat-website"]
        scroll(to: plus)
        plus.tap()
        XCTAssertTrue(app.navigationBars["New chat"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons.matching(NSPredicate(format: "label CONTAINS 'website'")).firstMatch.exists)
    }

    func testArchiveFromCardAndBack() throws {
        launch()
        let running = row("Weekly report export")
        running.press(forDuration: 1.0)
        app.buttons["Archive"].tap()
        let left = expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: running)
        wait(for: [left], timeout: 5)
        XCTAssertEqual(app.buttons["nowShowAll"].label, "Show all 6 running", "archived running chats leave Now")

        let docs = row("Tidy the docs folder")
        scroll(to: docs)
        docs.swipeLeft()
        // On a card row the swipe can run to a full swipe, which archives without showing the button.
        let archive = app.buttons["Archive"]
        if archive.waitForExistence(timeout: 1) { archive.tap() }
        let gone = expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: docs)
        wait(for: [gone], timeout: 5)

        app.buttons["sidebarFilter"].tap()
        app.buttons["Archived"].tap()
        XCTAssertTrue(docs.waitForExistence(timeout: 3), "archived chat shows under Archived")
        XCTAssertTrue(running.exists)
        docs.press(forDuration: 1.0)
        app.buttons["Unarchive"].tap()
        let unarchived = expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: docs)
        wait(for: [unarchived], timeout: 5)

        app.buttons["sidebarFilter"].tap()
        app.buttons["All"].tap()
        scroll(to: docs)
        XCTAssertTrue(docs.exists, "unarchived chat is back in its card")
    }

    /// AX3: titles wrap instead of truncating, and the fold rows keep whole words (design-qa R7-1, R7-2).
    func testAccessibilitySizesWrap() throws {
        launch(reset: true, "-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryAccessibilityXL")
        let title = row("Weekly report export")
        XCTAssertTrue(title.waitForExistence(timeout: 5))
        XCTAssertGreaterThan(title.frame.height, 110, "a wrapped title makes the row taller")
        shot("round7-ax3-now")

        let completed = app.buttons["completed-website"]
        scroll(to: completed)
        XCTAssertLessThan(completed.frame.height, 90, "\"2 completed\" stays on one line")
        let analytics = app.buttons["project-analytics"]
        scroll(to: analytics)
        shot("round7-ax3-folds")
        XCTAssertLessThan(analytics.frame.height, 130, "name over \"2 running\", no hyphenated count")
    }

    // MARK: - Helpers

    private func rows(_ title: String) -> XCUIElementQuery {
        app.buttons.matching(NSPredicate(format: "label == %@", title))
    }

    private func row(_ title: String) -> XCUIElement { rows(title).firstMatch }

    /// Drags the sidebar list up until `element` is in the upper two thirds, clear of the floating bottom bar
    /// (a swipe that starts near it doesn't reach the row).
    private func scroll(to element: XCUIElement) {
        for _ in 0..<8 where !(element.exists && element.isHittable && element.frame.midY < app.frame.height * 0.66) {
            let start = app.coordinate(withNormalizedOffset: CGVector(dx: 0.4, dy: 0.75))
            start.press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.4, dy: 0.45)))
        }
        XCTAssertTrue(element.waitForExistence(timeout: 3) && element.isHittable, "\(element) never scrolled into view")
    }

    /// Saved to `RELAY_SHOTS` when set (pass it as `TEST_RUNNER_RELAY_SHOTS`).
    private func shot(_ name: String) {
        guard let dir = ProcessInfo.processInfo.environment["RELAY_SHOTS"] else { return }
        sleep(1)
        try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("\(name).png"))
    }
}
