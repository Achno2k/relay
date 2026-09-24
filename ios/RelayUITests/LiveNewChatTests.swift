import XCTest

/// Starts a real agent from New chat with a non-default model and effort and no message, then checks the
/// empty state. Needs a bridge with `GET /controls?kind=` (`TEST_RUNNER_RELAY_E2E_LINK`) and an agent in the
/// throwaway workspace (`TEST_RUNNER_RELAY_E2E_AGENT`). The bridge has no route to close a pane, so the test
/// prints `RELAY_CREATED_AGENT=<id>` for cleanup.
@MainActor
final class LiveNewChatTests: XCTestCase {
    private var env: [String: String] { ProcessInfo.processInfo.environment }

    func testCreateClaudeWithModelAndEffortOpensEmptyChat() async throws {
        continueAfterFailure = false
        guard let link = env["RELAY_E2E_LINK"], let agentId = env["RELAY_E2E_AGENT"] else {
            throw XCTSkip("RELAY_E2E_LINK / RELAY_E2E_AGENT not set")
        }
        let api = try LiveAPI(link: link)
        let anchor: [String: AnyHashable] = try await api.json("/agents/" + agentId.replacingOccurrences(of: ":", with: "%3A"))
        let workspace = try XCTUnwrap(anchor["workspaceName"] as? String)
        let workspaceId = try XCTUnwrap(anchor["workspaceId"] as? String)
        let controls: [String: AnyHashable] = try await api.json("/controls?kind=claude")
        let models = try XCTUnwrap(controls["models"] as? [[String: String]])
        let efforts = try XCTUnwrap(controls["efforts"] as? [[String: String]])
        let model = try XCTUnwrap(models.first { $0["id"] != controls["defaultModel"] as? String })
        let effort = try XCTUnwrap(efforts.first { $0["id"] != controls["defaultEffort"] as? String })
        let before = Set(try await api.agentIds(in: workspaceId))

        let app = XCUIApplication()
        app.launchArguments = ["-uitest", "-pair", link, "-agent", agentId]
        app.launch()
        let newChat = app.navigationBars.buttons["New chat"].firstMatch
        XCTAssertTrue(newChat.waitForExistence(timeout: 20))
        newChat.tap()
        let folder = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", workspace + ",")).firstMatch
        XCTAssertTrue(folder.waitForExistence(timeout: 5), "no \(workspace) folder in New chat")
        folder.tap()
        app.buttons["Claude"].tap()
        let modelPicker = app.buttons["newChatModel"]
        XCTAssertTrue(modelPicker.waitForExistence(timeout: 10))
        modelPicker.tap()
        app.buttons[model["label"]!].tap()
        app.buttons["newChatEffort"].tap()
        app.buttons[effort["label"]!].tap()
        app.buttons["Create"].tap()
        sleep(2)
        let afterCreate = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
        afterCreate.name = "after-create"
        afterCreate.lifetime = .keepAlways
        add(afterCreate)

        let empty = app.descendants(matching: .any)["emptyAgentChat"]
        XCTAssertTrue(empty.waitForExistence(timeout: 60), "no empty state for the new agent")
        XCTAssertTrue(empty.label.contains("New Claude chat in \(workspace)"), "empty state reads \(empty.label)")
        XCTAssertFalse(app.descendants(matching: .any)["liveScreen"].exists, "a screen dump instead of the empty state")
        let shot = XCTAttachment(screenshot: XCUIScreen.main.screenshot())
        shot.lifetime = .keepAlways
        add(shot)

        let after = try await api.agentIds(in: workspaceId)
        let created = try XCTUnwrap(after.first { !before.contains($0) }, "no new agent")
        print("RELAY_CREATED_AGENT=\(created)")
        let agent: [String: AnyHashable] = try await api.json("/agents/" + created.replacingOccurrences(of: ":", with: "%3A"))
        let startedModel = agent["model"] as? String ?? ""
        XCTAssertTrue(startedModel.contains(model["id"]!), "started on \(startedModel)")
        XCTAssertEqual(agent["effort"] as? String, effort["id"])
        XCTAssertEqual(agent["transcriptState"] as? String, "pending")
        let page: [String: AnyHashable] = try await api.json("/agents/" + created.replacingOccurrences(of: ":", with: "%3A") + "/messages")
        XCTAssertEqual((page["messages"] as? [AnyHashable])?.count, 0, "a new agent's chat should be empty")
        XCTAssertTrue(empty.label.contains(agent["modelLabel"] as? String ?? "?"), "pill model \(empty.label)")
    }
}

private struct LiveAPI {
    let base: String
    let token: String

    init(link: String) throws {
        let comps = try XCTUnwrap(URLComponents(string: link))
        base = try XCTUnwrap(comps.queryItems?.first { $0.name == "url" }?.value)
        token = try XCTUnwrap(comps.queryItems?.first { $0.name == "token" }?.value)
    }

    func json<T>(_ path: String) async throws -> T {
        var request = URLRequest(url: try XCTUnwrap(URL(string: base + path)))
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        let (data, _) = try await URLSession.shared.data(for: request)
        return try XCTUnwrap(try JSONSerialization.jsonObject(with: data) as? T)
    }

    func agentIds(in workspaceId: String) async throws -> [String] {
        let agents: [[String: AnyHashable]] = try await json("/agents")
        return agents.filter { $0["workspaceId"] as? String == workspaceId }.compactMap { $0["id"] as? String }
    }
}
