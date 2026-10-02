import XCTest

/// Round 13: the Changes sheet against a live bridge. Skipped unless the runner passes
/// `TEST_RUNNER_RELAY_CHANGES_LINK` (a relay:// pair link) and `TEST_RUNNER_RELAY_CHANGES_AGENT` (the agent's key,
/// `<machineId>/<paneId>`). The agent's cwd must be a throwaway repo with: unpushed commits on a branch with an
/// upstream, a modified text file, a modified binary, a deleted file, a rename and an untracked file.
@MainActor
final class LiveChangesUITests: XCTestCase {
    private var env: [String: String] { ProcessInfo.processInfo.environment }
    private var app: XCUIApplication!

    override func setUp() async throws {
        continueAfterFailure = false
        guard let link = env["RELAY_CHANGES_LINK"], let agent = env["RELAY_CHANGES_AGENT"] else {
            throw XCTSkip("RELAY_CHANGES_LINK / RELAY_CHANGES_AGENT not set")
        }
        app = XCUIApplication()
        app.launchArguments = ["-uitest", "-pair", link, "-agent", agent]
        app.launch()
    }

    func testLiveChanges() throws {
        let button = app.buttons["changesButton"]
        XCTAssertTrue(button.waitForExistence(timeout: 20), "no Changes button in the top bar")
        shot("1-chat")
        button.tap()

        let branch = app.descendants(matching: .any)["changesBranch"]
        XCTAssertTrue(branch.waitForExistence(timeout: 15), "the branch row never loaded")
        XCTAssertTrue(branch.label.contains("ahead"), branch.label)
        let files = app.buttons.matching(identifier: "changesFile")
        XCTAssertGreaterThanOrEqual(files.count, 5)
        XCTAssertGreaterThanOrEqual(app.buttons.matching(identifier: "changesCommit").count, 1)
        shot("2-list")

        // Pull to refresh keeps the list.
        app.descendants(matching: .any)["changesList"].swipeDown(velocity: .slow)
        XCTAssertTrue(branch.waitForExistence(timeout: 10))

        // A modified text file: diff rows, then Open file.
        let modified = files.matching(NSPredicate(format: "label CONTAINS 'Modified' AND NOT (label CONTAINS 'binary')")).firstMatch
        XCTAssertTrue(modified.exists, "no modified text file")
        modified.tap()
        XCTAssertTrue(app.descendants(matching: .any)["changesDiff"].waitForExistence(timeout: 10), "no diff")
        XCTAssertGreaterThan(app.descendants(matching: .any).matching(identifier: "diffLine").count, 0)
        shot("3-diff")
        app.buttons["changesOpenFile"].tap()
        XCTAssertTrue(app.descendants(matching: .any)["fileViewerText"].waitForExistence(timeout: 10), "Open file didn't show the file")
        shot("4-file")
        app.buttons["fileViewerDone"].tap()
        back()

        // Deleted: a diff of removals and no Open file.
        files.matching(NSPredicate(format: "label CONTAINS 'Deleted'")).firstMatch.tap()
        XCTAssertTrue(app.descendants(matching: .any)["changesDiff"].waitForExistence(timeout: 10))
        XCTAssertFalse(app.buttons["changesOpenFile"].exists, "Open file on a deleted file")
        back()

        // Binary: no text diff.
        files.matching(NSPredicate(format: "label CONTAINS 'binary'")).firstMatch.tap()
        XCTAssertTrue(app.descendants(matching: .any)["changesBinary"].waitForExistence(timeout: 10))
        shot("5-binary")
        back()

        // Untracked: all additions.
        files.matching(NSPredicate(format: "label CONTAINS 'Untracked'")).firstMatch.tap()
        XCTAssertTrue(app.descendants(matching: .any)["changesDiff"].waitForExistence(timeout: 10))
        back()

        // A commit and one of its files.
        app.buttons.matching(identifier: "changesCommit").firstMatch.tap()
        XCTAssertTrue(app.descendants(matching: .any)["commitMessage"].waitForExistence(timeout: 10), "commit detail never loaded")
        shot("6-commit")
        app.buttons.matching(identifier: "commitFile").firstMatch.tap()
        XCTAssertTrue(app.descendants(matching: .any)["changesDiff"].waitForExistence(timeout: 10))
        shot("7-commit-diff")
        back()
        back()

        app.buttons["changesDone"].tap()
        XCTAssertTrue(button.waitForExistence(timeout: 5))
    }

    private func back() {
        app.navigationBars.buttons.element(boundBy: 0).tap()
    }

    /// Kept in the result bundle (`xcresulttool export attachments`).
    private func shot(_ name: String) {
        sleep(1)
        let attachment = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
        attachment.name = "live-changes-\(name)"
        attachment.lifetime = .keepAlways
        add(attachment)
    }
}
