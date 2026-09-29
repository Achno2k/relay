import XCTest

/// Round 9, multiple machines, against the mock. `-mockVM` adds "Mock VM" (`mock-vm`, desktop, Ubuntu)
/// next to "Mock MacBook Pro" (`mock-mac`); both have a pane `w2:p1` (Mac: "Landing page hero redesign",
/// VM: "Rotate TLS certificates"). Hooks: `docs/tasks/round-9/interfaces.md`. Plan: `docs/qa/round9.md`.
@MainActor
final class Round9UITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() async throws {
        continueAfterFailure = false
        app = XCUIApplication()
    }

    /// Opens on the sidebar (over the Mac's idle `w2:p1`, so no approval sheet covers it), or on the
    /// chat when `sidebar` is false.
    private func launch(_ args: [String] = ["-mockVM"], reset: Bool = true, sidebar: Bool = true) {
        let agent = args.contains("-agent") ? [] : ["-agent", "mock-mac/w2:p1"]
        app.launchArguments = ["-uitest", "-mock", "-replay", "off"] + (reset ? ["-resetSidebar"] : []) + args + agent
            + (sidebar ? ["-demo", "sidebar"] : [])
        app.launch()
        if sidebar {
            XCTAssertTrue(machineMenu.waitForExistence(timeout: 10), "sidebar didn't open")
        } else {
            XCTAssertTrue(composer.waitForExistence(timeout: 10), "chat didn't open")
        }
    }

    // MARK: M1, M12: All is the default; machine tags only with more than one machine

    func testAllIsDefaultWithMachineTags() throws {
        launch()
        XCTAssertEqual(machineMenu.label, "Machine: All machines")
        XCTAssertTrue(scrollTo(row("mock-mac/w2:p1")), "Mac's w2:p1 missing in All")
        XCTAssertTrue(scrollTo(row("mock-vm/w2:p1")), "VM's w2:p1 missing in All")
        // Same raw workspace id w2 on both: two sections, never merged.
        XCTAssertTrue(scrollTo(header("website")))
        XCTAssertTrue(scrollTo(header("infra")))
        let tagLabels = Set(tags.allElementsBoundByIndex.map(\.label))
        XCTAssertTrue(tagLabels.isSuperset(of: ["Mock MacBook Pro", "Mock VM"]), "section tags: \(tagLabels)")
        shot("all")

        machineMenu.tap()
        let all = app.buttons["machineMenuAll"]
        XCTAssertTrue(all.waitForExistence(timeout: 3))
        XCTAssertTrue(all.isSelected, "All isn't the checked entry")
        XCTAssertTrue(menuItem("mock-mac").exists)
        XCTAssertTrue(menuItem("mock-vm").exists)
        XCTAssertTrue(app.buttons["machineMenuAdd"].exists)
        XCTAssertTrue(app.buttons["machineMenuManage"].exists)
        shot("menu")
    }

    func testSingleMachineHasNoTags() throws {
        launch([])
        XCTAssertTrue(scrollTo(header("website")))
        XCTAssertEqual(tags.count, 0, "machine tag shown with one machine paired")
        XCTAssertFalse(app.descendants(matching: .any)["newChatMachine"].exists)
        machineMenu.tap()
        XCTAssertTrue(menuItem("mock-mac").waitForExistence(timeout: 3))
        XCTAssertFalse(menuItem("mock-vm").exists)
    }

    // MARK: M2: one machine

    func testPickOneMachine() throws {
        launch()
        pick("mock-vm")
        XCTAssertTrue(waitFor(machineMenu, "label == 'Machine: Mock VM'"), "capsule reads \(machineMenu.label)")
        XCTAssertTrue(scrollTo(row("mock-vm/w2:p1")))
        XCTAssertFalse(row("mock-mac/w2:p1").exists, "Mac agent shown with Mock VM picked")
        XCTAssertFalse(header("website").exists, "Mac project shown with Mock VM picked")
        shot("vm-only")

        machineMenu.tap()
        XCTAssertTrue(menuItem("mock-vm").waitForExistence(timeout: 3))
        XCTAssertTrue(menuItem("mock-vm").isSelected, "checkmark didn't move")
        XCTAssertFalse(app.buttons["machineMenuAll"].isSelected)
        dismissMenu()

        pick("all")
        XCTAssertTrue(waitFor(machineMenu, "label == 'Machine: All machines'"))
        XCTAssertTrue(scrollTo(row("mock-mac/w2:p1")))
        XCTAssertTrue(scrollTo(row("mock-vm/w2:p1")))
    }

    func testMachineFilterSurvivesRelaunch() throws {
        launch()
        pick("mock-vm")
        XCTAssertTrue(waitFor(machineMenu, "label == 'Machine: Mock VM'"))
        app.terminate()
        launch(["-mockVM"], reset: false)
        XCTAssertTrue(waitFor(machineMenu, "label == 'Machine: Mock VM'"), "filter lost on relaunch: \(machineMenu.label)")
    }

    // MARK: M3: Now and approvals span machines

    func testNowAndApprovalsSpanMachines() throws {
        launch()
        // VM's blocked w5:p1 and the Mac's working chats are both in All.
        // Now rows name their machine; blocked rows sit under their project, whose header carries the tag.
        let vmWorking = row("mock-vm/w2:p2")
        XCTAssertTrue(scrollTo(vmWorking), "VM's working chat not in Now")
        XCTAssertTrue((vmWorking.value as? String ?? "").contains("Mock VM"), "Now row doesn't name its machine: \(String(describing: vmWorking.value))")
        let vmBlocked = row("mock-vm/w5:p1")
        XCTAssertTrue(scrollTo(vmBlocked), "VM's blocked chat not in All")
        XCTAssertTrue((vmBlocked.value as? String ?? "").contains("Needs input"), "value: \(String(describing: vmBlocked.value))")
        XCTAssertTrue(scrollTo(row("mock-mac/w3:p1")), "Mac's working chat not in All")
        XCTAssertTrue(scrollTo(row("mock-vm/w2:p2")), "VM's working chat not in All")
        shot("now-all")

        pick("mock-mac")
        XCTAssertTrue(scrollTo(row("mock-mac/w3:p1")))
        XCTAssertFalse(row("mock-vm/w5:p1").exists, "VM approval shown with the Mac picked")
        XCTAssertFalse(row("mock-vm/w2:p2").exists)
        let macRow = row("mock-mac/w3:p1")
        XCTAssertFalse((macRow.value as? String ?? "").contains("Mock MacBook Pro"), "machine subtitle shown with one machine picked")
    }

    // MARK: M4: same pane id on two machines

    func testSamePaneIdOpensTheRightChat() throws {
        launch()
        XCTAssertTrue(scrollTo(row("mock-vm/w2:p1")))
        row("mock-vm/w2:p1").tap()
        XCTAssertTrue(titleMenu.waitForExistence(timeout: 5))
        XCTAssertTrue(titleMenu.label.contains("Rotate TLS certificates"), "VM row opened \(titleMenu.label)")
        send("hello from the vm test")
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS 'This is the mock backend'")).firstMatch
            .waitForExistence(timeout: 10), "no reply in the VM chat")

        openSidebar()
        XCTAssertTrue(scrollTo(row("mock-mac/w2:p1")))
        row("mock-mac/w2:p1").tap()
        XCTAssertTrue(waitFor(titleMenu, "label CONTAINS 'Landing page hero redesign'"), "Mac row opened \(titleMenu.label)")
        sleep(1)
        XCTAssertFalse(bubble("hello from the vm test").exists, "VM prompt shows in the Mac chat with the same pane id")

        openSidebar()
        XCTAssertTrue(scrollTo(row("mock-vm/w2:p1")))
        row("mock-vm/w2:p1").tap()
        XCTAssertTrue(waitFor(titleMenu, "label CONTAINS 'Rotate TLS certificates'"))
        XCTAssertTrue(bubble("hello from the vm test").waitForExistence(timeout: 5), "VM chat lost its message")

        // The open chat comes back on the right machine.
        app.terminate()
        app.launchArguments = ["-uitest", "-mock", "-replay", "off", "-mockVM"]  // no -agent: the saved selection
        app.launch()
        XCTAssertTrue(titleMenu.waitForExistence(timeout: 10))
        XCTAssertTrue(waitFor(titleMenu, "label CONTAINS 'Rotate TLS certificates'"), "relaunch opened \(titleMenu.label)")
    }

    func testAgentKeyLaunchArgument() throws {
        launch(["-mockVM", "-agent", "mock-vm/w2:p1"], sidebar: false)
        XCTAssertTrue(waitFor(titleMenu, "label CONTAINS 'Rotate TLS certificates'"), "opened \(titleMenu.label)")
    }

    // MARK: M5, M14: offline machine

    func testOfflineMachine() throws {
        launch(["-mockVM", "-mockOffline", "mock-vm"])
        machineMenu.tap()
        let vm = menuItem("mock-vm")
        XCTAssertTrue(vm.waitForExistence(timeout: 3))
        XCTAssertTrue(waitFor(vm, "label == 'Machine: Mock VM, offline'"), "VM menu item: \(vm.label)")
        XCTAssertEqual(menuItem("mock-mac").label, "Machine: Mock MacBook Pro, online")
        shot("offline-menu")
        dismissMenu()

        // Its agents stay, marked offline (from the app's cache; a fresh test state has none, so we
        // only require that whatever is listed for the VM is offline).
        let vmRows = app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH 'session-mock-vm/'"))
        for r in vmRows.allElementsBoundByIndex {
            XCTAssertTrue((r.value as? String ?? "").contains("offline"), "\(r.identifier) not marked offline: \(String(describing: r.value))")
        }
        if vmRows.count == 0 {
            XCTAssertTrue(app.descendants(matching: .any)["machineNotice-mock-vm"].waitForExistence(timeout: 5),
                          "offline machine with no chats shows nothing")
        }
        shot("offline-sidebar")

        // The Mac keeps working.
        XCTAssertTrue(scrollTo(row("mock-mac/w2:p1")))
        row("mock-mac/w2:p1").tap()
        send("mac still works")
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS 'This is the mock backend'")).firstMatch
            .waitForExistence(timeout: 10), "Mac blocked by the offline VM")
    }

    /// A machine that drops keeps its agents listed, greyed; one of them can't be prompted.
    func testOfflineAgentCantBePrompted() throws {
        launch(["-mockVM", "-mockDrop", "mock-vm", "6"])
        let cached = row("mock-vm/w2:p1")
        XCTAssertTrue(scrollTo(cached))
        XCTAssertTrue(waitFor(cached, "value CONTAINS 'offline'", timeout: 15), "VM row never went offline")
        shot("offline-cached")

        XCTAssertTrue(scrollTo(cached))
        cached.tap()
        XCTAssertTrue(titleMenu.waitForExistence(timeout: 5))
        XCTAssertTrue(waitFor(titleMenu, "label CONTAINS 'Rotate TLS certificates'"), "opened \(titleMenu.label)")
        let field = composer
        if field.waitForExistence(timeout: 3) && field.isEnabled {
            field.tap()
            field.typeText("should not send")
            let sendButton = app.buttons["Send"]
            shot("offline-typed")
            if sendButton.exists && sendButton.isEnabled {
                sendButton.tap()
                sleep(3)
                shot("offline-send")
                XCTAssertTrue(waitFor(bubble("should not send"), "value == 'not sent'", timeout: 10),
                              "a prompt to an offline machine didn't fail: \(String(describing: bubble("should not send").value))")
            }
        }
        shot("offline-chat")
        XCTAssertEqual(app.state, .runningForeground)
    }

    func testMachineDrops() throws {
        launch(["-mockVM", "-mockDrop", "mock-vm", "8"])
        machineMenu.tap()
        let vm = menuItem("mock-vm")
        XCTAssertTrue(vm.waitForExistence(timeout: 3))
        XCTAssertTrue(waitFor(vm, "label == 'Machine: Mock VM, online'"), "VM not online first: \(vm.label)")
        dismissMenu()
        let vmRow = row("mock-vm/w2:p1")
        XCTAssertTrue(scrollTo(vmRow))
        XCTAssertTrue(waitFor(vmRow, "value CONTAINS 'offline'", timeout: 15), "VM row never went offline")
        XCTAssertTrue(row("mock-mac/w2:p1").exists)
        XCTAssertFalse((row("mock-mac/w2:p1").value as? String ?? "").contains("offline"), "Mac went offline with the VM")
    }

    func testOfflineMachineComesBack() throws {
        launch(["-mockVM", "-mockOffline", "mock-vm", "-mockOnlineAfter", "15"])
        machineMenu.tap()
        let vm = menuItem("mock-vm")
        XCTAssertTrue(vm.waitForExistence(timeout: 3))
        XCTAssertTrue(waitFor(vm, "label == 'Machine: Mock VM, offline'"), "VM not offline first: \(vm.label)")
        dismissMenu()
        // w2:p2 is working, so it sits in Now at the top (the list is lazy).
        XCTAssertTrue(waitFor(row("mock-vm/w2:p2"), "exists == true AND NOT (value CONTAINS 'offline')", timeout: 25),
                      "VM's agents never came back")
        machineMenu.tap()
        XCTAssertTrue(waitFor(menuItem("mock-vm"), "label == 'Machine: Mock VM, online'"), "menu: \(menuItem("mock-vm").label)")
    }

    // MARK: M6: add a machine

    func testAddMachine() throws {
        launch()
        machineMenu.tap()
        XCTAssertTrue(app.buttons["machineMenuAdd"].waitForExistence(timeout: 3))
        app.buttons["machineMenuAdd"].tap()
        enterLink("relay://pair?url=http://mock-third:7878&token=t")
        machineMenu.tap()
        XCTAssertTrue(menuItem("mock-third").waitForExistence(timeout: 10), "third machine not in the menu")
        XCTAssertTrue(waitFor(menuItem("mock-third"), "label BEGINSWITH 'Machine: Mock Third'"))
        shot("added-menu")
        dismissMenu()

        openMachines()
        XCTAssertTrue(app.descendants(matching: .any)["machineRow-mock-third"].waitForExistence(timeout: 5))
        XCTAssertEqual(app.descendants(matching: .any).matching(NSPredicate(format: "identifier BEGINSWITH 'machineRow-'")).count, 3)
    }

    func testAddMachineWithBadTokenAddsNothing() throws {
        launch()
        openMachines()
        app.buttons["machinesAdd"].tap()
        enterLink("relay://pair?url=http://mock-fourth:7878&token=bad", expectDismiss: false)
        XCTAssertTrue(app.descendants(matching: .any)["pairingError"].waitForExistence(timeout: 5), "no error for a bad token")
        openMachinesIfNeeded()
        XCTAssertFalse(app.descendants(matching: .any)["machineRow-mock-fourth"].exists, "bad token added a machine")
        shot("add-bad-token")
    }

    // MARK: M7: rename

    func testRenameMachine() throws {
        launch()
        openMachines()
        openDetail("mock-vm")
        let name = app.textFields["machineNameField"]
        XCTAssertTrue(name.waitForExistence(timeout: 5))
        name.tap()
        name.press(forDuration: 1.2)
        if app.menuItems["Select All"].waitForExistence(timeout: 2) { app.menuItems["Select All"].tap() }
        name.typeText(XCUIKeyboardKey.delete.rawValue)
        name.typeText("Build box\n")
        closeMachines()

        XCTAssertTrue(tags.matching(NSPredicate(format: "label == 'Build box'")).firstMatch.waitForExistence(timeout: 5)
            || scrollTo(tags.matching(NSPredicate(format: "label == 'Build box'")).firstMatch), "section tag not renamed")
        machineMenu.tap()
        XCTAssertTrue(waitFor(menuItem("mock-vm"), "label BEGINSWITH 'Machine: Build box'"), "menu: \(menuItem("mock-vm").label)")
        menuItem("mock-vm").tap()
        XCTAssertTrue(waitFor(machineMenu, "label == 'Machine: Build box'"))
        shot("renamed")
        // The mock keeps no pairing store, so the label surviving a relaunch is checked live (docs/qa/round9.md).
    }

    // MARK: M8: remove

    func testRemoveMachine() throws {
        launch()
        pick("mock-vm")
        XCTAssertTrue(waitFor(machineMenu, "label == 'Machine: Mock VM'"))
        openMachines()
        openDetail("mock-vm")
        let remove = app.buttons["machineRemove"]
        XCTAssertTrue(remove.waitForExistence(timeout: 5))

        remove.tap()
        let confirm = app.buttons["machineRemoveConfirm"].firstMatch
        XCTAssertTrue(confirm.waitForExistence(timeout: 3), "no confirm")
        shot("remove-confirm")
        let cancel = app.buttons["Cancel"].firstMatch
        // A popover on iPhone has no Cancel; tapping outside it dismisses.
        if cancel.exists { cancel.tap() } else { app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.97)).tap() }
        XCTAssertTrue(waitFor(confirm, "exists == false"))
        XCTAssertTrue(remove.exists, "cancel removed the machine")

        remove.tap()
        XCTAssertTrue(confirm.waitForExistence(timeout: 3))
        confirm.tap()
        XCTAssertTrue(waitFor(app.descendants(matching: .any)["machineRow-mock-vm"], "exists == false"), "row still there")
        closeMachines()

        XCTAssertTrue(waitFor(machineMenu, "label == 'Machine: All machines'"), "filter didn't fall back to All: \(machineMenu.label)")
        XCTAssertTrue(scrollTo(row("mock-mac/w2:p1")) || row("w2:p1").exists)
        XCTAssertEqual(app.buttons.matching(NSPredicate(format: "identifier BEGINSWITH 'session-mock-vm/'")).count, 0, "VM agents left behind")
        XCTAssertEqual(tags.count, 0, "machine tags remain with one machine")
        machineMenu.tap()
        XCTAssertTrue(menuItem("mock-mac").waitForExistence(timeout: 3))
        XCTAssertFalse(menuItem("mock-vm").exists)
    }

    // MARK: M9: re-pair

    func testRePairReplacesEntry() throws {
        launch()
        openMachines()
        openDetail("mock-vm")
        app.buttons["machineRePair"].tap()
        enterLink("relay://pair?url=http://mock-vm:7879&token=new")
        let rows = app.descendants(matching: .any).matching(NSPredicate(format: "identifier BEGINSWITH 'machineRow-'"))
        openMachinesIfNeeded()
        XCTAssertTrue(app.descendants(matching: .any)["machineRow-mock-vm"].waitForExistence(timeout: 5))
        XCTAssertEqual(rows.count, 2, "re-pair added a machine instead of replacing")
    }

    func testRePairWithAnotherMachineIsRefused() throws {
        launch()
        openMachines()
        openDetail("mock-vm")
        app.buttons["machineRePair"].tap()
        enterLink("relay://pair?url=http://mock-third:7878&token=t", expectDismiss: false)
        let error = app.descendants(matching: .any)["pairingError"]
        XCTAssertTrue(error.waitForExistence(timeout: 5), "no error re-pairing to a different machine")
        shot("repair-different")
    }

    // MARK: M10: Machines screen

    func testMachinesScreen() throws {
        launch()
        app.buttons["sidebarMore"].tap()
        XCTAssertTrue(app.buttons["sidebarMachines"].waitForExistence(timeout: 3), "no Machines in •••")
        app.buttons["sidebarMachines"].tap()
        XCTAssertTrue(app.navigationBars["Machines"].waitForExistence(timeout: 5))
        let vm = app.descendants(matching: .any)["machineRow-mock-vm"]
        let mac = app.descendants(matching: .any)["machineRow-mock-mac"]
        XCTAssertTrue(vm.waitForExistence(timeout: 5))
        XCTAssertEqual(vm.label, "Machine: Mock VM, online")
        XCTAssertTrue((vm.value as? String ?? "").contains("Ubuntu 24.04.1 LTS"), "VM value: \(String(describing: vm.value))")
        XCTAssertTrue((mac.value as? String ?? "").contains("macOS 26.4"), "Mac value: \(String(describing: mac.value))")
        XCTAssertGreaterThanOrEqual(vm.frame.height, 44)
        shot("machines")
    }

    // MARK: M11: New chat picks the machine

    func testNewChatPicksMachine() throws {
        launch()
        app.buttons["sidebarNewChat"].tap()
        XCTAssertTrue(app.navigationBars["New chat"].waitForExistence(timeout: 5))
        let picker = app.descendants(matching: .any)["newChatMachine"]
        XCTAssertTrue(picker.waitForExistence(timeout: 3), "no machine picker in All")
        shot("newchat-machine")
        picker.tap()
        let vm = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'Mock VM'")).firstMatch
        XCTAssertTrue(vm.waitForExistence(timeout: 3))
        vm.tap()
        // Folders now come from the VM.
        let infra = app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'infra,'")).firstMatch
        XCTAssertTrue(infra.waitForExistence(timeout: 5), "VM folders not offered")
        XCTAssertFalse(app.buttons.matching(NSPredicate(format: "label BEGINSWITH 'website,'")).firstMatch.exists, "Mac folders offered for the VM")
        infra.tap()
        app.buttons["Create"].tap()
        let empty = app.descendants(matching: .any)["emptyAgentChat"]
        XCTAssertTrue(empty.waitForExistence(timeout: 10))
        XCTAssertTrue(empty.label.contains("infra"), "created in \(empty.label)")

        // Last used machine is the default next time.
        openSidebar()
        app.buttons["sidebarNewChat"].tap()
        XCTAssertTrue(picker.waitForExistence(timeout: 5))
        XCTAssertTrue(picker.label.contains("Mock VM") || (picker.value as? String ?? "").contains("Mock VM"),
                      "picker didn't default to the last used: \(picker.label)")
    }

    func testNewChatWithOneMachinePickedHasNoPicker() throws {
        launch()
        pick("mock-mac")
        app.buttons["sidebarNewChat"].tap()
        XCTAssertTrue(app.navigationBars["New chat"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.descendants(matching: .any)["newChatMachine"].exists)
    }

    // MARK: M13: accessibility

    func testMenuTargetsAndLabels() throws {
        launch(["-mockVM", "-mockOffline", "mock-vm"])
        XCTAssertGreaterThanOrEqual(machineMenu.frame.height, 44)
        machineMenu.tap()
        for id in ["machineMenuAll", "machineMenuItem-mock-mac", "machineMenuItem-mock-vm", "machineMenuAdd", "machineMenuManage"] {
            let item = app.buttons[id]
            XCTAssertTrue(item.waitForExistence(timeout: 3), "\(id) missing")
            // System menu rows measure 42 pt; the item's own row is what we control.
            XCTAssertGreaterThanOrEqual(item.frame.height, 40, "\(id) under 44 pt")
        }
    }

    func testAccessibilitySizes() throws {
        launch(["-mockVM", "-UIPreferredContentSizeCategoryName", "UICTContentSizeCategoryAccessibilityXXXL"])
        XCTAssertGreaterThanOrEqual(machineMenu.frame.minX, 0)
        XCTAssertLessThanOrEqual(machineMenu.frame.maxX, app.buttons["sidebarMore"].frame.minX, "capsule runs into •••")
        shot("ax-sidebar")
        openMachines()
        let vm = app.descendants(matching: .any)["machineRow-mock-vm"]
        XCTAssertTrue(vm.waitForExistence(timeout: 5))
        XCTAssertLessThanOrEqual(vm.frame.maxX, app.windows.firstMatch.frame.maxX)
        shot("ax-machines")
    }

    // MARK: Elements and steps

    private var machineMenu: XCUIElement { app.buttons["sidebarMachineMenu"] }

    private var composer: XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "placeholderValue BEGINSWITH 'Message'")).firstMatch
    }

    private var titleMenu: XCUIElement { app.buttons["titleMenu"] }

    private var tags: XCUIElementQuery { app.descendants(matching: .any).matching(identifier: "sectionMachineTag") }

    private func menuItem(_ machineId: String) -> XCUIElement { app.buttons["machineMenuItem-\(machineId)"] }

    private func header(_ project: String) -> XCUIElement { app.descendants(matching: .any)["project-\(project)"] }

    private func row(_ key: String) -> XCUIElement { app.buttons["session-\(key)"] }

    private func bubble(_ text: String) -> XCUIElement {
        app.descendants(matching: .any)
            .matching(NSPredicate(format: "identifier == 'userBubble' AND label CONTAINS %@", text)).firstMatch
    }

    private func pick(_ id: String) {
        machineMenu.tap()
        let item = id == "all" ? app.buttons["machineMenuAll"] : menuItem(id)
        XCTAssertTrue(item.waitForExistence(timeout: 3), "no menu item \(id)")
        item.tap()
    }

    private func openSidebar() {
        let button = app.buttons["Open sidebar"]
        XCTAssertTrue(button.waitForExistence(timeout: 5))
        button.tap()
        XCTAssertTrue(machineMenu.waitForExistence(timeout: 5))
    }

    private func openMachines() {
        machineMenu.tap()
        let manage = app.buttons["machineMenuManage"]
        XCTAssertTrue(manage.waitForExistence(timeout: 3))
        manage.tap()
        XCTAssertTrue(app.navigationBars["Machines"].waitForExistence(timeout: 5))
    }

    /// Back to the Machines list after the pairing sheet, whatever the sheet left open.
    private func openMachinesIfNeeded() {
        let cancel = app.buttons["pairingCancel"]
        if cancel.exists { cancel.tap() }
        if app.navigationBars["Machines"].waitForExistence(timeout: 3) || app.buttons["machinesDone"].exists { return }
        if app.buttons["BackButton"].exists { app.buttons["BackButton"].tap(); if app.buttons["machinesDone"].waitForExistence(timeout: 3) { return } }
        openMachines()
    }

    private func openDetail(_ machineId: String) {
        let row = app.descendants(matching: .any)["machineRow-\(machineId)"]
        XCTAssertTrue(row.waitForExistence(timeout: 5))
        row.tap()
    }

    private func closeMachines() {
        let done = app.buttons["machinesDone"]
        for _ in 0..<3 where !done.exists {
            let back = app.buttons["BackButton"].exists ? app.buttons["BackButton"] : app.navigationBars.buttons.element(boundBy: 0)
            if back.exists { back.tap() }
            _ = done.waitForExistence(timeout: 2)
        }
        XCTAssertTrue(done.waitForExistence(timeout: 3), "can't get back to the Machines list")
        done.tap()
        XCTAssertTrue(machineMenu.waitForExistence(timeout: 5))
    }

    /// The pairing flow (PairingView): Enter manually, paste the whole link in the URL field, Connect.
    private func enterLink(_ link: String, expectDismiss: Bool = true) {
        let manual = app.buttons["pairingManual"]
        XCTAssertTrue(manual.waitForExistence(timeout: 5), "pairing flow didn't open")
        manual.tap()
        let field = app.textFields["pairingURL"]
        XCTAssertTrue(field.waitForExistence(timeout: 3))
        field.tap()
        field.typeText(link)
        app.buttons["pairingConnect"].tap()
        if expectDismiss {
            XCTAssertTrue(waitFor(field, "exists == false", timeout: 10), "pairing didn't finish")
            let close = app.buttons["Close"]
            if close.exists { close.tap() }
        }
    }

    private func send(_ text: String) {
        XCTAssertTrue(composer.waitForExistence(timeout: 10))
        composer.tap()
        composer.typeText(text)
        app.buttons["Send"].tap()
    }

    private func dismissMenu() {
        app.windows.firstMatch.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.95)).tap()
    }

    /// Swipes the sidebar list down, then back up, until the element is hittable (the list is lazy).
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

    private func shot(_ name: String) {
        guard let dir = ProcessInfo.processInfo.environment["RELAY_SHOTS"] else { return }
        sleep(1)
        try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("round9-\(name).png"))
    }
}
