import Foundation

enum Fixture {
    static func url(_ name: String) -> URL {
        Bundle.module.url(forResource: "Fixtures/\(name)", withExtension: nil)!
    }

    static func data(_ name: String) throws -> Data {
        try Data(contentsOf: url(name))
    }

    static func text(_ name: String) throws -> String {
        try String(contentsOf: url(name), encoding: .utf8)
    }
}

/// Races `op` against `duration`; nil means it didn't finish in time. Used to keep resilience
/// tests (reconnects, timeouts) from hanging forever if a fix regresses.
func withTimeout<T: Sendable>(_ duration: Duration, _ op: @escaping @Sendable () async -> T) async -> T? {
    await withTaskGroup(of: T?.self) { group in
        group.addTask { await op() }
        group.addTask {
            try? await Task.sleep(for: duration)
            return nil
        }
        let result = await group.next() ?? nil
        group.cancelAll()
        return result
    }
}
