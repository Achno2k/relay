import XCTest

/// Round 2 against the mock: scroll button, sidebar tree, filter menu, archive, attachments.
@MainActor
final class Round2UITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() async throws {
        continueAfterFailure = false
        app = XCUIApplication()
    }

    private func launch(_ args: String...) {
        app.launchArguments = ["-uitest", "-mock", "-replay", "off", "-resetSidebar"] + args
        app.launch()
    }

    // MARK: 1. Scroll-to-bottom button

    func testScrollButtonReturnsToBottom() throws {
        launch("-agent", "w2:p1")
        let lastCode = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH '<Button'")).firstMatch
        XCTAssertTrue(lastCode.waitForExistence(timeout: 10))
        let button = app.buttons["Scroll to bottom"]
        XCTAssertFalse(button.exists)

        let scroll = app.scrollViews["transcript"]
        for _ in 0..<4 where !button.exists { scroll.swipeDown(velocity: .fast) }
        XCTAssertTrue(button.waitForExistence(timeout: 3), "↓ never appeared after scrolling up")
        XCTAssertFalse(lastCode.isHittable)

        button.tap()
        let gone = expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: button)
        wait(for: [gone], timeout: 5)
        XCTAssertTrue(lastCode.isHittable, "the last message isn't on screen after tapping ↓")
    }

    // MARK: 3. Sidebar tree

    func testProjectsTreeAndNewChatInFolder() throws {
        launch("-agent", "w1:p1")
        openSidebar()
        let shop = app.buttons["project-shop-api"]
        XCTAssertTrue(shop.waitForExistence(timeout: 5))
        XCTAssertEqual(shop.value as? String, "collapsed", "folders start collapsed")
        XCTAssertFalse(row("Fix flaky checkout tests").exists)

        shop.tap()
        XCTAssertTrue(row("Fix flaky checkout tests").waitForExistence(timeout: 3))
        XCTAssertEqual(shop.value as? String, "expanded")
        shot("round2-sidebar-expanded")

        app.buttons["newChat-website"].tap()
        XCTAssertTrue(app.navigationBars["New chat"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.buttons.matching(NSPredicate(format: "label CONTAINS 'website'")).firstMatch.exists)
    }

    // MARK: 4. Filter menu

    func testFilterMenuShowsFlatSessions() throws {
        launch("-agent", "w2:p1")
        openSidebar()
        let filter = app.buttons["sidebarFilter"]
        XCTAssertTrue(filter.waitForExistence(timeout: 5))
        XCTAssertEqual(filter.value as? String, "needs input", "orange dot while something is blocked")

        filter.tap()
        XCTAssertTrue(app.buttons["Ready for review"].waitForExistence(timeout: 3))
        shot("round2-filter-menu")
        app.buttons["Needs input"].tap()

        let blocked = row("Fix flaky checkout tests")
        XCTAssertTrue(blocked.waitForExistence(timeout: 3))
        XCTAssertTrue((blocked.value as? String ?? "").hasPrefix("Waiting for you · shop-api"), "flat rows name the folder")
        XCTAssertFalse(row("Refactor auth middleware").exists)
        XCTAssertFalse(app.buttons["project-shop-api"].exists, "Projects tree is replaced by Sessions")
        shot("round2-sessions-needs-input")

        filter.tap()
        app.buttons["Working"].tap()
        XCTAssertTrue(row("Refactor auth middleware").waitForExistence(timeout: 3))
        XCTAssertFalse(row("Fix flaky checkout tests").exists)

        // Remembered across launches.
        app.terminate()
        app.launchArguments = ["-uitest", "-mock", "-replay", "off", "-agent", "w2:p1"]
        app.launch()
        openSidebar()
        XCTAssertTrue(row("Refactor auth middleware").waitForExistence(timeout: 5))
        XCTAssertEqual(app.buttons["sidebarFilter"].label, "Filter: Working")
    }

    // MARK: 4. Archive

    func testArchiveAndUnarchive() throws {
        launch("-agent", "w1:p1")
        openSidebar()
        app.buttons["project-website"].tap()
        let landing = row("Landing page hero redesign")
        XCTAssertTrue(landing.waitForExistence(timeout: 3))

        landing.swipeLeft()
        app.buttons["Archive"].tap()
        let gone = expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: landing)
        wait(for: [gone], timeout: 5)

        app.buttons["sidebarFilter"].tap()
        app.buttons["Archived"].tap()
        XCTAssertTrue(landing.waitForExistence(timeout: 3), "archived chat shows under Archived")
        shot("round2-archived")

        landing.press(forDuration: 1.0)
        app.buttons["Unarchive"].tap()
        let left = expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: landing)
        wait(for: [left], timeout: 5)

        app.buttons["sidebarFilter"].tap()
        app.buttons["All"].tap()
        XCTAssertTrue(landing.waitForExistence(timeout: 3), "unarchived chat is back in its folder")
    }

    // MARK: 2. Attachments

    func testAttachImageAndPDF() throws {
        launch("-agent", "w2:p1", "-uitestAttachments")
        let composer = app.descendants(matching: .any).matching(NSPredicate(format: "placeholderValue BEGINSWITH 'Message'")).firstMatch
        XCTAssertTrue(composer.waitForExistence(timeout: 10))

        attach("Test image (RELAY)")
        attach("Test PDF")
        let items = app.descendants(matching: .any).matching(identifier: "trayItem")
        XCTAssertEqual(items.count, 2)
        shot("round2-composer-uploading")
        let uploaded = NSPredicate(format: "value == 'uploaded'")
        for i in 0..<2 {
            let done = expectation(for: uploaded, evaluatedWith: items.element(boundBy: i))
            wait(for: [done], timeout: 10)
        }
        composer.tap()
        composer.typeText("What's in these?")
        shot("round2-composer-attachments")
        app.buttons["Send"].tap()

        XCTAssertTrue(app.buttons["attachmentImage"].firstMatch.waitForExistence(timeout: 5), "no thumbnail in the bubble")
        XCTAssertTrue(app.buttons["attachmentFile"].firstMatch.exists, "no file chip in the bubble")
        let reply = app.staticTexts.matching(NSPredicate(format: "label CONTAINS 'I got 2 attachments'")).firstMatch
        XCTAssertTrue(reply.waitForExistence(timeout: 10))
        shot("round2-bubble-attachments")

        app.buttons["attachmentImage"].firstMatch.tap()
        XCTAssertTrue(app.buttons["Close"].waitForExistence(timeout: 5), "image didn't open full screen")
        shot("round2-image-viewer")
        app.buttons["Close"].tap()
    }

    // MARK: - Helpers

    private func openSidebar() {
        let open = app.buttons["Open sidebar"]
        XCTAssertTrue(open.waitForExistence(timeout: 10))
        open.tap()
        _ = app.buttons["sidebarFilter"].waitForExistence(timeout: 3)
    }

    private func row(_ title: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "label == %@", title)).firstMatch
    }

    private func attach(_ item: String) {
        app.buttons["composerPlus"].tap()
        let button = app.buttons[item]
        XCTAssertTrue(button.waitForExistence(timeout: 3), "no \(item) in the + menu")
        button.tap()
    }

    /// Saved to `RELAY_SHOTS` when set (pass it as `TEST_RUNNER_RELAY_SHOTS`).
    private func shot(_ name: String) {
        guard let dir = ProcessInfo.processInfo.environment["RELAY_SHOTS"] else { return }
        sleep(1)
        let png = XCUIScreen.main.screenshot().pngRepresentation
        try? png.write(to: URL(fileURLWithPath: dir).appendingPathComponent("\(name).png"))
    }
}
