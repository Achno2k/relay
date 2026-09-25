import XCTest

/// `reply.live.tool` against the mock backend's "slow bash" turn: the "Running Bash…" row shows as
/// soon as the screen has the call, the transcript's toolCall takes over the same row (it stays
/// expanded, there's never a second one), and the raw tool block never shows as text.
@MainActor
final class LiveToolUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() async throws {
        continueAfterFailure = false
        app = XCUIApplication()
        app.launchArguments = ["-uitest", "-mock", "-replay", "off", "-resetSidebar", "-agent", "w2:p1"]
        app.launch()
    }

    private var composer: XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "placeholderValue BEGINSWITH 'Message'")).firstMatch
    }

    private var runningRows: XCUIElementQuery {
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Running Bash'"))
    }

    private var stepRows: XCUIElementQuery {
        app.descendants(matching: .any).matching(NSPredicate(format: "label == 'Ran sleep 5'"))
    }

    private var rawToolText: XCUIElementQuery {
        app.staticTexts.matching(NSPredicate(format: "label CONTAINS 'Bash(' OR label CONTAINS '⎿'"))
    }

    func testRunningToolShowsImmediatelyAndSwapsInPlace() throws {
        XCTAssertTrue(composer.waitForExistence(timeout: 10))
        composer.tap()
        composer.typeText("slow bash please")
        let sent = Date()
        app.buttons["Send"].tap()

        // The mock's transcript call lands about 5.3s after the send; the live row must beat it by far.
        XCTAssertTrue(runningRows.firstMatch.waitForExistence(timeout: 4), "no Running Bash… row from reply.live")
        let shownAfter = Date().timeIntervalSince(sent)
        XCTAssertLessThan(shownAfter, 4.5, "the row only came with the transcript")
        shot("live-tool-1-running")

        runningRows.firstMatch.tap()
        XCTAssertTrue(stepRows.firstMatch.waitForExistence(timeout: 2), "expanding the live row shows its step")
        // The step shows up as its combined row plus the text inside; whatever that is, it must not change.
        let steps = stepRows.count

        // Across the swap (transcript call, then tool: null, then the result): one row, still
        // expanded, and never the raw tool block as text.
        let end = Date().addingTimeInterval(5)
        var samples = 0
        while Date() < end {
            XCTAssertLessThanOrEqual(runningRows.count, 1, "duplicate Running Bash… row")
            XCTAssertEqual(stepRows.count, steps, "the step row vanished or doubled: the row was rebuilt")
            XCTAssertEqual(rawToolText.count, 0, "tool text rendered as reply text")
            samples += 1
            Thread.sleep(forTimeInterval: 0.15)
        }
        XCTAssertGreaterThan(samples, 5)

        XCTAssertTrue(app.staticTexts["Slept for 5 seconds."].waitForExistence(timeout: 5))
        XCTAssertEqual(runningRows.count, 0, "still Running once the turn finished")
        XCTAssertEqual(stepRows.count, steps, "the landed row kept the live row's expanded state")
        shot("live-tool-2-landed")
    }

    private func shot(_ name: String) {
        guard let dir = ProcessInfo.processInfo.environment["RELAY_SHOTS"] else { return }
        let png = XCUIScreen.main.screenshot().pngRepresentation
        try? png.write(to: URL(fileURLWithPath: dir).appendingPathComponent("\(name).png"))
    }
}

/// The same against a live bridge and a real Claude agent running a slow Bash command. Skipped unless
/// `TEST_RUNNER_RELAY_E2E_LINK` / `TEST_RUNNER_RELAY_E2E_AGENT` are set (same env as `LiveE2ETests`).
/// Drives the recording in docs/tasks/round-6/live-tool.mov.
@MainActor
final class LiveToolE2ETests: XCTestCase {
    private var env: [String: String] { ProcessInfo.processInfo.environment }
    private var app: XCUIApplication!

    override func setUp() async throws {
        continueAfterFailure = false
        guard let link = env["RELAY_E2E_LINK"], let agent = env["RELAY_E2E_AGENT"] else {
            throw XCTSkip("RELAY_E2E_LINK / RELAY_E2E_AGENT not set")
        }
        app = XCUIApplication()
        app.launchArguments = ["-uitest", "-pair", link, "-agent", agent]
        app.launch()
    }

    func testSlowBashShowsRunningRowWithoutToolText() throws {
        let composer = app.descendants(matching: .any).matching(NSPredicate(format: "placeholderValue BEGINSWITH 'Message'")).firstMatch
        XCTAssertTrue(composer.waitForExistence(timeout: 15), "composer never appeared")
        composer.tap()
        composer.typeText("Use the Bash tool to run exactly this one command: ping -c 8 127.0.0.1 . Then reply with just the word done.")
        app.buttons["Send"].tap()

        let running = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Running Bash'"))
        let rawToolText = app.staticTexts.matching(NSPredicate(format: "label CONTAINS 'Bash(' OR label CONTAINS '⎿' OR label CONTAINS 'PING 127'"))
        XCTAssertTrue(running.firstMatch.waitForExistence(timeout: 45), "no Running Bash… row")
        shot("live-tool-e2e-1-running")

        // While it runs (8s of ping) and through the transcript landing: one row, no tool text.
        var sawRunning = 0
        let end = Date().addingTimeInterval(12)
        while Date() < end {
            XCTAssertLessThanOrEqual(running.count, 1, "duplicate Running Bash… row")
            XCTAssertEqual(rawToolText.count, 0, "tool text rendered as reply text")
            if running.count == 1 { sawRunning += 1 }
            Thread.sleep(forTimeInterval: 0.2)
        }
        XCTAssertGreaterThan(sawRunning, 5)

        XCTAssertTrue(app.buttons["Send"].waitForExistence(timeout: 90), "turn never finished")
        sleep(2)
        XCTAssertEqual(running.count, 0, "still Running once the turn finished")
        shot("live-tool-e2e-2-landed")
    }

    private func shot(_ name: String) {
        let png = XCUIScreen.main.screenshot().pngRepresentation
        let attachment = XCTAttachment(data: png, uniformTypeIdentifier: "public.png")
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
        if let dir = env["RELAY_E2E_SHOTS"] {
            try? png.write(to: URL(fileURLWithPath: dir).appendingPathComponent("\(name).png"))
        }
    }
}
