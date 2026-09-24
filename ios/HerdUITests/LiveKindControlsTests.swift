import XCTest

/// Model and effort on real pi and codex panes, through the title menu. Skipped unless the runner passes
/// `TEST_RUNNER_HERD_E2E_LINK` plus `TEST_RUNNER_HERD_E2E_PI_AGENT` and/or `TEST_RUNNER_HERD_E2E_CODEX_AGENT`.
/// Targets are read from the bridge (`GET /agents/:id/controls`), and the original values are put back.
@MainActor
final class LiveKindControlsTests: XCTestCase {
    private var env: [String: String] { ProcessInfo.processInfo.environment }
    private var app: XCUIApplication!

    override func setUp() async throws {
        continueAfterFailure = false
    }

    func testPiModelAndEffort() async throws {
        try await run(kindKey: "HERD_E2E_PI_AGENT", effortInSubtitle: true)
    }

    func testCodexModelAndEffort() async throws {
        try await run(kindKey: "HERD_E2E_CODEX_AGENT", effortInSubtitle: false)
    }

    private func run(kindKey: String, effortInSubtitle: Bool) async throws {
        guard let link = env["HERD_E2E_LINK"], let agentId = env[kindKey] else { throw XCTSkip("\(kindKey) not set") }
        let bridge = try Bridge(link: link, agentId: agentId)
        let original = try await bridge.agent()
        // Put the pane back as we found it, even when an assertion stops the test.
        addTeardownBlock {
            if let m = original.model { try? await bridge.control(["model": m]) }
            if let e = original.effort { try? await bridge.control(["effort": e]) }
        }
        let controls = try await bridge.controls()
        let model = try XCTUnwrap(controls.models.first { $0.id != original.model }, "only one model offered")

        app = XCUIApplication()
        app.launchArguments = ["-uitest", "-pair", link, "-agent", agentId]
        app.launch()
        let subtitle = app.staticTexts["titleSubtitle"]
        XCTAssertTrue(subtitle.waitForExistence(timeout: 20))
        waitForIdle()
        shot("kind-\(agentId)-1")

        // Model
        app.buttons["titleMenu"].tap()
        menuRow("Model").tap()
        let all = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'All models'")).firstMatch
        if all.waitForExistence(timeout: 2) {
            all.tap()
            let search = app.searchFields["Search models"]
            XCTAssertTrue(search.waitForExistence(timeout: 5))
            search.tap()
            search.typeText(model.label)
            let row = app.buttons["model-\(model.id)"]
            XCTAssertTrue(row.waitForExistence(timeout: 5), "\(model.id) not in the search results")
            row.tap()
        } else {
            let row = app.buttons[model.label]
            XCTAssertTrue(row.waitForExistence(timeout: 5), "\(model.label) not in the Model menu")
            row.tap()
        }
        waitForIdle(timeout: 45)
        XCTAssertTrue(subtitle.label.hasPrefix(model.label), "pill shows \(subtitle.label), expected \(model.label)")
        assertNoToast("model \(model.id)")
        var now = try await bridge.agent()
        XCTAssertEqual(now.model, model.id)
        shot("kind-\(agentId)-2-model")

        // Effort, from the list for the new model
        let efforts = try await bridge.controls().efforts
        let effort = try XCTUnwrap(efforts.first { $0.id != now.effort }, "only one effort for \(model.id)")
        app.buttons["titleMenu"].tap()
        menuRow("Effort").tap()
        let effortRow = app.buttons[effort.label]
        XCTAssertTrue(effortRow.waitForExistence(timeout: 5), "\(effort.label) not in the Effort menu")
        effortRow.tap()
        waitForIdle(timeout: 45)
        assertNoToast("effort \(effort.id)")
        now = try await bridge.agent()
        XCTAssertEqual(now.effort, effort.id)
        if effortInSubtitle {
            XCTAssertTrue(subtitle.label.hasSuffix(effort.label), "pill shows \(subtitle.label), expected \(effort.label)")
        }
        shot("kind-\(agentId)-3-effort")
    }

    // MARK: - Helpers

    private func menuRow(_ title: String) -> XCUIElement {
        let row = app.buttons.matching(NSPredicate(format: "label == %@ OR label BEGINSWITH %@", title, title + ",")).firstMatch
        XCTAssertTrue(row.waitForExistence(timeout: 5), "no \(title) row")
        return row
    }

    private func waitForIdle(timeout: TimeInterval = 15) {
        let idle = expectation(for: NSPredicate(format: "value == 'idle'"), evaluatedWith: app.buttons["titleMenu"])
        wait(for: [idle], timeout: timeout)
    }

    private func assertNoToast(_ what: String) {
        let toast = app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH \"Couldn't\"")).firstMatch
        XCTAssertFalse(toast.exists, "bridge refused \(what): \(toast.exists ? toast.label : "")")
    }

    private func shot(_ name: String) {
        let attachment = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
        if let dir = env["HERD_E2E_SHOTS"] {
            try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("\(name).png"))
        }
    }
}

/// Just enough of the bridge API for test setup and cleanup.
private struct Bridge: Sendable {
    struct Option: Decodable { var id: String; var label: String }
    struct Controls: Decodable { var models: [Option]; var efforts: [Option] }
    struct AgentFields: Decodable, Sendable { var model: String?; var effort: String? }

    let base: String
    let token: String
    let agentPath: String

    init(link: String, agentId: String) throws {
        let comps = try XCTUnwrap(URLComponents(string: link))
        base = try XCTUnwrap(comps.queryItems?.first { $0.name == "url" }?.value)
        token = try XCTUnwrap(comps.queryItems?.first { $0.name == "token" }?.value)
        agentPath = "/agents/" + agentId.replacingOccurrences(of: ":", with: "%3A")
    }

    func agent() async throws -> AgentFields { try await get(agentPath) }
    func controls() async throws -> Controls { try await get(agentPath + "/controls") }

    func control(_ body: [String: String]) async throws {
        var request = try request(agentPath + "/control")
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(body)
        request.timeoutInterval = 60
        _ = try await URLSession.shared.data(for: request)
    }

    private func get<T: Decodable>(_ path: String) async throws -> T {
        let (data, _) = try await URLSession.shared.data(for: try request(path))
        return try JSONDecoder().decode(T.self, from: data)
    }

    private func request(_ path: String) throws -> URLRequest {
        var request = URLRequest(url: try XCTUnwrap(URL(string: base + path)))
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        return request
    }
}
