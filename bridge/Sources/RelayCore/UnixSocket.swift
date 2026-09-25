import Foundation

/// Blocking newline-delimited JSON over a unix domain socket. Call only from a background queue/thread.
final class UnixSocket: @unchecked Sendable {
    private let lock = NSLock()
    private var fd: Int32
    private var closed = false
    private var inFlight = 0
    private var buffer = Data()

    /// Failure paths throw without closing `fd`: every stored property is set by then, so Swift
    /// runs `deinit` (and `close()`) on the half-built socket anyway. Closing here as well used to
    /// close the fd number twice, and the second close could hit whatever socket reused it.
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
        guard bytes.count < capacity else { throw HerdrError.io("socket path too long") }
        withUnsafeMutableBytes(of: &addr.sun_path) { raw in
            raw.copyBytes(from: bytes)
            raw[bytes.count] = 0
        }
        let rc = withUnsafePointer(to: &addr) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                connect(fd, $0, socklen_t(MemoryLayout<sockaddr_un>.size))
            }
        }
        guard rc == 0 else { throw HerdrError.unavailable("cannot connect to herdr socket: \(String(cString: strerror(errno)))") }
    }

    deinit { close() }

    /// Safe from any thread, including while another thread is blocked in `readLine`/`writeLine`
    /// (that's how `HerdrEventStream` cancels a subscription). `shutdown` wakes the blocked call;
    /// the fd itself is only closed once no call is using it. Closing it under a reader's feet would
    /// let that reader's next `read` land on whatever socket the OS hands the fd number to next.
    func close() {
        lock.lock()
        defer { lock.unlock() }
        guard !closed else { return }
        closed = true
        if fd >= 0 { Darwin.shutdown(fd, SHUT_RDWR) }
        if inFlight == 0 { release() }
    }

    /// Lends the fd to one syscall. It stays open until `end()`, even if `close()` runs meanwhile.
    private func begin() throws -> Int32 {
        lock.lock()
        defer { lock.unlock() }
        guard !closed else { throw HerdrError.io("socket closed") }
        inFlight += 1
        return fd
    }

    private func end() {
        lock.lock()
        defer { lock.unlock() }
        inFlight -= 1
        if closed && inFlight == 0 { release() }
    }

    /// Caller holds `lock`.
    private func release() {
        guard fd >= 0 else { return }
        Darwin.close(fd)
        fd = -1
    }

    func writeLine(_ data: Data) throws {
        var payload = data
        payload.append(UInt8(ascii: "\n"))
        try payload.withUnsafeBytes { raw in
            var offset = 0
            while offset < raw.count {
                let fd = try begin()
                let n = Darwin.write(fd, raw.baseAddress! + offset, raw.count - offset)
                let err = errno
                end()
                if n < 0 {
                    if err == EINTR { continue }
                    throw HerdrError.io("write: \(String(cString: strerror(err)))")
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
            let fd = try begin()
            let n = Darwin.read(fd, &chunk, chunk.count)
            let err = errno
            end()
            if n > 0 {
                buffer.append(contentsOf: chunk[0..<n])
            } else if n == 0 {
                throw HerdrError.io("herdr closed the connection")
            } else if err == EINTR {
                continue
            } else if err == EAGAIN || err == EWOULDBLOCK {
                throw HerdrError.timeout
            } else {
                throw HerdrError.io("read: \(String(cString: strerror(err)))")
            }
        }
    }
}
