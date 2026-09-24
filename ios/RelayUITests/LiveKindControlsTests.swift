import XCTest

/// Model and effort on real pi and codex panes, through the title menu. Skipped unless the runner passes
/// `TEST_RUNNER_RELAY_E2E_LINK` plus `TEST_RUNNER_RELAY_E2E_PI_AGENT` and/or `TEST_RUNNER_RELAY_E2E_CODEX_AGENT`.
/// Targets are read from the bridge (`GET /agents/:id/controls`), and the original values are put back.
@MainActor
final class LiveKindControlsTests: XCTestCase {
    private var env: [String: String] { ProcessInfo.processInfo.environment }
    private var app: XCUIApplication!

    override func setUp() async throws {
        continueAfterFailure = false
    }

    func testPiModelAndEffort() async throws {
        try await run(kindKey: "RELAY_E2E_PI_AGENT", effortInSubtitle: true)
    }

    func testCodexModelAndEffort() async throws {
        try await run(kindKey: "RELAY_E2E_CODEX_AGENT", effortInSubtitle: false)
    }

    /// Codex in Ask mode asks before a network command: approve one run, cancel another.
    /// Never taps "don't ask again" (it would save a rule in codex). Each run uses a fresh URL, and the
    /// approved run must actually reach the network (an HTTP status line in the tool result).
    func testCodexApproval() async throws {
        guard let link = env["RELAY_E2E_LINK"], let agentId = env["RELAY_E2E_CODEX_AGENT"] else {
            throw XCTSkip("RELAY_E2E_CODEX_AGENT not set")
        }
        let bridge = try Bridge(link: link, agentId: agentId)
        let originalMode = try await bridge.agentMode()
        addTeardownBlock {
            if (try? await bridge.approvalStatus()) == 200 { try? await bridge.keys(["esc"]) }
            if let mode = originalMode { try? await bridge.control(["permissionMode": mode]) }
        }
        try await bridge.control(["permissionMode": "ask"])

        app = XCUIApplication()
        app.launchArguments = ["-uitest", "-pair", link, "-agent", agentId]
        app.launch()
        let composer = app.descendants(matching: .any).matching(NSPredicate(format: "placeholderValue BEGINSWITH 'Message'")).firstMatch
        XCTAssertTrue(composer.waitForExistence(timeout: 20))

        // Approve
        let approveURL = Self.freshURL()
        send(Self.prompt(approveURL), via: composer)
        let yes = app.buttons["Yes, proceed"]
        XCTAssertTrue(yes.waitForExistence(timeout: 90), "codex never asked")
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS 'Would you like to run'")).firstMatch.exists)
        shot("codex-approval-sheet")
        yes.tap()
        try await waitUntil(timeout: 90, "approval never cleared after Yes") { try await bridge.approvalStatus() == 204 }
        try await waitUntil(timeout: 120, "codex didn't finish after approval") { try await bridge.agentStatus() != "working" }
        let statusAfterYes = try await bridge.agentStatus()
        XCTAssertNotEqual(statusAfterYes, "blocked")
        try await checkTranscript(bridge: bridge, tag: approveURL)
        shot("codex-approval-approved")

        // Cancel
        XCTAssertTrue(app.buttons["Send"].waitForExistence(timeout: 30))
        try await waitUntil(timeout: 30, "first question still open") { try await bridge.approvalStatus() == 204 }
        let cancelURL = Self.freshURL()
        send(Self.prompt(cancelURL), via: composer)
        let no = app.buttons["No, and tell Codex what to do differently"]
        XCTAssertTrue(no.waitForExistence(timeout: 90), "codex never asked the second time")
        // Must be the second question, not the first one coming back.
        let tag = String(cancelURL.split(separator: "/").last ?? "")
        let question = app.staticTexts["approvalQuestion"]
        XCTAssertTrue(question.waitForExistence(timeout: 5))
        XCTAssertTrue(question.label.contains(tag), "the sheet shows \(question.label), not the second command")
        no.tap()
        try await waitUntil(timeout: 60, "approval never cleared after No") { try await bridge.approvalStatus() == 204 }
        try await waitUntil(timeout: 60, "codex still blocked after No") { try await bridge.agentStatus() != "blocked" }
        shot("codex-approval-cancelled")
    }

    /// Once codex has real transcripts: the prompt confirms (no pending bubble) and the curl call is a tool row.
    /// Without a transcript (screen-read fallback) there's nothing to check yet.
    private func checkTranscript(bridge: Bridge, tag url: String) async throws {
        guard try await bridge.hasTranscript() else {
            XCTContext.runActivity(named: "codex has no transcript yet: skipped the message checks") { _ in }
            return
        }
        let tag = String(url.split(separator: "/").last ?? "")
        let messages = try await bridge.messages()
        XCTAssertTrue(messages.contains { $0.role == "user" && $0.text.contains(tag) }, "prompt not in the transcript")
        XCTAssertTrue(messages.contains { $0.toolSummaries.contains { $0.contains(tag) } }, "curl call isn't a tool call")
        XCTAssertTrue(messages.contains { $0.toolPreviews.contains { $0.contains("HTTP/") } }, "the approved curl never reached the network")

        let bubble = app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier == 'userBubble' AND label CONTAINS %@", tag)).firstMatch
        XCTAssertTrue(bubble.waitForExistence(timeout: 20), "no bubble for the prompt")
        let confirmed = expectation(for: NSPredicate(format: "value == 'sent'"), evaluatedWith: bubble)
        await fulfillment(of: [confirmed], timeout: 30)
        let pending = app.descendants(matching: .any).matching(NSPredicate(format: "identifier == 'userBubble' AND value == 'pending'"))
        XCTAssertEqual(pending.count, 0, "a sent message never confirmed")
        let toolRow = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Used' OR label BEGINSWITH 'Worked for'")).firstMatch
        XCTAssertTrue(toolRow.waitForExistence(timeout: 10), "no tool row in the chat")
    }

    private static func freshURL() -> String {
        let tld = ["com", "org", "net"].randomElement()!
        let tag = String((0..<6).map { _ in "abcdefghjkmnpqrstuvwxyz23456789".randomElement()! })
        // A path, not a query: zsh treats `?` as a glob and the command would fail before curl ran.
        return "https://example.\(tld)/relay-\(tag)"
    }

    private static func prompt(_ url: String) -> String {
        "Run exactly this, requesting escalated permissions (network) so I can approve it: curl -sI \(url) | head -1"
    }

    private func send(_ text: String, via composer: XCUIElement) {
        composer.tap()
        composer.typeText(text)
        app.buttons["Send"].tap()
    }

    private func waitUntil(timeout: TimeInterval, _ message: String, _ condition: () async throws -> Bool) async throws {
        let deadline = Date().addingTimeInterval(timeout)
        while Date() < deadline {
            if (try? await condition()) == true { return }
            try await Task.sleep(for: .seconds(1))
        }
        XCTFail(message)
    }

    private func run(kindKey: String, effortInSubtitle: Bool) async throws {
        guard let link = env["RELAY_E2E_LINK"], let agentId = env[kindKey] else { throw XCTSkip("\(kindKey) not set") }
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
        if let dir = env["RELAY_E2E_SHOTS"] {
            try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("\(name).png"))
        }
    }
}

/// Just enough of the bridge API for test setup and cleanup.
private struct Bridge: Sendable {
    struct Option: Decodable { var id: String; var label: String }
    struct Controls: Decodable { var models: [Option]; var efforts: [Option] }
    struct AgentFields: Decodable, Sendable {
        var model: String?; var effort: String?; var status: String?; var permissionMode: String?; var hasTranscript: Bool?
    }
    struct Block: Decodable, Sendable { var type: String; var text: String?; var summary: String?; var preview: String? }
    struct MessageFields: Decodable, Sendable {
        var role: String
        var blocks: [Block]
        var text: String { blocks.compactMap(\.text).joined(separator: "\n") }
        var toolSummaries: [String] { blocks.filter { $0.type == "toolCall" }.compactMap(\.summary) }
        var toolPreviews: [String] { blocks.filter { $0.type == "toolResult" }.compactMap(\.preview) }
    }
    struct Page: Decodable, Sendable { var messages: [MessageFields] }

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
    func agentStatus() async throws -> String? { try await agent().status }
    func hasTranscript() async throws -> Bool { try await agent().hasTranscript ?? false }
    func messages() async throws -> [MessageFields] { try await (get(agentPath + "/messages?limit=50") as Page).messages }
    func agentMode() async throws -> String? { try await agent().permissionMode }

    func approvalStatus() async throws -> Int {
        let (_, response) = try await URLSession.shared.data(for: try request(agentPath + "/approval"))
        return (response as? HTTPURLResponse)?.statusCode ?? 0
    }

    func keys(_ keys: [String]) async throws {
        var request = try request(agentPath + "/keys")
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(["keys": keys])
        _ = try await URLSession.shared.data(for: request)
    }

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
