import XCTest

/// Design QA, not a regression test: screenshots every sidebar state in dark and light, plus AX3 and an
/// accessibility audit, for comparison with the sidebar-b mockups. Skipped unless `RELAY_SHOTS` is set
/// (pass it as `TEST_RUNNER_RELAY_SHOTS`; shots are also kept as attachments); nothing here fails on design deltas.
@MainActor
final class DesignQAShots: XCTestCase {
    private var app: XCUIApplication!
    private var dir: URL!

    override func setUp() async throws {
        guard let path = ProcessInfo.processInfo.environment["RELAY_SHOTS"] else {
            throw XCTSkip("RELAY_SHOTS not set")
        }
        dir = URL(fileURLWithPath: path)
        continueAfterFailure = true
        app = XCUIApplication()
    }

    func testSidebarStatesDark() throws { try states(.dark, "dark") }
    func testSidebarStatesLight() throws { try states(.light, "light") }

    func testSidebarAX3() throws {
        XCUIDevice.shared.appearance = .light
        launch("-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryAccessibilityXL")
        shot("ax3-default")
        app.swipeUp()
        shot("ax3-scrolled")
    }

    func testSidebarAccessibility() throws {
        XCUIDevice.shared.appearance = .dark
        launch()
        // One snapshot; per-element queries take minutes on a full sidebar.
        var report = "# Element tree (VoiceOver order follows tree order)\n" + app.debugDescription
        report += "\n# performAccessibilityAudit\n"
        for pass in ["dark", "light"] {
            if pass == "light" { XCUIDevice.shared.appearance = .light; sleep(1) }
            try? app.performAccessibilityAudit { issue in
                report += "[\(pass)] \(issue.auditType) · \(issue.compactDescription) · \(issue.element?.identifier ?? "") '\(issue.element?.label ?? "")'\n"
                return true
            }
        }
        try? report.write(to: dir.appendingPathComponent("a11y.txt"), atomically: true, encoding: .utf8)
        keep(XCTAttachment(string: report), "a11y.txt")
    }

    // MARK: -

    private func states(_ appearance: XCUIDevice.Appearance, _ name: String) throws {
        XCUIDevice.shared.appearance = appearance
        launch()
        shot("\(name)-default")

        let showAll = app.buttons["nowShowAll"]
        if showAll.waitForExistence(timeout: 2) {
            showAll.tap()
            shot("\(name)-showall")
            showAll.tap()
        }

        // The list is lazy: the completed row only exists once scrolled into view.
        let completed = app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH 'completed-'")).firstMatch
        // "Hittable" is true under the bottom bar too, so scroll until the row clears it.
        let clear = app.windows.firstMatch.frame.maxY - 140
        for _ in 0..<4 where !(completed.exists && completed.frame.maxY < clear) { app.swipeUp() }
        if completed.exists {
            completed.tap()
            shot("\(name)-completed")
            completed.tap()
            app.swipeDown()
        }
        app.swipeUp()
        shot("\(name)-scrolled")
        app.swipeDown()

        let filter = app.buttons["sidebarFilter"]
        if filter.waitForExistence(timeout: 2) {
            filter.tap()
            _ = app.buttons["Needs input"].waitForExistence(timeout: 3)
            shot("\(name)-filter")
            app.buttons["Needs input"].tap()
            shot("\(name)-filter-needsinput")
            filter.tap()
            _ = app.buttons["All"].waitForExistence(timeout: 3)
            app.buttons["All"].tap()
        }

        let search = app.buttons["sidebarSearch"]
        if search.waitForExistence(timeout: 2) {
            search.tap()
            let field = app.textFields["sidebarSearchField"]
            if field.waitForExistence(timeout: 3) { field.typeText("tidy") }
            shot("\(name)-search")
        }
    }

    private func launch(_ extra: String...) {
        app.launchArguments = ["-uitest", "-mock", "-replay", "off", "-resetSidebar", "-demo", "sidebar", "-agent", "w2:p1"] + extra
        app.launch()
        _ = app.buttons["sidebarFilter"].waitForExistence(timeout: 10)
    }

    private func shot(_ name: String) {
        sleep(1)
        let screenshot = XCUIScreen.main.screenshot()
        try? screenshot.pngRepresentation.write(to: dir.appendingPathComponent("\(name).png"))
        keep(XCTAttachment(screenshot: screenshot), name)
    }

    /// Also in the .xcresult, since on a device `RELAY_SHOTS` is a host path the runner can't write to.
    private func keep(_ attachment: XCTAttachment, _ name: String) {
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }
}
