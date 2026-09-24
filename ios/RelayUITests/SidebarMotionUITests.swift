import XCTest

/// Opening and closing the sidebar by button, tap, edge swipe and drag. Also drives the recording in
/// docs/screenshots/sidebar-anim.mov.
@MainActor
final class SidebarMotionUITests: XCTestCase {
    func testOpenAndCloseEveryWay() throws {
        continueAfterFailure = false
        let app = XCUIApplication()
        app.launchArguments = ["-uitest", "-mock", "-replay", "off", "-resetSidebar", "-agent", "w2:p1"]
        app.launch()
        let open = app.buttons["Open sidebar"]
        XCTAssertTrue(open.waitForExistence(timeout: 10))
        let filter = app.buttons["sidebarFilter"]
        sleep(1)

        // Button, then a tap on the dimmed chat.
        open.tap()
        XCTAssertTrue(filter.waitForHittable(), "sidebar didn't open from the button")
        sleep(1)
        let window = app.windows.firstMatch
        window.coordinate(withNormalizedOffset: CGVector(dx: 0.95, dy: 0.5)).tap()
        XCTAssertTrue(filter.waitForNotHittable(), "tap on the chat didn't close it")
        sleep(1)

        // Edge swipe to open, following the finger.
        let edge = window.coordinate(withNormalizedOffset: CGVector(dx: 0.01, dy: 0.6))
        edge.press(forDuration: 0.05, thenDragTo: window.coordinate(withNormalizedOffset: CGVector(dx: 0.8, dy: 0.6)),
                   withVelocity: .slow, thenHoldForDuration: 0.1)
        XCTAssertTrue(filter.waitForHittable(), "edge swipe didn't open it")
        sleep(1)

        // Drag the chat back to close.
        let chat = window.coordinate(withNormalizedOffset: CGVector(dx: 0.93, dy: 0.6))
        chat.press(forDuration: 0.05, thenDragTo: window.coordinate(withNormalizedOffset: CGVector(dx: 0.1, dy: 0.6)),
                   withVelocity: .slow, thenHoldForDuration: 0.1)
        XCTAssertTrue(filter.waitForNotHittable(), "drag didn't close it")
        sleep(1)
    }
}

private extension XCUIElement {
    /// The sidebar is open when its filter button sits on screen (it slides off to the left when closed).
    /// Frames, not hittability: XCUITest can't evaluate hittability of an off-screen element.
    func waitForHittable(_ timeout: TimeInterval = 4) -> Bool { waitFor(timeout) { $0.exists && $0.frame.minX >= 0 } }
    func waitForNotHittable(_ timeout: TimeInterval = 4) -> Bool { waitFor(timeout) { !$0.exists || $0.frame.minX < 0 } }

    private func waitFor(_ timeout: TimeInterval, _ condition: (XCUIElement) -> Bool) -> Bool {
        let deadline = Date().addingTimeInterval(timeout)
        while Date() < deadline {
            if condition(self) { return true }
            usleep(100_000)
        }
        return false
    }
}
