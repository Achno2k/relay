import XCTest

/// The sidebar shell (design B): toolbar menus, search, New chat and the chat peek. Mock backend.
@MainActor
final class SidebarShellUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() async throws {
        continueAfterFailure = false
        app = XCUIApplication()
        app.launchArguments = ["-uitest", "-mock", "-replay", "off", "-resetSidebar", "-agent", "w2:p1", "-demo", "sidebar"]
        app.launch()
        XCTAssertTrue(app.buttons["sidebarMachineMenu"].waitForExistence(timeout: 10), "sidebar didn't open")
    }

    /// The search circle grows into a field; results replace the grouped cards; closing brings them back.
    func testSearch() throws {
        shot("shell-open")
        app.buttons["sidebarSearch"].tap()
        let field = app.textFields["sidebarSearchField"]
        XCTAssertTrue(field.waitForExistence(timeout: 3))
        XCTAssertFalse(app.buttons["sidebarNewChat"].exists, "New chat should give way to the field")
        shot("shell-search-empty")
        field.typeText("landing")

        let hit = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Landing page hero redesign'")).firstMatch
        XCTAssertTrue(hit.waitForExistence(timeout: 3), "search found nothing")
        XCTAssertFalse(app.staticTexts["Now"].exists, "grouped cards still showing during a search")
        shot("shell-search-results")

        field.typeText("zzzz")
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH 'No chats match'")).firstMatch.waitForExistence(timeout: 3))

        app.buttons["Close search"].tap()
        XCTAssertTrue(app.buttons["sidebarSearch"].waitForExistence(timeout: 3))
        XCTAssertTrue(app.buttons["sidebarNewChat"].exists)
    }

    /// The machine menu lists the paired Mac and its connection; ••• still holds Usage.
    func testToolbarMenus() throws {
        app.buttons["sidebarMachineMenu"].tap()
        XCTAssertTrue(app.buttons["Connected"].waitForExistence(timeout: 3) || app.staticTexts["Connected"].exists,
                      "machine menu has no connection row")
        app.windows.firstMatch.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.6)).tap()

        app.buttons["sidebarMore"].tap()
        let usage = app.buttons["sidebarUsage"]
        XCTAssertTrue(usage.waitForExistence(timeout: 3))
        usage.tap()
        XCTAssertTrue(app.navigationBars["Usage"].waitForExistence(timeout: 5) || app.staticTexts["Usage"].waitForExistence(timeout: 1))
    }

    /// The chat peeks on the right as a labelled button that returns to it.
    func testPeekReturnsToChat() throws {
        let peek = app.descendants(matching: .any)["sidebarPeek"]
        XCTAssertTrue(peek.waitForExistence(timeout: 3))
        XCTAssertTrue(peek.label.hasPrefix("Return to"), "peek label: \(peek.label)")
        XCTAssertGreaterThan(peek.frame.minX, 300, "peek should sit right of the 342 pt sidebar")
        // Most of the chat sits off screen; tap the strip that peeks.
        peek.coordinate(withNormalizedOffset: CGVector(dx: 0.05, dy: 0.5)).tap()
        // Closed once the machine menu has slid off to the left.
        let machine = app.buttons["sidebarMachineMenu"]
        let deadline = Date().addingTimeInterval(4)
        while machine.exists && machine.frame.minX >= 0 && Date() < deadline { usleep(100_000) }
        XCTAssertTrue(!machine.exists || machine.frame.minX < 0, "tapping the peek didn't return to the chat")
    }

    /// New chat opens the sheet.
    func testNewChat() throws {
        app.buttons["sidebarNewChat"].tap()
        XCTAssertTrue(app.navigationBars["New chat"].waitForExistence(timeout: 5))
    }

    /// VoiceOver order: toolbar, then the list, then the bottom bar (R7-4).
    func testAccessibilityOrder() throws {
        let tree = app.debugDescription
        if let dir = ProcessInfo.processInfo.environment["RELAY_SHOTS"] {
            try? tree.write(toFile: dir + "/shell-a11y-tree.txt", atomically: true, encoding: .utf8)
        }
        func position(_ id: String) -> String.Index {
            guard let r = tree.range(of: "'\(id)'") else {
                XCTFail("\(id) not in the accessibility tree"); return tree.endIndex
            }
            return r.lowerBound
        }
        let machine = position("sidebarMachineMenu"), more = position("sidebarMore")
        let row = tree.range(of: "Weekly report export")?.lowerBound ?? tree.endIndex
        let search = position("sidebarSearch"), newChat = position("sidebarNewChat")
        XCTAssertLessThan(machine, row, "toolbar should come before the list")
        XCTAssertLessThan(more, row, "toolbar should come before the list")
        XCTAssertLessThan(row, search, "list should come before the bottom bar")
        XCTAssertLessThan(row, newChat, "list should come before the bottom bar")
    }

    /// At AX3 and above New chat is one line (icon only, still labelled), and the toolbar fits the sidebar (R7-3).
    func testAccessibilitySizes() throws {
        for size in ["UICTContentSizeCategoryAccessibilityXL", "UICTContentSizeCategoryAccessibilityXXXL"] {
            app.terminate()
            app.launchArguments += ["-UIPreferredContentSizeCategoryName", size]
            app.launch()
            let machine = app.buttons["sidebarMachineMenu"], more = app.buttons["sidebarMore"]
            XCTAssertTrue(machine.waitForExistence(timeout: 10))
            let newChat = app.buttons["sidebarNewChat"]
            XCTAssertEqual(newChat.label, "New chat")
            XCTAssertLessThanOrEqual(newChat.frame.height, 60, "New chat wrapped at \(size)")
            XCTAssertGreaterThanOrEqual(machine.frame.minX, 0)
            XCTAssertLessThanOrEqual(machine.frame.maxX, more.frame.minX, "machine menu runs into the pill at \(size)")
            XCTAssertLessThanOrEqual(more.frame.maxX, 342, "pill spills out of the sidebar at \(size)")
            XCTAssertGreaterThanOrEqual(more.frame.width, 44)
            shot("shell-\(size)")
        }
    }

    /// Saved to `RELAY_SHOTS` when set (pass it as `TEST_RUNNER_RELAY_SHOTS`).
    private func shot(_ name: String) {
        guard let dir = ProcessInfo.processInfo.environment["RELAY_SHOTS"] else { return }
        sleep(1)
        let png = XCUIScreen.main.screenshot().pngRepresentation
        try? png.write(to: URL(fileURLWithPath: dir).appendingPathComponent("\(name).png"))
    }
}
