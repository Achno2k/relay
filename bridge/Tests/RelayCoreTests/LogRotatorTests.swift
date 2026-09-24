import Foundation
import Testing
@testable import RelayCore

@Suite struct LogRotatorTests {
    @Test func truncatesOnceOverTheCapAndLeavesSmallFilesAlone() async throws {
        let path = "/tmp/relay-log-\(UUID().uuidString.prefix(8)).log"
        defer { try? FileManager.default.removeItem(atPath: path) }
        FileManager.default.createFile(atPath: path, contents: Data())
        // Open our own fd on it, standing in for launchd's stdout/stderr redirection.
        let fd = open(path, O_WRONLY | O_APPEND)
        #expect(fd >= 0)
        defer { close(fd) }

        let small = Data(repeating: UInt8(ascii: "x"), count: 100)
        _ = small.withUnsafeBytes { write(fd, $0.baseAddress, $0.count) }
        let rotator = LogRotator(path: path, maxBytes: 1024, fds: [fd])
        #expect(await rotator.rotateIfNeeded() == false)
        #expect(try FileManager.default.attributesOfItem(atPath: path)[.size] as? Int == 100)

        let big = Data(repeating: UInt8(ascii: "y"), count: 2000)
        _ = big.withUnsafeBytes { write(fd, $0.baseAddress, $0.count) }
        #expect(await rotator.rotateIfNeeded() == true)
        #expect(try FileManager.default.attributesOfItem(atPath: path)[.size] as? Int == 0)

        // The fd still works after truncation: a subsequent write lands at offset 0, not a hole.
        let after = Data(repeating: UInt8(ascii: "z"), count: 10)
        _ = after.withUnsafeBytes { write(fd, $0.baseAddress, $0.count) }
        #expect(try FileManager.default.attributesOfItem(atPath: path)[.size] as? Int == 10)
    }

    @Test func missingFileIsANoOp() async {
        let rotator = LogRotator(path: "/tmp/relay-log-does-not-exist-\(UUID().uuidString).log", maxBytes: 10, fds: [])
        #expect(await rotator.rotateIfNeeded() == false)
    }
}
