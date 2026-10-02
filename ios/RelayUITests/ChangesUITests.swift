import XCTest

/// Round 13: the read-only Changes sheet against the mock (`MockChanges`). `w2:p1` (website) has a busy branch,
/// `w1:p1` is clean and pushed, `w3:p1` (analytics) isn't a git repository.
@MainActor
final class ChangesUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() async throws {
        continueAfterFailure = false
        app = XCUIApplication()
    }

    private func launch(agent: String) {
        app.launchArguments = ["-uitest", "-mock", "-replay", "off", "-resetSidebar", "-agent", agent]
        app.launch()
    }

    private func openChanges() {
        let button = app.buttons["changesButton"]
        XCTAssertTrue(button.waitForExistence(timeout: 10), "no Changes button in the top bar")
        XCTAssertEqual(button.label, "Changes")
        button.tap()
        XCTAssertTrue(app.descendants(matching: .any)["changesSheet"].waitForExistence(timeout: 5), "the sheet didn't open")
    }

    func testBranchFilesAndCommits() throws {
        launch(agent: "w2:p1")
        openChanges()
        let branch = app.descendants(matching: .any)["changesBranch"]
        XCTAssertTrue(branch.waitForExistence(timeout: 5))
        XCTAssertTrue(branch.label.contains("hero-redesign"), branch.label)
        XCTAssertTrue(branch.label.contains("2 ahead, 1 behind"), branch.label)
        for name in ["Hero.tsx", "README.md", "hero.png", "section3.css", "intro.md", "Banner.tsx"] {
            XCTAssertTrue(file(name).exists, "no row for \(name)")
        }
        for header in ["Modified", "New", "Deleted", "Renamed"] {
            XCTAssertTrue(app.staticTexts[header].exists, "no \(header) section")
        }
        // Read-only: no git actions anywhere.
        for word in ["Commit", "Push", "Stage", "Discard"] {
            XCTAssertFalse(app.buttons[word].exists, "\(word) button on a read-only screen")
        }
        shot("list")

        // A modified file: its diff, then the file itself.
        file("Hero.tsx").tap()
        XCTAssertTrue(app.descendants(matching: .any)["changesDiff"].waitForExistence(timeout: 5), "no diff")
        XCTAssertGreaterThan(app.descendants(matching: .any).matching(identifier: "diffLine").count, 0)
        XCTAssertFalse(app.descendants(matching: .any)["diffFile"].exists, "the file header row repeats the title")
        shot("diff")
        let open = app.buttons["changesOpenFile"]
        XCTAssertTrue(open.exists)
        open.tap()
        XCTAssertTrue(app.descendants(matching: .any)["fileViewerText"].waitForExistence(timeout: 5), "Open file didn't show the file")
        app.buttons["fileViewerDone"].tap()
        XCTAssertTrue(open.waitForExistence(timeout: 3), "closing the file viewer left the diff")
        back()

        // A deleted file has nothing to open.
        file("intro.md").tap()
        XCTAssertTrue(app.descendants(matching: .any)["changesDiff"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["changesOpenFile"].exists, "Open file on a deleted file")
        back()

        // A binary file: no text diff, Open file instead.
        file("hero.png").tap()
        XCTAssertTrue(app.descendants(matching: .any)["changesBinary"].waitForExistence(timeout: 5))
        back()

        // A pure rename says so.
        XCTAssertTrue(file("Banner.tsx").label.contains("from src/components/Promo.tsx"))
        file("Banner.tsx").tap()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS 'Renamed from'")).firstMatch.waitForExistence(timeout: 5))
        back()

        // A commit: message, files, a file's diff.
        let commit = app.buttons.matching(identifier: "changesCommit").firstMatch
        if !commit.isHittable { app.descendants(matching: .any)["changesList"].swipeUp() }
        commit.tap()
        XCTAssertTrue(app.descendants(matching: .any)["commitMessage"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS 'small phones'")).firstMatch.exists, "no commit body")
        XCTAssertEqual(app.buttons.matching(identifier: "commitFile").count, 2)
        shot("commit")
        app.buttons.matching(identifier: "commitFile").firstMatch.tap()
        XCTAssertTrue(app.descendants(matching: .any)["changesDiff"].waitForExistence(timeout: 5))
        shot("commit-diff")
        back()
        back()

        app.buttons["changesDone"].tap()
        XCTAssertTrue(waitFor(app.descendants(matching: .any)["changesSheet"], "exists == false"), "Done didn't close the sheet")
    }

    func testCleanAndPushed() throws {
        launch(agent: "w1:p1")
        openChanges()
        XCTAssertTrue(app.descendants(matching: .any)["changesNoFiles"].waitForExistence(timeout: 5))
        XCTAssertTrue(app.staticTexts["No uncommitted changes"].exists)
        XCTAssertTrue(app.staticTexts["Everything is pushed"].exists)
        XCTAssertTrue(app.descendants(matching: .any)["changesBranch"].label.contains("Up to date"))
        shot("clean")
    }

    func testNotARepository() throws {
        launch(agent: "w3:p1")
        openChanges()
        XCTAssertTrue(app.staticTexts["Not a git repository"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.descendants(matching: .any)["changesBranch"].exists)
        shot("not-repo")
    }

    func testAccessibilityAudit() throws {
        launch(agent: "w2:p1")
        openChanges()
        XCTAssertTrue(app.descendants(matching: .any)["changesBranch"].waitForExistence(timeout: 5))
        // Text at the sheet's bottom edge reads as clipped at larger sizes; it's audited once scrolled up.
        let bottom = app.windows.firstMatch.frame.maxY * 0.85
        try audit([.dynamicType, .elementDetection, .hitRegion, .sufficientElementDescription, .trait]) { issue in
            issue.auditType == .dynamicType && (issue.element?.frame.maxY ?? 0) > bottom
        }
        app.descendants(matching: .any)["changesList"].swipeUp()
        // An issue with no element (zero frame) can't be traced to a view of this screen.
        try audit([.dynamicType, .hitRegion]) { $0.element == nil || $0.element?.frame.isEmpty == true }
        file("Hero.tsx").tap()
        XCTAssertTrue(app.descendants(matching: .any)["changesDiff"].waitForExistence(timeout: 5))
        try audit([.elementDetection, .sufficientElementDescription, .trait])
    }

    private func audit(_ types: XCUIAccessibilityAuditType, ignoring: @escaping (XCUIAccessibilityAuditIssue) -> Bool = { _ in false }) throws {
        try app.performAccessibilityAudit(for: types) { issue in
            let ignored = ignoring(issue)
            print("AUDIT", ignored ? "ignored" : "issue", issue.compactDescription, issue.element?.label ?? "", issue.element?.frame ?? .zero)
            return ignored
        }
    }

    // MARK: - Helpers

    private func file(_ name: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "identifier == 'changesFile' AND label BEGINSWITH %@", name + ",")).firstMatch
    }

    private func back() {
        app.navigationBars.buttons.element(boundBy: 0).tap()
    }

    private func waitFor(_ element: XCUIElement, _ predicate: String, timeout: TimeInterval = 5) -> Bool {
        let e = expectation(for: NSPredicate(format: predicate), evaluatedWith: element)
        return XCTWaiter().wait(for: [e], timeout: timeout) == .completed
    }

    /// Kept in the result bundle, and saved to `RELAY_SHOTS` when set (pass it as `TEST_RUNNER_RELAY_SHOTS`).
    private func shot(_ name: String) {
        sleep(1)
        let screenshot = XCUIScreen.main.screenshot()
        let attachment = XCTAttachment(screenshot: screenshot)
        attachment.name = "changes-\(name)"
        attachment.lifetime = .keepAlways
        add(attachment)
        guard let dir = ProcessInfo.processInfo.environment["RELAY_SHOTS"] else { return }
        try? screenshot.pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("changes-\(name).png"))
    }
}
