import XCTest

/// Round 8 app fixes, against the mock backend. Prompts starting "unsent" fail once there, like a
/// send while the bridge is unreachable.
@MainActor
final class Round8UITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() async throws {
        continueAfterFailure = false
        app = XCUIApplication()
        app.launchArguments = ["-uitest", "-mock", "-replay", "off", "-resetSidebar", "-agent", "w2:p4"]
        app.launch()
    }

    private var composer: XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "placeholderValue BEGINSWITH 'Message'")).firstMatch
    }

    private func bubble(_ text: String) -> XCUIElement {
        app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier == 'userBubble' AND label CONTAINS %@", text)).firstMatch
    }

    private func send(_ text: String) {
        XCTAssertTrue(composer.waitForExistence(timeout: 10))
        composer.tap()
        composer.typeText(text)
        app.buttons["Send"].tap()
    }

    /// R8-i6: a failed send says so and can be retried; the retry lands as the real message.
    func testFailedSendCanBeRetried() throws {
        send("unsent hello")
        let failed = bubble("unsent hello")
        XCTAssertTrue(failed.waitForExistence(timeout: 5))
        let notSent = expectation(for: NSPredicate(format: "value == 'not sent'"), evaluatedWith: failed)
        wait(for: [notSent], timeout: 5)
        let retry = app.buttons["retrySend"]
        XCTAssertTrue(retry.exists, "no retry on a failed send")
        shot("round8-failed-send")

        retry.tap()
        let sent = app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier == 'userBubble' AND label CONTAINS 'unsent hello' AND value == 'sent'")).firstMatch
        XCTAssertTrue(sent.waitForExistence(timeout: 10), "the retried prompt never landed")
        XCTAssertFalse(app.buttons["retrySend"].exists)
        XCTAssertEqual(
            app.descendants(matching: .any).matching(NSPredicate(format: "identifier == 'userBubble' AND label CONTAINS 'unsent hello'")).count, 1,
            "retry must not leave a duplicate bubble"
        )
    }

    /// R8-i6: a failed send can be deleted instead.
    func testFailedSendCanBeDeleted() throws {
        send("unsent goodbye")
        let failed = bubble("unsent goodbye")
        XCTAssertTrue(app.buttons["retrySend"].waitForExistence(timeout: 5))
        failed.press(forDuration: 1)
        let delete = app.buttons["Delete"]
        XCTAssertTrue(delete.waitForExistence(timeout: 5), "no Delete in the bubble's menu")
        delete.tap()
        let gone = expectation(for: NSPredicate(format: "exists == false"), evaluatedWith: failed)
        wait(for: [gone], timeout: 5)
    }

    /// R8-i7: at accessibility sizes the approval options wrap in full; none is cut off.
    func testApprovalOptionsAreNotClippedAtAX3() throws {
        app.terminate()
        app.launchArguments = [
            "-uitest", "-mock", "-replay", "off", "-resetSidebar", "-agent", "w1:p2",
            "-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryAccessibilityXL",
        ]
        app.launch()
        let option = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Yes, and don'")).firstMatch
        XCTAssertTrue(option.waitForExistence(timeout: 10), "sheet never appeared")
        sleep(1)
        shot("round8-approval-ax3")
        // "Yes, and don't ask again for npm test" needs three lines at this size; capped at two it read
        // "…again for n…". Measured against the one-line "Yes" (the audit's text-clipped check flags the
        // glass capsule either way).
        // Buttons expose no text children, so compare whole buttons: same padding, one line vs three
        // (about 2.1x the height; two lines would be about 1.5x).
        let yes = app.buttons["Yes"].frame.height
        let long = option.frame.height
        XCTAssertGreaterThan(yes, 0)
        XCTAssertGreaterThan(long, yes * 1.8, "the long option is cut to two lines (\(long) vs \(yes))")
    }

    private func shot(_ name: String) {
        guard let dir = ProcessInfo.processInfo.environment["RELAY_SHOTS"] else { return }
        sleep(1)
        try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("\(name).png"))
    }
}
