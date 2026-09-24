import Foundation
import Logging
import ServiceLifecycle

/// Deletes uploads older than 7 days: once at startup, then daily.
public struct UploadCleaner: Service {
    let store: UploadStore
    let logger: Logger

    public init(store: UploadStore, logger: Logger) {
        self.store = store
        self.logger = logger
    }

    public func run() async throws {
        await cancelWhenGracefulShutdown {
            while !Task.isCancelled {
                let n = store.cleanup()
                if n > 0 { logger.info("removed \(n) expired uploads") }
                try? await Task.sleep(for: .seconds(24 * 3600))
            }
        }
    }
}
