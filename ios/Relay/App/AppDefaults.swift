import Foundation
import RelayKit

/// Where the app keeps its own UI state: selected chat, seen times, filter, expanded folders, device chip,
/// archive. Under UI tests (`-uitest`) or unit tests (XCTest host) everything goes to a separate suite,
/// so tests on the user's phone never read or change their real state.
enum AppDefaults {
    static let testSuite = "dev.amansingh.herd.uitest"

    static var isIsolated: Bool {
        LaunchOptions.current.isUITest || ProcessInfo.processInfo.environment["XCTestConfigurationFilePath"] != nil
    }

    /// Per-device UI state.
    static var standard: UserDefaults {
        isIsolated ? UserDefaults(suiteName: testSuite)! : .standard
    }

    /// State extensions may read later (the archive); the App Group when not isolated.
    static var shared: UserDefaults {
        isIsolated ? UserDefaults(suiteName: testSuite)! : PairingStore.sharedDefaults
    }

    /// Clears the test suite only; never the user's domains.
    static func resetTestState() {
        guard isIsolated else { return }
        UserDefaults.standard.removePersistentDomain(forName: testSuite)
    }
}
