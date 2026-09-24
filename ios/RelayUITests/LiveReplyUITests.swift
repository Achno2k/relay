import XCTest

/// Drives the real app against a live bridge and a real agent, asking for a long, no-tool answer,
/// and asserts the growing `reply.live` preview shows up before the transcript message lands.
/// Skipped unless `RELAY_E2E_LINK` / `RELAY_E2E_AGENT` are set (same env as `LiveE2ETests`).
/// Also drives the recording in docs/screenshots/live-typing.mov.
@MainActor
final class LiveReplyUITests: XCTestCase {
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

    func testGrowingTextBeforeTranscriptMessageLands() throws {
        let isComposer = NSPredicate(format: "placeholderValue BEGINSWITH 'Message'")
        let composer = app.descendants(matching: .any).matching(isComposer).firstMatch
        XCTAssertTrue(composer.waitForExistence(timeout: 15), "composer never appeared")

        composer.tap()
        composer.typeText(
            "Write a 300-word explanation of how DNS resolution works end to end, no tools, no headers or lists, just flowing prose.")
        app.buttons["Send"].tap()

        let live = app.descendants(matching: .any)["liveReply"].firstMatch
        XCTAssertTrue(live.waitForExistence(timeout: 60), "no live preview appeared while the agent was working")
        let firstText = live.label
        XCTAssertFalse(firstText.isEmpty, "live preview appeared with no text")
        shot("1-live-growing")

        // The preview only guarantees the visible tail (it's OK to show only that, per api.md), so
        // its length isn't strictly monotonic across a paragraph break. What must hold: it keeps
        // changing while the agent works, i.e. this isn't a static placeholder.
        var sawADifferentText = false
        for _ in 0..<10 {
            usleep(500_000)
            let text = app.descendants(matching: .any)["liveReply"].firstMatch.label
            if !text.isEmpty, text != firstText {
                sawADifferentText = true
                break
            }
        }
        XCTAssertTrue(sawADifferentText, "live preview never changed after its first appearance (\(firstText.prefix(40))…)")
        shot("2-live-grown")

        // Once the turn finishes, the preview is gone and a real assistant text row stands in its place.
        XCTAssertTrue(app.buttons["Send"].waitForExistence(timeout: 90), "turn never finished")
        XCTAssertFalse(app.descendants(matching: .any)["liveReply"].firstMatch.exists, "live preview stuck around after the reply landed")
        sleep(1)
        shot("3-landed")
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
