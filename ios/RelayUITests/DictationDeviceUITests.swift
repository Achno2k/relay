import XCTest

/// The real mic on an iPhone: permission prompts, then listening and stop. Mock chats, real speech engine.
/// XCUITest can't speak; the transcript itself is covered by `DictationDeviceTests` with a recording.
@MainActor
final class DictationDeviceUITests: XCTestCase {
    func testMicAsksThenListensAndStops() throws {
        #if targetEnvironment(simulator)
        throw XCTSkip("needs a real microphone")
        #else
        continueAfterFailure = false
        let app = XCUIApplication()
        app.launchArguments = ["-uitest", "-mock", "-realDictation", "-replay", "off", "-resetSidebar", "-agent", "w2:p1"]
        app.launch()
        let springboard = XCUIApplication(bundleIdentifier: "com.apple.springboard")

        let mic = app.buttons["composerMic"]
        XCTAssertTrue(mic.waitForExistence(timeout: 15))
        XCTAssertEqual(mic.label, "Start dictation")
        mic.tap()

        // Microphone, then speech recognition, on first run; the model may download before it listens.
        let deadline = Date().addingTimeInterval(240)
        var states: [String] = []
        while Date() < deadline, mic.value as? String != "Listening" {
            let alert = springboard.alerts.firstMatch
            if alert.exists {
                let allow = ["Allow", "OK", "Allow Full Access"].map { alert.buttons[$0] }.first { $0.exists }
                (allow ?? alert.buttons.element(boundBy: alert.buttons.count - 1)).tap()
            }
            if let state = mic.value as? String, states.last != state { states.append(state) }
            if app.otherElements["dictationNotice"].exists, mic.label == "Start dictation" {
                XCTFail("dictation didn't start: \(app.otherElements["dictationNotice"].staticTexts.firstMatch.label)")
            }
            usleep(300_000)
        }
        print("dictation-device-ui: states \(states)")
        XCTAssertEqual(mic.value as? String, "Listening")
        XCTAssertEqual(mic.label, "Stop dictation")
        let shot = XCTAttachment(screenshot: app.screenshot())
        shot.name = "listening"
        shot.lifetime = .keepAlways
        add(shot)
        sleep(2)

        mic.tap()
        let idle = NSPredicate(format: "label == 'Start dictation'")
        expectation(for: idle, evaluatedWith: mic)
        waitForExpectations(timeout: 10)
        XCTAssertFalse(app.otherElements["dictationNotice"].exists, "an error after stop")
        #endif
    }
}
