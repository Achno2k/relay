import Foundation
import Synchronization
@testable import RelayCore

/// An in-process herdr: a unix socket answering one JSON line per request from canned handlers.
///
/// Many of these run at once in one test process, so fd numbers get recycled constantly. Every fd
/// has exactly one owner that closes it: the accept thread owns the listening socket and the wake
/// pipe, each serve thread owns its connection. `stop()` never closes anything; it wakes the
/// owners (pipe write, `shutdown`) and lets them close. Closing an fd another thread is about to
/// use would send that thread's next `accept`/`read`/`write` to whatever socket reuses the number,
/// possibly another test's.
final class FakeHerdr: Sendable {
    typealias Handler = @Sendable (_ method: String, _ params: [String: Any]) -> Any

    let dir: URL
    let socketPath: String
    private let handler: Handler
    private let listenFD: Int32
    private let wakeRead: Int32
    private let wakeWrite: Int32
    private let acceptDone = DispatchSemaphore(value: 0)
    /// Open connections. A serve thread closes its fd while holding this lock, and `stop()` only
    /// shuts down fds while holding it, so it never touches a closed (or recycled) number.
    private let conns = Mutex(Conns())
    let calls = Mutex<[(String, String)]>([])

    private struct Conns {
        var open: Set<Int32> = []
        var stopped = false
    }

    /// `reusing` lets a test simulate herdr restarting on the same socket path: pass the previous
    /// instance's `socketPath` (its directory is kept, not regenerated).
    init(reusing existingSocketPath: String? = nil, handler: @escaping Handler) throws {
        if let existingSocketPath {
            dir = URL(fileURLWithPath: existingSocketPath).deletingLastPathComponent()
            socketPath = existingSocketPath
            if FileManager.default.fileExists(atPath: socketPath) { try FileManager.default.removeItem(atPath: socketPath) }
        } else {
            dir = URL(fileURLWithPath: "/tmp").appendingPathComponent("relay-\(UUID().uuidString)")
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: false)
            socketPath = dir.appendingPathComponent("s").path
        }
        self.handler = handler

        func fail(_ what: String, closing fds: Int32...) -> HerdrError {
            let msg = String(cString: strerror(errno))
            for fd in fds { Darwin.close(fd) }
            return HerdrError.io("FakeHerdr: \(what): \(msg)")
        }
        let fd = socket(AF_UNIX, SOCK_STREAM, 0)
        guard fd >= 0 else { throw fail("socket") }
        var addr = sockaddr_un()
        addr.sun_family = sa_family_t(AF_UNIX)
        let bytes = Array(socketPath.utf8)
        withUnsafeMutableBytes(of: &addr.sun_path) { raw in
            raw.copyBytes(from: bytes)
            raw[bytes.count] = 0
        }
        let bound = withUnsafePointer(to: &addr) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { bind(fd, $0, socklen_t(MemoryLayout<sockaddr_un>.size)) }
        }
        guard bound == 0 else { throw fail("bind", closing: fd) }
        guard listen(fd, 16) == 0 else { throw fail("listen", closing: fd) }
        var pipeFDs: [Int32] = [0, 0]
        guard pipe(&pipeFDs) == 0 else { throw fail("pipe", closing: fd) }
        listenFD = fd
        wakeRead = pipeFDs[0]
        wakeWrite = pipeFDs[1]
        let t = Thread { [self] in
            acceptLoop()
            Darwin.close(listenFD)
            Darwin.close(wakeRead)
            Darwin.close(wakeWrite)
            acceptDone.signal()
        }
        t.start()
    }

    /// Also drops already-accepted connections (e.g. a live `events.subscribe`), like a real herdr
    /// crash or restart would, not just the listening socket. Returns once nothing new can connect.
    /// Safe to call more than once.
    func stop() {
        let first = conns.withLock { c -> Bool in
            guard !c.stopped else { return false }
            c.stopped = true
            for fd in c.open { Darwin.shutdown(fd, SHUT_RDWR) }
            return true
        }
        guard first else { return }
        var b: UInt8 = 1
        _ = Darwin.write(wakeWrite, &b, 1)
        acceptDone.wait()
        try? FileManager.default.removeItem(atPath: socketPath)
    }

    func methods() -> [String] { calls.withLock { $0.map(\.0) } }
    func params(of method: String) -> String? { calls.withLock { $0.last { $0.0 == method }?.1 } }

    private func acceptLoop() {
        while true {
            var fds = [pollfd(fd: listenFD, events: Int16(POLLIN), revents: 0),
                       pollfd(fd: wakeRead, events: Int16(POLLIN), revents: 0)]
            let r = poll(&fds, 2, -1)
            if r < 0 { if errno == EINTR { continue } else { return } }
            if fds[1].revents != 0 { return }
            guard fds[0].revents != 0 else { continue }
            let c = accept(listenFD, nil, nil)
            if c < 0 { if errno == EINTR || errno == ECONNABORTED { continue } else { return } }
            // Registering under the lock closes the gap with `stop()`: either it sees `c` and shuts
            // it down, or we see `stopped` and drop `c` here.
            let keep = conns.withLock { s -> Bool in
                guard !s.stopped else { return false }
                s.open.insert(c)
                return true
            }
            guard keep else {
                Darwin.close(c)
                return
            }
            // A client that gives up early (timeouts, a resubscribe) must not SIGPIPE the test process.
            var one: Int32 = 1
            setsockopt(c, SOL_SOCKET, SO_NOSIGPIPE, &one, socklen_t(MemoryLayout<Int32>.size))
            let t = Thread { [self] in serve(c) }
            t.start()
        }
    }

    private func serve(_ fd: Int32) {
        defer {
            conns.withLock { c in
                c.open.remove(fd)
                Darwin.close(fd)
            }
        }
        var buf = Data()
        var chunk = [UInt8](repeating: 0, count: 65536)
        while true {
            let n = read(fd, &chunk, chunk.count)
            if n <= 0 { return }
            buf.append(contentsOf: chunk[0..<n])
            while let nl = buf.firstIndex(of: UInt8(ascii: "\n")) {
                let line = buf[buf.startIndex..<nl]
                buf.removeSubrange(buf.startIndex...nl)
                guard let req = (try? JSONSerialization.jsonObject(with: Data(line))) as? [String: Any],
                      let method = req["method"] as? String else { continue }
                let params = req["params"] as? [String: Any] ?? [:]
                let pjson = (try? JSONSerialization.data(withJSONObject: params, options: [.sortedKeys, .withoutEscapingSlashes])).map { String(decoding: $0, as: UTF8.self) } ?? ""
                calls.withLock { $0.append((method, pjson)) }
                let out = handler(method, params)
                var resp: [String: Any] = ["id": req["id"] ?? ""]
                if let err = out as? FakeError {
                    resp["error"] = ["code": err.code, "message": err.message]
                } else {
                    resp["result"] = out
                }
                var data = (try? JSONSerialization.data(withJSONObject: resp)) ?? Data()
                data.append(UInt8(ascii: "\n"))
                _ = data.withUnsafeBytes { write(fd, $0.baseAddress!, $0.count) }
                if method == "events.subscribe" {
                    // Keep the subscription open until the client leaves.
                    while read(fd, &chunk, chunk.count) > 0 {}
                    return
                }
            }
        }
    }
}

struct FakeError {
    var code: String
    var message: String
}

/// Synthetic herdr world used by the route tests.
enum World {
    static func agent(_ pane: String, name: String?, status: String, cwd: String, session: [String: Any]?, title: String = "Some task") -> [String: Any] {
        var a: [String: Any] = [
            "pane_id": pane, "workspace_id": String(pane.split(separator: ":")[0]), "tab_id": "\(pane.split(separator: ":")[0]):t1",
            "terminal_id": "term_\(pane)", "agent": "claude", "agent_status": status, "focused": false, "revision": 1,
            "terminal_title_stripped": title, "cwd": cwd, "foreground_cwd": cwd, "state_change_seq": 3,
        ]
        if let name { a["name"] = name }
        if let session { a["agent_session"] = session }
        return a
    }

    static var workspaces: [[String: Any]] { [
        ["workspace_id": "w1", "label": "shop-api", "number": 1, "focused": false, "pane_count": 2, "tab_count": 1, "active_tab_id": "w1:t1", "agent_status": "idle"],
        ["workspace_id": "w2", "label": "website", "number": 2, "focused": false, "pane_count": 1, "tab_count": 1, "active_tab_id": "w2:t1", "agent_status": "idle"],
    ] }
}
