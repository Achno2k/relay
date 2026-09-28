import XCTest

/// Accessibility audit of the screens past the sidebar: chat, approval sheet, New chat, Usage, at the
/// default size and at AX3. Opt-in (`TEST_RUNNER_RELAY_SHOTS=<dir>`): writes `a11y-screens.txt` and
/// a screenshot per screen there. It reports, it doesn't fail.
@MainActor
final class A11yAuditUITests: XCTestCase {
    private var app: XCUIApplication!
    private var report = ""

    override func setUp() async throws {
        guard ProcessInfo.processInfo.environment["RELAY_SHOTS"] != nil else { throw XCTSkip("RELAY_SHOTS not set") }
        continueAfterFailure = true
    }

    func testAuditScreens() throws {
        for size in ["default", "AX3"] {
            launch(agent: "w2:p1", size: size)
            XCTAssertTrue(app.staticTexts["Done"].waitForExistence(timeout: 10))
            audit("chat-\(size)")

            let plus = app.buttons["composerPlus"]
            plus.tap()
            _ = app.buttons["Files"].waitForExistence(timeout: 3)
            audit("plus-menu-\(size)")
            app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.3)).tap()

            app.buttons["titleMenu"].tap()
            sleep(1)
            audit("title-menu-\(size)")
            app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.95)).tap()
            sleep(1)

            app.buttons["New chat"].firstMatch.tap()
            sleep(2)
            audit("new-chat-\(size)")
            app.terminate()

            launch(agent: "w1:p2", size: size)
            _ = app.buttons.matching(NSPredicate(format: "label CONTAINS 'Type something.'")).firstMatch.waitForExistence(timeout: 10)
            audit("approval-\(size)")
            app.terminate()

            launch(agent: "w2:p1", size: size, extra: ["-demo", "usage"])
            sleep(3)
            audit("usage-\(size)")
            app.terminate()
        }
        let url = URL(fileURLWithPath: ProcessInfo.processInfo.environment["RELAY_SHOTS"]!).appendingPathComponent("a11y-screens.txt")
        try report.write(to: url, atomically: true, encoding: .utf8)
    }

    private func launch(agent: String, size: String, extra: [String] = []) {
        app = XCUIApplication()
        app.launchArguments = ["-uitest", "-mock", "-replay", "off", "-resetSidebar", "-agent", agent] + extra
        if size == "AX3" {
            app.launchArguments += ["-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryAccessibilityXL"]
        }
        app.launch()
    }

    private func audit(_ name: String) {
        let dir = ProcessInfo.processInfo.environment["RELAY_SHOTS"]!
        try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("a11y-\(name).png"))
        report += "\n## \(name)\n"
        try? app.performAccessibilityAudit { issue in
            self.report += "\(issue.auditType) · \(issue.compactDescription) · \(issue.element?.identifier ?? "") '\(issue.element?.label ?? "")'\n"
            return true
        }
    }
}
