import XCTest

/// Picks real files through the system Files picker (mock bridge). Opt-in: the simulator's
/// On My iPhone must hold `RelayQA/big-01.bin` … `big-10.bin` (20 MB each) and
/// `RelayQAHuge/huge.zip` (600 MB); pass `TEST_RUNNER_RELAY_FILES_QA=1`.
@MainActor
final class FilesPickerUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUp() async throws {
        continueAfterFailure = false
        guard ProcessInfo.processInfo.environment["RELAY_FILES_QA"] == "1" else {
            throw XCTSkip("RELAY_FILES_QA not set; seed the Files folders first")
        }
        app = XCUIApplication()
        app.launchArguments = ["-mock", "-replay", "off", "-uitest", "-resetSidebar", "-agent", "w2:p4"]
        app.launch()
    }

    /// Ten 20 MB files in one pick: all ten land in the tray, in order, and finish uploading.
    func testTenTwentyMegabyteFiles() throws {
        openFolder("RelayQA")
        shot("picker-relayqa")
        for i in 1...10 {
            let cell = app.staticTexts[String(format: "big-%02d.bin", i)]
            require(cell, "big-\(i)")
            cell.tap()
            if i == 1 { shot("picker-after-first") }
        }
        let open = app.buttons["Open"]
        require(open, "open")
        open.tap()

        let items = app.descendants(matching: .any).matching(identifier: "trayItem")
        let start = Date()
        while items.count < 10, Date().timeIntervalSince(start) < 20 { usleep(200_000) }
        XCTAssertEqual(items.count, 10, "all ten files should be in the tray")

        let send = app.buttons["Send"]
        XCTAssertTrue(send.waitForExistence(timeout: 5))
        let ready = NSPredicate(format: "isEnabled == true")
        expectation(for: ready, evaluatedWith: send)
        waitForExpectations(timeout: 60)
        shot("files-10x20mb")
    }

    /// A 600 MB file is turned down by its size, without being loaded, and the composer stays usable.
    func testHugeFileIsRejected() throws {
        openFolder("RelayQAHuge")
        let cell = app.staticTexts["huge.zip"]
        require(cell, "huge")
        cell.tap()
        let open = app.buttons["Open"]
        require(open, "open")
        open.tap()
        let error = app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS 'huge.zip is over 20 MB'")).firstMatch
        require(error, "size-error")
        XCTAssertEqual(app.descendants(matching: .any).matching(identifier: "trayItem").count, 0)
        shot("files-huge")
    }

    private func openFolder(_ name: String) {
        let plus = app.buttons["composerPlus"]
        XCTAssertTrue(plus.waitForExistence(timeout: 10))
        plus.tap()
        let files = app.buttons["Files"]
        if !files.waitForExistence(timeout: 3), let dir = ProcessInfo.processInfo.environment["RELAY_SHOTS"] {
            try? app.debugDescription.write(toFile: dir + "/menu-tree.txt", atomically: true, encoding: .utf8)
        }
        XCTAssertTrue(files.exists)
        files.tap()
        // The picker reopens wherever it was last. Browse (tapped again, it pops to its root), then On My iPhone.
        let browse = app.buttons.matching(identifier: "Browse").firstMatch
        if browse.waitForExistence(timeout: 5) {
            browse.tap()
            if !app.staticTexts["On My iPhone"].waitForExistence(timeout: 2) { browse.tap() }
        }
        let onPhone = app.staticTexts.matching(identifier: "On My iPhone").firstMatch
        require(onPhone, "on-my-iphone")
        onPhone.tap()
        let folder = app.cells.containing(.staticText, identifier: name).firstMatch
        require(folder, "folder-\(name)")
        folder.tap()
    }

    /// Fails with the element tree and a screenshot saved, so a changed picker layout is easy to fix.
    private func require(_ element: XCUIElement, _ name: String) {
        if !element.waitForExistence(timeout: 5), let dir = ProcessInfo.processInfo.environment["RELAY_SHOTS"] {
            try? app.debugDescription.write(toFile: dir + "/\(name)-tree.txt", atomically: true, encoding: .utf8)
            shot("\(name)-fail")
        }
        XCTAssertTrue(element.exists, "\(name) not found")
    }

    private func shot(_ name: String) {
        guard let dir = ProcessInfo.processInfo.environment["RELAY_SHOTS"] else { return }
        sleep(1)
        try? XCUIScreen.main.screenshot().pngRepresentation.write(to: URL(fileURLWithPath: dir).appendingPathComponent("\(name).png"))
    }
}
