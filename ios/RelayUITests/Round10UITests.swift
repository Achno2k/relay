import XCTest

/// Round 10, the user's bug list, against the mock. Plan and results: `docs/qa/round10.md`.
/// `w2:p1` ("Landing page hero redesign", idle) has a long history plus Read/Edit rows with files
/// (`MockChats.landing`, `MockFiles`). The mock answers any prompt with a Read and a line of text
/// after ~3 s; a prompt starting "slow bash" streams a live Bash tool for ~5 s instead.
/// Not covered here (needs a real bridge or a finger): B5 mid-fling, B6, R10-1. See round10.md.
@MainActor
final class Round10UITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() async throws {
        continueAfterFailure = false
        app = XCUIApplication()
    }

    private func launch(agent: String = "w2:p1", _ args: [String] = []) {
        app.launchArguments = ["-uitest", "-mock", "-replay", "off", "-resetSidebar", "-agent", agent] + args
        app.launch()
    }

    // MARK: B8, B3: feedback from the moment a prompt is sent

    /// B8: after send, the field empties and a shimmering "Thinking…" shows at once, not a lone dot.
    /// On `w2:p3`, an empty chat: on the long `w2:p1` history each query takes longer than the mock's turn.
    func testB8ThinkingShowsRightAfterSend() throws {
        launch(agent: "w2:p3")
        send("Round ten ping")
        // "Thinking…" until the mock's Read starts at ~1.2 s, then the shimmering "Running Read…" row.
        let busyNow = busy.waitForExistence(timeout: 2)
        if !busyNow {
            print("B8TREE", app.debugDescription.split(separator: "\n").filter { $0.contains("Thinking") || $0.contains("Running") || $0.contains("Round ten") }.joined(separator: "\n"))
        }
        XCTAssertTrue(busyNow, "nothing shows the agent is busy right after send")
        XCTAssertTrue(bubble("Round ten ping").exists, "user bubble didn't show at once")
        XCTAssertFalse((composer.value as? String ?? "").contains("Round ten ping"), "typed text stayed in the field")
        shot("b8-thinking")

        // The mock's reply lands after ~3 s; the indicator goes with it.
        XCTAssertTrue(text("This is the mock backend").waitForExistence(timeout: 8), "reply never landed")
        XCTAssertTrue(waitFor(busy, "exists == false", timeout: 5), "still shows busy after the reply")
    }

    /// B3: while a tool runs, the shimmer names it; it never looks idle while the agent works.
    func testB3RunningToolIsNamed() throws {
        launch(agent: "w2:p3")
        send("slow bash please")
        XCTAssertTrue(busy.waitForExistence(timeout: 1.5), "nothing shows the agent is busy right after send")
        let running = app.descendants(matching: .any)
            .matching(NSPredicate(format: "label BEGINSWITH 'Running Bash'")).firstMatch
        XCTAssertTrue(running.waitForExistence(timeout: 4), "the live Bash tool never showed as 'Running Bash…'")
        shot("b3-running")
        XCTAssertTrue(text("Slept for 5 seconds").waitForExistence(timeout: 12), "reply never landed")
        XCTAssertTrue(waitFor(busy, "exists == false", timeout: 5), "shimmer stayed after the turn ended")
    }

    // MARK: B5: scroll to bottom

    /// B5 (at rest only; XCUITest waits for the list to settle before a tap, so mid-fling is a manual
    /// check, see round10.md): ↓ shows once scrolled up and brings back the last message.
    func testB5ScrollToBottomButton() throws {
        launch()
        let list = app.descendants(matching: .any)["transcript"]
        XCTAssertTrue(list.waitForExistence(timeout: 10))
        for _ in 0..<4 { list.swipeDown(velocity: .fast) }
        let down = app.buttons["Scroll to bottom"]
        XCTAssertTrue(down.waitForExistence(timeout: 3), "↓ didn't show after scrolling up")
        down.tap()
        XCTAssertTrue(waitFor(down, "exists == false", timeout: 4), "↓ still there after tapping it")
        XCTAssertTrue(composer.isHittable)
    }

    // MARK: B2: tap files in chat

    /// B2: an Edit row opens its diff; the File tab shows the file as it is now.
    func testB2EditRowOpensDiffAndFile() throws {
        launch(["-demo", "tools"])
        let edit = fileRow("Hero.tsx", name: "Edited")
        XCTAssertTrue(scrollChat(to: edit), "no tappable Edit row for Hero.tsx")
        edit.tap()
        XCTAssertTrue(viewer.waitForExistence(timeout: 5), "file viewer didn't open")
        XCTAssertTrue(element("fileViewerDiff").waitForExistence(timeout: 5), "no diff on the Changes tab")
        XCTAssertGreaterThan(app.descendants(matching: .any).matching(identifier: "diffLine").count, 0)
        shot("b2-diff")

        let fileTab = app.descendants(matching: .any)["fileViewerTabs"].buttons["File"]
        XCTAssertTrue(fileTab.waitForExistence(timeout: 3), "no File tab")
        fileTab.tap()
        XCTAssertTrue(element("fileViewerText").waitForExistence(timeout: 5), "File tab didn't load the file")
        XCTAssertGreaterThan(app.descendants(matching: .any).matching(identifier: "codeLine").count, 0)
        shot("b2-file")

        closeViewer()
        XCTAssertTrue(composer.waitForExistence(timeout: 3))
    }

    /// B2: a Read row opens the file (no Changes tab); an image and Markdown render as such.
    func testB2ReadRowsOpenFiles() throws {
        launch(["-demo", "file:l-t3"])
        XCTAssertTrue(viewer.waitForExistence(timeout: 8), "-demo file: didn't open the viewer")
        XCTAssertTrue(element("fileViewerImage").waitForExistence(timeout: 5), "hero.png didn't show as an image")
        shot("b2-image")
        closeViewer()

        app.terminate()
        launch(["-demo", "tools"])
        let readme = fileRow("README.md", name: "Read")
        XCTAssertTrue(scrollChat(to: readme), "no tappable Read row for README.md")
        readme.tap()
        XCTAssertTrue(element("fileViewerMarkdown").waitForExistence(timeout: 5), "README.md didn't render as Markdown")
        XCTAssertFalse(app.descendants(matching: .any)["fileViewerTabs"].exists, "a Read has no Changes, so no tabs")
        closeViewer()
    }

    /// B2: only rows with a file to show are tappable; a Glob row stays a plain step.
    func testB2OnlyFileRowsAreTappable() throws {
        launch(agent: "w1:p2", ["-demo", "tools"])
        let glob = app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier == 'toolStep' AND label CONTAINS 'Found tests/checkout'")).firstMatch
        XCTAssertTrue(glob.waitForExistence(timeout: 10), "Glob row missing or wrongly tappable")
        let read = fileRow("conftest.py", name: "Read")
        XCTAssertTrue(scrollChat(to: read), "Read conftest.py isn't tappable")
    }

    // MARK: B7: the plan from plan mode

    /// B7: `-mockPlan` puts `w1:p2` at a plan approval. The sheet shows the plan above the options.
    func testB7PlanInApprovalSheet() throws {
        launch(agent: "w1:p2", ["-mockPlan"])
        let plan = element("approvalPlan").firstMatch
        XCTAssertTrue(plan.waitForExistence(timeout: 10), "approval sheet has no plan")
        XCTAssertTrue(element("approvalQuestion").firstMatch.exists)
        let options = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Yes, and'"))
        XCTAssertTrue(options.firstMatch.waitForExistence(timeout: 3), "no options under the plan")
        XCTAssertTrue(options.firstMatch.isHittable, "options not reachable with a plan shown")
        XCTAssertLessThanOrEqual(plan.frame.maxY, options.firstMatch.frame.minY + 1, "plan runs under the options")
        shot("b7-sheet")
    }

    /// B7: the chat shows the plan as a card after the ExitPlanMode tools, collapsed with "Show full plan".
    func testB7PlanCardInChat() throws {
        launch(agent: "w1:p2", ["-mockPlan", "-demo", "card"])
        let card = element("planCard").firstMatch
        XCTAssertTrue(card.waitForExistence(timeout: 10), "no plan card in the chat")
        // By label: the card's `planCard` id also lands on its children, so `planToggle` isn't queryable.
        let toggle = app.buttons["Show full plan"].firstMatch
        XCTAssertTrue(toggle.waitForExistence(timeout: 3), "long plan has no Show full plan")
        toggle.tap()
        XCTAssertTrue(app.buttons["Show less"].firstMatch.waitForExistence(timeout: 3), "plan card didn't expand")
        shot("b7-card")
    }

    // MARK: B1: the gap under the sidebar's "Relay" title

    /// B1: the first section sits close under the large title (r10-polish: 8 pt under it), not a big gap.
    func testB1TitleGap() throws {
        launch(["-demo", "sidebar"])
        let title = app.staticTexts["Relay"].firstMatch
        XCTAssertTrue(title.waitForExistence(timeout: 10))
        let now = app.descendants(matching: .any)["nowHeader"]
        XCTAssertTrue(now.waitForExistence(timeout: 5))
        let gap = now.frame.minY - title.frame.maxY
        shot("b1-title")
        XCTAssertGreaterThanOrEqual(gap, 0, "Now header overlaps the title")
        XCTAssertLessThanOrEqual(gap, 24, "gap under the title is \(gap) pt")
    }

    // MARK: Elements and steps

    private var composer: XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "placeholderValue BEGINSWITH 'Message'")).firstMatch
    }

    private var working: XCUIElement { app.descendants(matching: .any)["workingIndicator"] }

    /// "Thinking…" or a running tool's shimmering "Running Bash…" row.
    private var busy: XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(
            format: "identifier == 'workingIndicator' OR label BEGINSWITH 'Running '"
        )).firstMatch
    }

    private var viewer: XCUIElement { app.descendants(matching: .any)["fileViewer"] }

    private func element(_ id: String) -> XCUIElement { app.descendants(matching: .any)[id] }

    private func text(_ prefix: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH %@", prefix)).firstMatch
    }

    private func bubble(_ text: String) -> XCUIElement {
        app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier == 'userBubble' AND label CONTAINS %@", text)).firstMatch
    }

    private func fileRow(_ file: String, name: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(
            format: "identifier == 'toolStepFile' AND label BEGINSWITH %@ AND label CONTAINS %@", name, file
        )).firstMatch
    }

    private func send(_ text: String) {
        XCTAssertTrue(composer.waitForExistence(timeout: 10))
        composer.tap()
        composer.typeText(text)
        app.buttons["Send"].tap()
    }

    private func closeViewer() {
        let done = app.buttons["fileViewerDone"]
        XCTAssertTrue(done.waitForExistence(timeout: 3))
        done.tap()
        XCTAssertTrue(waitFor(viewer, "exists == false", timeout: 5), "file viewer didn't close")
    }

    /// Swipes the chat up, then down, until the element is hittable (the list is lazy).
    private func scrollChat(to element: XCUIElement) -> Bool {
        let low = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.7))
        let high = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.35))
        for (from, to) in Array(repeating: (high, low), count: 10) + Array(repeating: (low, high), count: 14) {
            if element.exists && element.isHittable { return true }
            from.press(forDuration: 0.05, thenDragTo: to)
        }
        return element.exists && element.isHittable
    }

    private func waitFor(_ element: XCUIElement, _ predicate: String, timeout: TimeInterval = 5) -> Bool {
        let e = expectation(for: NSPredicate(format: predicate), evaluatedWith: element)
        return XCTWaiter().wait(for: [e], timeout: timeout) == .completed
    }

    private func shot(_ name: String) {
        guard let dir = ProcessInfo.processInfo.environment["RELAY_SHOTS"] else { return }
        sleep(1)
        try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("round10-\(name).png"))
    }
}
