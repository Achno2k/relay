import Foundation
import Synchronization
@testable import HerdCore

/// An in-process herdr: a unix socket answering one JSON line per request from canned handlers.
final class FakeHerdr: Sendable {
    typealias Handler = @Sendable (_ method: String, _ params: [String: Any]) -> Any

    let dir: URL
    let socketPath: String
    private let handler: Handler
    private let listenFD: Int32
    let calls = Mutex<[(String, String)]>([])

    init(handler: @escaping Handler) throws {
        dir = URL(fileURLWithPath: "/tmp").appendingPathComponent("herd-\(UUID().uuidString.prefix(8))")
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        socketPath = dir.appendingPathComponent("s").path
        self.handler = handler

        let fd = socket(AF_UNIX, SOCK_STREAM, 0)
        var addr = sockaddr_un()
        addr.sun_family = sa_family_t(AF_UNIX)
        let bytes = Array(socketPath.utf8)
        withUnsafeMutableBytes(of: &addr.sun_path) { raw in
            raw.copyBytes(from: bytes)
            raw[bytes.count] = 0
        }
        _ = withUnsafePointer(to: &addr) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) { bind(fd, $0, socklen_t(MemoryLayout<sockaddr_un>.size)) }
        }
        listen(fd, 16)
        listenFD = fd
        let t = Thread { [self] in acceptLoop() }
        t.start()
    }

    func stop() {
        Darwin.shutdown(listenFD, SHUT_RDWR)
        Darwin.close(listenFD)
        try? FileManager.default.removeItem(at: dir)
    }

    func methods() -> [String] { calls.withLock { $0.map(\.0) } }
    func params(of method: String) -> String? { calls.withLock { $0.last { $0.0 == method }?.1 } }

    private func acceptLoop() {
        while true {
            let c = accept(listenFD, nil, nil)
            if c < 0 { return }
            let t = Thread { [self] in serve(c) }
            t.start()
        }
    }

    private func serve(_ fd: Int32) {
        defer { close(fd) }
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
