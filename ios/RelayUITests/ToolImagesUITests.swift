import XCTest

/// Round 12: images in tool results. `w2:p1` (`MockChats.landing`) has a Read of `assets/hero.png` with one
/// image (`l-t3`) and a step with two screenshots (`l-t5`); the mock serves generated PNGs for both.
@MainActor
final class ToolImagesUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() async throws {
        continueAfterFailure = false
        app = XCUIApplication()
        app.launchArguments = ["-uitest", "-mock", "-replay", "off", "-resetSidebar", "-agent", "w2:p1", "-demo", "tools"]
        app.launch()
    }

    /// Thumbnail under the tool row → full-screen viewer (double-tap zoom, share) → swipe down closes it.
    func testThumbnailOpensViewer() throws {
        let hero = image(in: "Read assets/hero.png")
        XCTAssertTrue(hero.waitForExistence(timeout: 10), "no thumbnail under the image Read")
        XCTAssertTrue(scrollChat(to: hero), "thumbnail never came on screen")
        XCTAssertTrue(waitFor(hero, "value == 'loaded'", timeout: 5), "thumbnail never loaded")

        // Several images sit in one row; the shown images replace their "[image]" preview lines.
        let screens = app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier == 'toolImage' AND label BEGINSWITH 'Viewed 2 screenshots'"))
        XCTAssertEqual(screens.count, 2)
        let step = app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier == 'toolStep' AND label BEGINSWITH 'Viewed 2 screenshots'")).firstMatch
        step.tap()
        XCTAssertTrue(waitFor(step, "label CONTAINS 'Captured light and dark'"), "preview didn't open")
        XCTAssertFalse(step.label.contains("[image]"), "the [image] placeholder still shows next to the images")
        shot("thumbnails")

        // Bring the hero clear of the glass top bar before tapping it.
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.3))
            .press(forDuration: 0.05, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.55)))
        XCTAssertTrue(hero.isHittable)
        shot("hero")
        hero.tap()
        let full = app.descendants(matching: .any)["imageViewerImage"]
        XCTAssertTrue(full.waitForExistence(timeout: 5), "viewer didn't open")
        XCTAssertTrue(app.buttons["imageViewerShare"].exists, "no share button")
        XCTAssertTrue(app.buttons["imageViewerClose"].exists, "no close button")
        shot("viewer")

        full.doubleTap()
        XCTAssertTrue(waitFor(full, "value == 'zoomed'"), "double-tap didn't zoom")
        shot("viewer-zoomed")
        full.doubleTap()
        XCTAssertTrue(waitFor(full, "value == 'fit'"), "second double-tap didn't zoom back out")

        app.swipeDown(velocity: .fast)
        XCTAssertTrue(waitFor(full, "exists == false"), "swipe down didn't close the viewer")
        XCTAssertTrue(hero.waitForExistence(timeout: 3))
    }

    private func image(in title: String) -> XCUIElement {
        app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier == 'toolImage' AND label BEGINSWITH %@", title)).firstMatch
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

    /// Saved to `RELAY_SHOTS` when set (pass it as `TEST_RUNNER_RELAY_SHOTS`).
    private func shot(_ name: String) {
        guard let dir = ProcessInfo.processInfo.environment["RELAY_SHOTS"] else { return }
        sleep(1)
        try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("toolimages-\(name).png"))
    }
}
