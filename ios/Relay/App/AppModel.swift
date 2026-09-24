import Foundation
import HerdKit
import Observation

/// Launch arguments. `-mock` runs against the bundled fixtures; the rest set up a screen for screenshots:
/// `-demo sidebar|tools|top|card|newChat|pairing`, `-agent <id>`, `-replay off`, `-pair <herd:// link>`, `-uitestAttachments`, `-uitest`, `-resetSidebar`.
struct LaunchOptions {
    var mock = false
    var demo: String?
    var agent: String?
    var replay = true
    /// A `herd://pair` link handled at launch, same path as `onOpenURL`.
    var pairLink: URL?
    /// `-uitestAttachments`: the `+` menu offers generated test files (UI tests can't drive the Photos picker).
    var testAttachments = false
    /// `-testWord X`: the word drawn into the generated test image and PDF, so a live test can't match old replies.
    var testWord: String?
    /// `-uitest`: keep all UI state in a separate defaults suite (see `AppDefaults`).
    var isUITest = false
    /// `-resetSidebar` (with `-uitest`): start from empty test state.
    var resetTestState = false

    static let current = LaunchOptions(arguments: ProcessInfo.processInfo.arguments)

    init(arguments: [String]) {
        #if DEBUG
        func value(_ flag: String) -> String? {
            guard let i = arguments.firstIndex(of: flag), i + 1 < arguments.count else { return nil }
            return arguments[i + 1]
        }
        mock = arguments.contains("-mock")
        demo = value("-demo")
        agent = value("-agent")
        replay = value("-replay") != "off"
        pairLink = value("-pair").flatMap(URL.init(string:))
        testAttachments = arguments.contains("-uitestAttachments")
        testWord = value("-testWord")
        isUITest = arguments.contains("-uitest")
        resetTestState = arguments.contains("-resetSidebar")
        #endif
    }

    func isDemo(_ name: String) -> Bool { demo == name }
}

/// Paired or not. Builds an `AppStore` once there's a bridge (or the mock) to talk to.
@MainActor
@Observable
final class AppModel {
    private(set) var store: AppStore?
    var pairingError: String?
    var isPairing = false

    init(options: LaunchOptions = .current) {
        if options.resetTestState { AppDefaults.resetTestState() }
        #if DEBUG
        if options.mock {
            if !options.isDemo("pairing") {
                let backend = MockBackend(replayInterval: options.replay ? .seconds(4) : nil)
                let store = AppStore(backend: backend, hostLabel: "Mock bridge")
                if let agent = options.agent { store.selectedAgentId = agent }
                self.store = store
            }
            return
        }
        #endif
        if let pairing = PairingStore.load() {
            store = Self.makeStore(pairing)
        }
        if let link = options.pairLink { handle(url: link) }
    }

    private static func makeStore(_ pairing: Pairing) -> AppStore {
        let store = AppStore(backend: LiveBackend(pairing: pairing), hostLabel: pairing.url.host() ?? pairing.url.absoluteString)
        #if DEBUG
        if let agent = LaunchOptions.current.agent { store.selectedAgentId = agent }
        #endif
        return store
    }

    /// Checks the token against `/agents` before saving it.
    func pair(_ pairing: Pairing) async {
        isPairing = true
        pairingError = nil
        defer { isPairing = false }
        do {
            _ = try await APIClient(baseURL: pairing.url, token: pairing.token).agents()
            try PairingStore.save(pairing)
            store?.stop()
            store = Self.makeStore(pairing)
        } catch {
            pairingError = error.localizedDescription
        }
    }

    func handle(url: URL) {
        guard url.scheme?.lowercased() == "herd" else { return }
        guard let pairing = Pairing(link: url) else {
            pairingError = HerdError.invalidPairingLink.localizedDescription
            return
        }
        Task { await pair(pairing) }
    }

    func unpair() {
        store?.stop()
        store = nil
        PairingStore.clear()
    }
}
