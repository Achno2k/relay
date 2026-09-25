import Foundation
import Synchronization
import Testing
@testable import RelayCore

/// `UnixSocket` must close each fd exactly once, and never while another thread is still using
/// it. Either mistake closes whatever socket the OS gave that fd number to next, in the bridge
/// that's NIO's or another herdr call's socket. These tests churn sockets while a watcher thread
/// keeps opening pipes and checks nobody else closed them.
@Suite struct FdHygieneTests {
    /// Opens pipes on a loop and records every time one of its own fds was closed behind its back.
    final class Watcher: Sendable {
        let strayCloses = Atomic<Int>(0)
        private let running = Atomic<Bool>(true)
        private let done = DispatchSemaphore(value: 0)

        init() {
            let t = Thread { [self] in
                while running.load(ordering: .relaxed) {
                    var p: [Int32] = [0, 0]
                    guard pipe(&p) == 0 else { continue }
                    for _ in 0..<50 where fcntl(p[0], F_GETFD) < 0 || fcntl(p[1], F_GETFD) < 0 {
                        strayCloses.add(1, ordering: .relaxed)
                        break
                    }
                    close(p[0])
                    close(p[1])
                }
                done.signal()
            }
            t.start()
        }

        func finish() -> Int {
            running.store(false, ordering: .relaxed)
            done.wait()
            return strayCloses.load(ordering: .relaxed)
        }
    }

    @Test func failedConnectClosesItsFdOnce() {
        let watcher = Watcher()
        let missing = "/tmp/relay-missing-\(UUID().uuidString)"
        for _ in 0..<3000 { _ = try? UnixSocket(path: missing, readTimeout: 1) }
        #expect(watcher.finish() == 0)
    }

    @Test func closeWhileReadingNeverClosesSomeoneElsesFd() throws {
        let fake = try FakeHerdr { _, _ in ["type": "subscription_started"] }
        defer { fake.stop() }
        let watcher = Watcher()
        for _ in 0..<300 {
            let sock = try UnixSocket(path: fake.socketPath, readTimeout: 5)
            try sock.writeLine(Data(#"{"id":"1","method":"events.subscribe","params":{}}"#.utf8))
            _ = try sock.readLine()
            let reading = DispatchSemaphore(value: 0)
            let finished = DispatchSemaphore(value: 0)
            let t = Thread {
                reading.signal()
                _ = try? sock.readLine()  // blocks until `close()` below wakes it
                finished.signal()
            }
            t.start()
            reading.wait()
            sock.close()
            #expect(finished.wait(timeout: .now() + 5) == .success)
        }
        #expect(watcher.finish() == 0)
    }
}
