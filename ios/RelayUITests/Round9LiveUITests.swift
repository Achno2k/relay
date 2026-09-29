import XCTest

/// Round 9 live: the Mac's bridge plus a second bridge on the same Mac posing as "Test VM"
/// (`scripts/second-bridge.sh`, port 7881, id `test-vm`). Each test states the bridge state it needs; the
/// runner sets it up (and may stop/start the 7881 bridge mid-test). Skipped unless
/// `TEST_RUNNER_RELAY_R9_MAC_LINK` and `TEST_RUNNER_RELAY_R9_VM_LINK` are set. Only the e2e agent
/// (`TEST_RUNNER_RELAY_R9_AGENT`, default `w14:p2`) is prompted. Plan: `docs/qa/round9.md`.
@MainActor
final class Round9LiveUITests: XCTestCase {
    private var app: XCUIApplication!
    private var env: [String: String] { ProcessInfo.processInfo.environment }
    private var macLink = ""
    private var vmLink = ""
    private var agent: String { env["RELAY_R9_AGENT"] ?? "w14:p2" }
    private let vm = "test-vm"

    override func setUp() async throws {
        continueAfterFailure = false
        guard let mac = env["RELAY_R9_MAC_LINK"], let vm = env["RELAY_R9_VM_LINK"] else {
            throw XCTSkip("RELAY_R9_MAC_LINK / RELAY_R9_VM_LINK not set")
        }
        macLink = mac
        vmLink = vm
        app = XCUIApplication()
    }

    // MARK: G1 + L1: migrate the old single pairing, then add the second machine

    /// Needs: both bridges up.
    func test1MigrateThenAddSecondMachine() async throws {
        let macId = try await machineId(macLink)
        launch(["-seedLegacyPairing", macLink])
        machineMenu.tap()
        let mac = menuItem(macId)
        XCTAssertTrue(mac.waitForExistence(timeout: 15), "the old pairing didn't come back under the Mac's /machine id")
        XCTAssertTrue(waitFor(mac, "label ENDSWITH ', online'", timeout: 15), "Mac: \(mac.label)")
        XCTAssertEqual(machineItems.count, 1, "migration left more than one machine")
        shot("live-migrated")

        XCTAssertTrue(app.buttons["machineMenuAdd"].exists)
        app.buttons["machineMenuAdd"].tap()
        enterLink(vmLink)
        machineMenu.tap()
        XCTAssertTrue(menuItem(vm).waitForExistence(timeout: 15), "Test VM not in the menu")
        XCTAssertTrue(waitFor(menuItem(vm), "label == 'Machine: Test VM, online'", timeout: 15), "VM: \(menuItem(vm).label)")
        XCTAssertTrue(waitFor(mac, "label ENDSWITH ', online'"))
        shot("live-two-machines")
        dismissMenu()

        // Same herdr behind both: the e2e agent shows once per machine, keyed apart.
        XCTAssertTrue(scrollTo(row("\(macId)/\(agent)")), "e2e agent missing under the Mac")
        XCTAssertTrue(scrollTo(row("\(vm)/\(agent)")), "e2e agent missing under Test VM")
        XCTAssertGreaterThanOrEqual(tags.count, 1, "no machine tags with two machines")
        shot("live-sidebar")
    }

    // MARK: L2: a prompt through Test VM goes to Test VM's bridge

    /// Needs: test1 state, both bridges up. The runner checks the 7881 log for the prompt.
    func test2PromptThroughTestVM() throws {
        let word = "R9VM" + String((0..<4).map { _ in "BCDFGHJKLMNPQRSTVWXZ".randomElement()! })
        launch(["-agent", "\(vm)/\(agent)"], sidebar: false)
        XCTAssertTrue(waitFor(app.buttons["titleMenu"], "exists == true", timeout: 15))
        send("Reply with exactly one word: \(word)")
        let reply = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@ AND NOT (label CONTAINS 'Reply with')", word)).firstMatch
        XCTAssertTrue(reply.waitForExistence(timeout: 120), "no reply through Test VM")
        shot("live-vm-reply")
        print("R9-LIVE-WORD \(word)")
    }

    // MARK: L3: Test VM goes offline and comes back; the Mac is untouched

    /// Needs: test1 state, 7881 stopped at launch; the runner starts it again ~30 s in.
    func test3VMOfflineThenBack() async throws {
        let macId = try await machineId(macLink)
        launch()
        machineMenu.tap()
        XCTAssertTrue(menuItem(vm).waitForExistence(timeout: 10))
        XCTAssertTrue(waitFor(menuItem(vm), "label == 'Machine: Test VM, offline'", timeout: 20), "VM: \(menuItem(vm).label)")
        XCTAssertTrue(waitFor(menuItem(macId), "label ENDSWITH ', online'", timeout: 10), "Mac: \(menuItem(macId).label)")
        shot("live-vm-offline")
        dismissMenu()
        XCTAssertTrue(scrollTo(row("\(macId)/\(agent)")), "Mac's agents gone while the VM is offline")

        // Comes back on its own once the runner restarts the bridge (reconnect backoff is up to 30 s).
        machineMenu.tap()
        XCTAssertTrue(waitFor(menuItem(vm), "label == 'Machine: Test VM, online'", timeout: 90), "VM never came back: \(menuItem(vm).label)")
        dismissMenu()
        let back = scrollTo(row("\(vm)/\(agent)"))
        if !back { dumpTree("live-vm-back-tree") }
        XCTAssertTrue(back, "VM agents didn't return")
        shot("live-vm-back")
    }

    // MARK: L6: the URL now reaches a different machine

    /// Needs: test1 state, 7881 restarted with the same token but `RELAY_MACHINE_ID=other-vm`.
    func test4ChangedIdNeedsRePair() async throws {
        let macId = try await machineId(macLink)
        launch()
        machineMenu.tap()
        XCTAssertTrue(menuItem(vm).waitForExistence(timeout: 10))
        XCTAssertTrue(waitFor(menuItem(vm), "label == 'Machine: Test VM, re-pair needed'", timeout: 30), "VM: \(menuItem(vm).label)")
        XCTAssertFalse(menuItem("other-vm").exists, "a changed id became a new machine")
        XCTAssertTrue(waitFor(menuItem(macId), "label ENDSWITH ', online'"), "Mac: \(menuItem(macId).label)")
        shot("live-changed-id")
    }

    // MARK: L4: token rotated; re-pair replaces the entry

    /// Needs: test1 state, 7881 back as `test-vm` with a NEW token; `RELAY_R9_VM_LINK` is the new link.
    func test5RotatedTokenThenRePair() throws {
        launch()
        machineMenu.tap()
        XCTAssertTrue(menuItem(vm).waitForExistence(timeout: 10))
        XCTAssertTrue(waitFor(menuItem(vm), "label == 'Machine: Test VM, re-pair needed'", timeout: 30), "VM: \(menuItem(vm).label)")
        shot("live-rotated")
        let row = app.descendants(matching: .any)["machineRow-\(vm)"]
        openMachines(row)
        row.tap()
        app.buttons["machineRePair"].tap()
        enterLink(vmLink)
        // Back on the machine's detail screen, now online.
        let online = app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS 'Online' OR value CONTAINS 'Online'")).firstMatch
        XCTAssertTrue(online.waitForExistence(timeout: 20), "re-pair didn't bring it online")
        shot("live-repaired-detail")
        if app.buttons["BackButton"].waitForExistence(timeout: 3) { app.buttons["BackButton"].tap() }
        let rows = app.descendants(matching: .any).matching(NSPredicate(format: "identifier BEGINSWITH 'machineRow-'"))
        XCTAssertTrue(waitFor(app.descendants(matching: .any)["machineRow-\(vm)"], "label == 'Machine: Test VM, online'", timeout: 20),
                      "VM after re-pair: \(app.descendants(matching: .any)["machineRow-\(vm)"].label)")
        XCTAssertEqual(rows.count, 2, "re-pair added a machine")
        shot("live-repaired")
    }

    // MARK: M7 live: a rename survives a relaunch

    /// Needs: test1 state, both bridges up.
    func test6RenameSurvivesRelaunch() throws {
        launch()
        let row = app.descendants(matching: .any)["machineRow-\(vm)"]
        machineMenu.tap()
        openMachines(row)
        row.tap()
        let name = app.textFields["machineNameField"]
        XCTAssertTrue(name.waitForExistence(timeout: 5))
        name.tap()
        name.press(forDuration: 1.2)
        if app.menuItems["Select All"].waitForExistence(timeout: 2) { app.menuItems["Select All"].tap() }
        name.typeText(XCUIKeyboardKey.delete.rawValue)
        name.typeText("Cloud box\n")
        if app.buttons["BackButton"].exists { app.buttons["BackButton"].tap() }
        XCTAssertTrue(app.buttons["machinesDone"].waitForExistence(timeout: 5))
        app.buttons["machinesDone"].tap()

        app.terminate()
        launch()
        machineMenu.tap()
        XCTAssertTrue(waitFor(menuItem(vm), "label BEGINSWITH 'Machine: Cloud box'", timeout: 10), "rename lost: \(menuItem(vm).label)")
        shot("live-renamed")

        // Back to the bridge's name for the next run.
        openMachines(app.descendants(matching: .any)["machineRow-\(vm)"])
        app.descendants(matching: .any)["machineRow-\(vm)"].tap()
        XCTAssertTrue(name.waitForExistence(timeout: 5))
        name.tap()
        name.press(forDuration: 1.2)
        if app.menuItems["Select All"].waitForExistence(timeout: 2) { app.menuItems["Select All"].tap() }
        name.typeText(XCUIKeyboardKey.delete.rawValue + "\n")
    }

    // MARK: G2: old pairing, bridge offline at first launch

    /// Needs: 7881 stopped at launch; the runner starts it (same token, `test-vm`) ~30 s in.
    func test7MigrateWhileOffline() throws {
        launch(["-seedLegacyPairing", vmLink])
        machineMenu.tap()
        XCTAssertTrue(waitFor(machineItems.firstMatch, "exists == true", timeout: 10), "no machine after migrating")
        XCTAssertEqual(machineItems.count, 1)
        XCTAssertTrue(waitFor(machineItems.firstMatch, "label ENDSWITH ', offline' OR label ENDSWITH ', connecting'", timeout: 20),
                      "provisional entry: \(machineItems.firstMatch.label)")
        shot("live-migrate-offline")
        dismissMenu()

        // Once the bridge answers, the entry is re-keyed to its real id, still one machine.
        let deadline = Date().addingTimeInterval(180)
        while !menuItem(vm).exists && Date() < deadline {
            machineMenu.tap()
            if menuItem(vm).waitForExistence(timeout: 3) { break }
            dismissMenu()
            sleep(5)
        }
        XCTAssertTrue(menuItem(vm).exists, "never re-keyed to test-vm")
        XCTAssertTrue(waitFor(menuItem(vm), "label == 'Machine: Test VM, online'", timeout: 20), "VM: \(menuItem(vm).label)")
        XCTAssertEqual(machineItems.count, 1, "re-keying left a duplicate")
        shot("live-migrate-rekeyed")
    }

    // MARK: L8: an attachment in a Test VM chat is uploaded to Test VM's bridge

    /// Needs: both bridges up. The runner counts files in each bridge's `uploads/` before and after.
    func test8AttachmentGoesToTestVM() throws {
        let word = "R9AT" + String((0..<4).map { _ in "BCDFGHJKLMNPQRSTVWXZ".randomElement()! })
        app.launchArguments = ["-uitest", "-pair", macLink, "-agent", "\(vm)/\(agent)", "-uitestAttachments", "-testWord", word]
        app.launch()
        XCTAssertTrue(composer.waitForExistence(timeout: 20), "composer never appeared")
        XCTAssertTrue(waitFor(app.buttons["titleMenu"], "exists == true", timeout: 15))
        app.buttons["composerPlus"].tap()
        let pdf = app.buttons["Test PDF"]
        XCTAssertTrue(pdf.waitForExistence(timeout: 5), "no Test PDF in the + menu")
        pdf.tap()
        let item = app.descendants(matching: .any).matching(identifier: "trayItem").firstMatch
        XCTAssertTrue(waitFor(item, "value == 'uploaded'", timeout: 30), "upload never finished")
        composer.tap()
        composer.typeText("What is the code word in the attached PDF? Reply with just the word.")
        app.buttons["Send"].tap()
        let reply = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", word)).firstMatch
        XCTAssertTrue(reply.waitForExistence(timeout: 120), "the agent never read \(word) through Test VM")
        shot("live-vm-attachment")
    }

    // MARK: Helpers

    private func launch(_ args: [String] = [], sidebar: Bool = true) {
        app.launchArguments = ["-uitest"] + args + (sidebar ? ["-demo", "sidebar"] : [])
        app.launch()
        if sidebar { XCTAssertTrue(machineMenu.waitForExistence(timeout: 15), "sidebar didn't open") }
    }

    private var machineMenu: XCUIElement { app.buttons["sidebarMachineMenu"] }
    private var machineItems: XCUIElementQuery { app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH 'machineMenuItem-'")) }
    private var tags: XCUIElementQuery { app.descendants(matching: .any).matching(identifier: "sectionMachineTag") }
    private func menuItem(_ id: String) -> XCUIElement { app.buttons["machineMenuItem-\(id)"] }
    private func row(_ key: String) -> XCUIElement { app.buttons["session-\(key)"] }

    private var composer: XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "placeholderValue BEGINSWITH 'Message'")).firstMatch
    }

    private func send(_ text: String) {
        XCTAssertTrue(composer.waitForExistence(timeout: 15))
        composer.tap()
        composer.typeText(text)
        app.buttons["Send"].tap()
    }

    /// Taps Manage machines… in the open machine menu (reopening it if a tap was lost) until `row` shows.
    private func openMachines(_ row: XCUIElement) {
        for _ in 0..<3 {
            let manage = app.buttons["machineMenuManage"]
            if !manage.waitForExistence(timeout: 3) { machineMenu.tap(); _ = manage.waitForExistence(timeout: 3) }
            if manage.exists { manage.tap() }
            if row.waitForExistence(timeout: 5) { return }
        }
        XCTFail("Machines screen didn't open")
    }

    private func enterLink(_ link: String) {
        let manual = app.buttons["pairingManual"]
        XCTAssertTrue(manual.waitForExistence(timeout: 5), "pairing flow didn't open")
        manual.tap()
        let field = app.textFields["pairingURL"]
        XCTAssertTrue(field.waitForExistence(timeout: 3))
        field.tap()
        field.typeText(link)
        app.buttons["pairingConnect"].tap()
        XCTAssertTrue(waitFor(field, "exists == false", timeout: 20), "pairing didn't finish")
    }

    /// `GET /machine` straight from the bridge, with the link's own url and token.
    private func machineId(_ link: String) async throws -> String {
        let comps = try XCTUnwrap(URLComponents(string: link))
        let base = try XCTUnwrap(comps.queryItems?.first { $0.name == "url" }?.value)
        let token = try XCTUnwrap(comps.queryItems?.first { $0.name == "token" }?.value)
        var request = URLRequest(url: try XCTUnwrap(URL(string: base + "/machine")))
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        let (data, _) = try await URLSession.shared.data(for: request)
        let json = try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: Any])
        return try XCTUnwrap(json["id"] as? String)
    }

    private func dismissMenu() {
        app.windows.firstMatch.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.95)).tap()
    }

    @discardableResult
    private func scrollTo(_ element: XCUIElement) -> Bool {
        let low = app.coordinate(withNormalizedOffset: CGVector(dx: 0.4, dy: 0.75))
        let high = app.coordinate(withNormalizedOffset: CGVector(dx: 0.4, dy: 0.45))
        for (from, to) in Array(repeating: (low, high), count: 8) + Array(repeating: (high, low), count: 12) {
            if element.exists && element.isHittable { return true }
            from.press(forDuration: 0.05, thenDragTo: to)
        }
        return element.exists && element.isHittable
    }

    private func waitFor(_ element: XCUIElement, _ predicate: String, timeout: TimeInterval = 5) -> Bool {
        let e = expectation(for: NSPredicate(format: predicate), evaluatedWith: element)
        return XCTWaiter().wait(for: [e], timeout: timeout) == .completed
    }

    private func dumpTree(_ name: String) {
        shot(name)
        guard let dir = env["RELAY_SHOTS"] else { return }
        try? app.debugDescription.write(toFile: "\(dir)/round9-\(name).txt", atomically: true, encoding: .utf8)
    }

    private func shot(_ name: String) {
        guard let dir = env["RELAY_SHOTS"] else { return }
        sleep(1)
        try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("round9-\(name).png"))
    }
}
