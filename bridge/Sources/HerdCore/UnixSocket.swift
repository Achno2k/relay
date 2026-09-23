import Foundation

/// Blocking newline-delimited JSON over a unix domain socket. Call only from a background queue/thread.
final class UnixSocket: @unchecked Sendable {
    private let lock = NSLock()
    private var fd: Int32
    private var buffer = Data()

    init(path: String, readTimeout: TimeInterval?) throws {
        fd = socket(AF_UNIX, SOCK_STREAM, 0)
        guard fd >= 0 else { throw HerdrError.io("socket: \(String(cString: strerror(errno)))") }
        var one: Int32 = 1
        setsockopt(fd, SOL_SOCKET, SO_NOSIGPIPE, &one, socklen_t(MemoryLayout<Int32>.size))
        if let readTimeout {
            var tv = timeval(tv_sec: Int(readTimeout), tv_usec: Int32((readTimeout - floor(readTimeout)) * 1_000_000))
            setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &tv, socklen_t(MemoryLayout<timeval>.size))
        }
        var addr = sockaddr_un()
        addr.sun_family = sa_family_t(AF_UNIX)
        let bytes = Array(path.utf8)
        let capacity = MemoryLayout.size(ofValue: addr.sun_path)
        guard bytes.count < capacity else {
            Darwin.close(fd)
            throw HerdrError.io("socket path too long")
        }
        withUnsafeMutableBytes(of: &addr.sun_path) { raw in
            raw.copyBytes(from: bytes)
            raw[bytes.count] = 0
        }
        let rc = withUnsafePointer(to: &addr) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                connect(fd, $0, socklen_t(MemoryLayout<sockaddr_un>.size))
            }
        }
        guard rc == 0 else {
            let msg = String(cString: strerror(errno))
            Darwin.close(fd)
            throw HerdrError.unavailable("cannot connect to herdr socket: \(msg)")
        }
    }

    deinit { close() }

    func close() {
        lock.lock()
        defer { lock.unlock() }
        if fd >= 0 {
            Darwin.shutdown(fd, SHUT_RDWR)
            Darwin.close(fd)
            fd = -1
        }
    }

    private var currentFD: Int32 {
        lock.lock()
        defer { lock.unlock() }
        return fd
    }

    func writeLine(_ data: Data) throws {
        var payload = data
        payload.append(UInt8(ascii: "\n"))
        try payload.withUnsafeBytes { raw in
            var offset = 0
            while offset < raw.count {
                let fd = currentFD
                guard fd >= 0 else { throw HerdrError.io("socket closed") }
                let n = Darwin.write(fd, raw.baseAddress! + offset, raw.count - offset)
                if n < 0 {
                    if errno == EINTR { continue }
                    throw HerdrError.io("write: \(String(cString: strerror(errno)))")
                }
                offset += n
            }
        }
    }

    /// Returns the next complete line, or throws on EOF/timeout.
    func readLine() throws -> Data {
        var chunk = [UInt8](repeating: 0, count: 65536)
        while true {
            if let nl = buffer.firstIndex(of: UInt8(ascii: "\n")) {
                let line = buffer[buffer.startIndex..<nl]
                buffer.removeSubrange(buffer.startIndex...nl)
                return Data(line)
            }
            let fd = currentFD
            guard fd >= 0 else { throw HerdrError.io("socket closed") }
            let n = Darwin.read(fd, &chunk, chunk.count)
            if n > 0 {
                buffer.append(contentsOf: chunk[0..<n])
            } else if n == 0 {
                throw HerdrError.io("herdr closed the connection")
            } else if errno == EINTR {
                continue
            } else if errno == EAGAIN || errno == EWOULDBLOCK {
                throw HerdrError.timeout
            } else {
                throw HerdrError.io("read: \(String(cString: strerror(errno)))")
            }
        }
    }
}
