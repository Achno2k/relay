import XCTest

/// Round 7 filter menu against the mock: the design's order and separator, the checkmark, the needs-input
/// dot, and that the choice is remembered. Round2UITests covers the flat lists and archive.
@MainActor
final class SidebarFilterMenuUITests: XCTestCase {
    private var app: XCUIApplication!
    private let order = ["All", "Needs input", "Ready for review", "Working", "Completed", "Archived"]

    override func setUp() async throws {
        continueAfterFailure = false
        app = XCUIApplication()
        app.launchArguments = ["-uitest", "-mock", "-replay", "off", "-resetSidebar", "-agent", "w2:p1"]
        app.launch()
    }

    func testMenuOrderCheckmarkAndMemory() throws {
        openSidebar()
        let filter = app.buttons["sidebarFilter"]
        XCTAssertEqual(filter.label, "Filter: All")
        XCTAssertEqual(filter.value as? String, "needs input", "dot while a chat is blocked")
        XCTAssertGreaterThanOrEqual(filter.frame.width, 44)
        XCTAssertGreaterThanOrEqual(filter.frame.height, 44)

        shot("round7-filter-button")

        filter.tap()
        let items = order.map { app.buttons[$0] }
        XCTAssertTrue(items[0].waitForExistence(timeout: 3))
        shot("round7-filter-menu")
        let tops = items.map(\.frame.minY)
        XCTAssertEqual(tops, tops.sorted(), "menu order is \(order.joined(separator: ", "))")
        let gaps = zip(tops.dropFirst(), tops).map { $0 - $1 }
        XCTAssertGreaterThan(gaps.last!, gaps.first! + 4, "a separator sits above Archived")
        XCTAssertTrue(items[0].isSelected, "All is checked")
        XCTAssertFalse(items[4].isSelected)

        items[4].tap()
        XCTAssertTrue(filter.waitForLabel("Filter: Completed"))
        filter.tap()
        XCTAssertTrue(app.buttons["Completed"].waitForExistence(timeout: 3))
        XCTAssertTrue(app.buttons["Completed"].isSelected, "the checkmark follows the choice")
        app.buttons["Completed"].tap()

        app.terminate()
        app.launchArguments.removeAll { $0 == "-resetSidebar" }
        app.launch()
        openSidebar()
        XCTAssertEqual(app.buttons["sidebarFilter"].label, "Filter: Completed", "remembered across launches")
    }

    private func openSidebar() {
        let open = app.buttons["Open sidebar"]
        XCTAssertTrue(open.waitForExistence(timeout: 10))
        open.tap()
        XCTAssertTrue(app.buttons["sidebarFilter"].waitForExistence(timeout: 5))
    }

    private func shot(_ name: String) {
        let a = XCTAttachment(screenshot: app.screenshot())
        a.name = name
        a.lifetime = .keepAlways
        add(a)
    }
}

private extension XCUIElement {
    func waitForLabel(_ label: String, timeout: TimeInterval = 3) -> Bool {
        let done = XCTNSPredicateExpectation(predicate: NSPredicate(format: "label == %@", label), object: self)
        return XCTWaiter.wait(for: [done], timeout: timeout) == .completed
    }
}
