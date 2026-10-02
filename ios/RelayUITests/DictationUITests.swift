import XCTest

/// Dictation against the mock backend, whose scripted engine "hears" words without a microphone.
@MainActor
final class DictationUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() async throws {
        continueAfterFailure = false
        app = XCUIApplication()
        app.launchArguments = ["-uitest", "-mock", "-replay", "off", "-resetSidebar", "-agent", "w2:p1"]
        app.launch()
    }

    private var composer: XCUIElement {
        app.descendants(matching: .any)["composerField"]
    }

    /// Mic → words stream into the draft after what was typed → stop; nothing is sent.
    func testDictationAppendsToTheDraftAndNeverSends() throws {
        XCTAssertTrue(composer.waitForExistence(timeout: 10))
        composer.tap()
        composer.typeText("Please")

        let mic = app.buttons["composerMic"]
        XCTAssertEqual(mic.label, "Start dictation")
        mic.tap()
        XCTAssertTrue(app.buttons["Stop dictation"].waitForExistence(timeout: 5), "no stop state while listening")
        let heard = NSPredicate(format: "value BEGINSWITH 'Please run'")
        expectation(for: heard, evaluatedWith: composer)
        waitForExpectations(timeout: 5)

        mic.tap()
        XCTAssertTrue(app.buttons["Start dictation"].waitForExistence(timeout: 5), "still listening after stop")
        sleep(1)
        let value = composer.value as? String ?? ""
        XCTAssertTrue(value.hasPrefix("Please run"), "draft lost: \(value)")
        XCTAssertFalse(value.hasSuffix("branch"), "kept listening after stop: \(value)")
        XCTAssertFalse(app.staticTexts[value].exists, "sent on its own")
    }
}
