import Foundation
import Logging
import ServiceLifecycle

/// launchd redirects `relay serve`'s stdout/stderr straight into `~/.relay/relay.log` and just keeps
/// appending, forever. This periodically truncates that file in place once it passes `maxBytes`, so
/// the log stays size-capped without needing launchd or an external logrotate.
public actor LogRotator: Service {
    let path: String
    let maxBytes: Int
    let checkInterval: Duration
    /// The fds to truncate: stdout and stderr, since launchd points both at `path`. Overridable for tests.
    let fds: [Int32]

    public init(path: String = RelayHome.url.appendingPathComponent("relay.log").path,
                maxBytes: Int = 10 * 1024 * 1024, checkInterval: Duration = .seconds(300), fds: [Int32] = [1, 2]) {
        self.path = path
        self.maxBytes = maxBytes
        self.checkInterval = checkInterval
        self.fds = fds
    }

    public func run() async throws {
        while !Task.isCancelled {
            rotateIfNeeded()
            try? await Task.sleep(for: checkInterval)
        }
    }

    /// Truncates `path` (and every fd pointing at it) to zero once it's over `maxBytes`.
    /// Returns whether it rotated, for tests.
    @discardableResult
    public func rotateIfNeeded() -> Bool {
        guard let attrs = try? FileManager.default.attributesOfItem(atPath: path),
              let size = (attrs[.size] as? NSNumber)?.uint64Value, size > UInt64(maxBytes)
        else { return false }
        for fd in fds {
            ftruncate(fd, 0)
            lseek(fd, 0, SEEK_SET)
        }
        return true
    }
}
