import Foundation
import IOKit.ps
import Synchronization
import UniformTypeIdentifiers

public enum AttachmentKind: String, Codable, Sendable {
    case image, pdf, file

    public static func of(name: String) -> AttachmentKind {
        let ext = (name as NSString).pathExtension
        guard !ext.isEmpty, let type = UTType(filenameExtension: ext) else { return .file }
        if type.conforms(to: .pdf) { return .pdf }
        if type.conforms(to: .image) { return .image }
        return .file
    }
}

public struct Attachment: Codable, Sendable, Equatable {
    public var id: String
    public var name: String
    public var kind: AttachmentKind
    public var size: Int
}

/// Files the phone attached, kept on the Mac under `~/.herd/uploads/<pane>/<id>-<name>` (0600).
/// Paths stay on the Mac: prompts carry them to Claude, the wire only ever sees id, name and kind.
public struct UploadStore: Sendable {
    public let root: URL
    public static let maxBytes = 20 * 1024 * 1024
    public static let maxPerMessage = 10
    public static let lifetime: TimeInterval = 7 * 24 * 3600
    static let markerPrefix = "Attached files: "

    public init(root: URL = HerdHome.url.appendingPathComponent("uploads")) {
        self.root = root.standardizedFileURL
    }

    // MARK: Names

    /// Keeps `A-Z a-z 0-9 . _ -`, turns the rest into `-`, collapses runs, cuts the stem to 80.
    public static func sanitize(_ raw: String) -> String {
        let base = (raw as NSString).lastPathComponent
        var out = ""
        for u in base.unicodeScalars {
            let ok = (u.isASCII && (CharacterSet.alphanumerics.contains(u) || u == "." || u == "_" || u == "-"))
            let c: Character = ok ? Character(u) : "-"
            if c == "-" && out.hasSuffix("-") { continue }
            out.append(c)
        }
        out = out.trimmingCharacters(in: CharacterSet(charactersIn: "-."))
        let ext = (out as NSString).pathExtension
        var stem = ext.isEmpty ? out : String(out.dropLast(ext.count + 1))
        stem = String(stem.prefix(80)).trimmingCharacters(in: CharacterSet(charactersIn: "-."))
        if stem.isEmpty { stem = "file" }
        return ext.isEmpty ? stem : stem + "." + String(ext.prefix(12))
    }

    static func paneDir(_ paneId: String) -> String {
        String(paneId.unicodeScalars.map { CharacterSet.alphanumerics.contains($0) && $0.isASCII ? Character($0) : "_" })
    }

    static func newId() -> String {
        var g = SystemRandomNumberGenerator()
        return (0..<8).map { _ in String(format: "%02x", UInt8.random(in: .min ... .max, using: &g)) }.joined()
    }

    static func isId(_ s: Substring) -> Bool {
        s.count == 16 && s.allSatisfy { $0.isHexDigit && !$0.isUppercase }
    }

    // MARK: Files

    public func save(paneId: String, data: Data, filename: String) throws -> Attachment {
        let dir = root.appendingPathComponent(Self.paneDir(paneId))
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        let name = Self.sanitize(filename)
        let id = Self.newId()
        let url = dir.appendingPathComponent("\(id)-\(name)")
        guard FileManager.default.createFile(atPath: url.path, contents: data, attributes: [.posixPermissions: 0o600]) else {
            throw CocoaError(.fileWriteUnknown)
        }
        return Attachment(id: id, name: name, kind: .of(name: name), size: data.count)
    }

    /// Finds an upload by id in any agent's folder.
    public func find(_ id: String) -> URL? {
        guard Self.isId(Substring(id)) else { return nil }
        let fm = FileManager.default
        for dir in (try? fm.contentsOfDirectory(at: root, includingPropertiesForKeys: nil)) ?? [] {
            for f in (try? fm.contentsOfDirectory(atPath: dir.path)) ?? [] where f.hasPrefix(id + "-") {
                // Built from `root`, not the listing, so the marker path matches `root` exactly.
                return root.appendingPathComponent(dir.lastPathComponent).appendingPathComponent(f)
            }
        }
        return nil
    }

    public static func mimeType(for url: URL) -> String {
        UTType(filenameExtension: url.pathExtension)?.preferredMIMEType ?? "application/octet-stream"
    }

    /// Deletes uploads older than `lifetime`, and folders left empty. Returns how many files went.
    @discardableResult
    public func cleanup(now: Date = Date()) -> Int {
        let fm = FileManager.default
        var removed = 0
        pruneSentLog(now: now)
        for dir in (try? fm.contentsOfDirectory(at: root, includingPropertiesForKeys: nil)) ?? [] where dir.hasDirectoryPath {
            let files = (try? fm.contentsOfDirectory(at: dir, includingPropertiesForKeys: [.contentModificationDateKey])) ?? []
            for f in files {
                let m = (try? f.resourceValues(forKeys: [.contentModificationDateKey]).contentModificationDate) ?? now
                if now.timeIntervalSince(m) > Self.lifetime, (try? fm.removeItem(at: f)) != nil { removed += 1 }
            }
            if ((try? fm.contentsOfDirectory(atPath: dir.path)) ?? ["x"]).isEmpty { try? fm.removeItem(at: dir) }
        }
        return removed
    }

    // MARK: Sent log

    /// What the bridge sent, so history can show attachments even after Claude Code rewrote the
    /// prompt: it swaps image paths for `[Image #N]`, wraps pastes in `<pasted_content>` tags and
    /// hard-wraps long lines, so the marker line doesn't survive intact.
    public struct SentRecord: Codable, Sendable, Equatable {
        public struct Ref: Codable, Sendable, Equatable {
            public var id: String
            public var name: String
            public var kind: AttachmentKind
        }
        public var at: Date
        public var pane: String
        public var text: String
        public var attachments: [Ref]
    }

    var sentLog: URL { root.appendingPathComponent("sent.jsonl") }

    static let logEncoder: JSONEncoder = {
        let e = JSONEncoder()
        e.dateEncodingStrategy = .iso8601
        return e
    }()

    static let logDecoder: JSONDecoder = {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .iso8601
        return d
    }()

    public func recordSent(pane: String, text: String, files: [URL], at: Date = Date()) {
        let refs = files.map { url -> SentRecord.Ref in
            let file = url.lastPathComponent
            let name = String(file.dropFirst(17))
            return SentRecord.Ref(id: String(file.prefix(16)), name: name, kind: .of(name: name))
        }
        guard var line = try? Self.logEncoder.encode(SentRecord(at: at, pane: pane, text: text, attachments: refs)) else { return }
        line.append(UInt8(ascii: "\n"))
        try? FileManager.default.createDirectory(at: root, withIntermediateDirectories: true, attributes: [.posixPermissions: 0o700])
        if let h = try? FileHandle(forWritingTo: sentLog) {
            defer { try? h.close() }
            _ = try? h.seekToEnd()
            try? h.write(contentsOf: line)
        } else {
            FileManager.default.createFile(atPath: sentLog.path, contents: line, attributes: [.posixPermissions: 0o600])
        }
    }

    func sentRecords() -> [SentRecord] {
        guard let data = try? Data(contentsOf: sentLog) else { return [] }
        return data.split(separator: UInt8(ascii: "\n")).compactMap { try? Self.logDecoder.decode(SentRecord.self, from: Data($0)) }
    }

    /// Whether a transcript user message looks like one of ours after Claude Code rewrote it.
    public static func looksAttached(_ text: String) -> Bool {
        text.contains(markerPrefix.trimmingCharacters(in: .whitespaces)) || text.contains("[Image #")
    }

    /// Compares prompt text while ignoring what Claude Code changes: pasted-content tags,
    /// `[Image #N]` placeholders, the marker line onwards, and all whitespace (it hard-wraps).
    public static func fingerprint(_ text: String) -> String {
        var t = PastedContent.strip(text)
        t = t.replacingOccurrences(of: #"\[Image #\d+\]"#, with: "", options: .regularExpression)
        if let r = t.range(of: markerPrefix.trimmingCharacters(in: .whitespaces)) { t = String(t[..<r.lowerBound]) }
        return String(t.unicodeScalars.filter { !CharacterSet.whitespacesAndNewlines.contains($0) }.map(Character.init))
    }

    /// The sent record whose text matches `text`, closest to `at`.
    public func lookupSent(_ text: String, at: Date?) -> SentRecord? {
        let fp = Self.fingerprint(text)
        let matches = sentRecords().filter { Self.fingerprint($0.text) == fp }
        guard let at else { return matches.last }
        return matches.min { abs($0.at.timeIntervalSince(at)) < abs($1.at.timeIntervalSince(at)) }
            .flatMap { abs($0.at.timeIntervalSince(at)) < 600 ? $0 : nil }
    }

    func pruneSentLog(now: Date) {
        let keep = sentRecords().filter { now.timeIntervalSince($0.at) <= Self.lifetime }
        let data = keep.compactMap { try? Self.logEncoder.encode($0) }.reduce(into: Data()) { d, l in
            d.append(l)
            d.append(UInt8(ascii: "\n"))
        }
        if FileManager.default.fileExists(atPath: sentLog.path) { try? data.write(to: sentLog) }
    }

    // MARK: Marker

    /// The prompt Claude receives: the text, a blank line, then `Attached files: <path> <path>`.
    public static func prompt(text: String, paths: [URL]) -> String {
        guard !paths.isEmpty else { return text }
        let line = markerPrefix + paths.map(\.path).joined(separator: " ")
        return text.isEmpty ? line : text + "\n\n" + line
    }

    public struct Parsed: Equatable, Sendable {
        public var attachments: [(id: String, name: String, kind: AttachmentKind)]
        public var text: String

        public static func == (a: Parsed, b: Parsed) -> Bool {
            a.text == b.text && a.attachments.map { "\($0.id)|\($0.name)|\($0.kind)" } == b.attachments.map { "\($0.id)|\($0.name)|\($0.kind)" }
        }
    }

    /// Splits our marker line off a user message. Nil unless every path is one of our uploads.
    public func parseMarker(_ text: String) -> Parsed? {
        let trimmed = text.trimmingCharacters(in: .whitespacesAndNewlines)
        let lines = trimmed.components(separatedBy: "\n")
        guard let last = lines.last, last.hasPrefix(Self.markerPrefix) else { return nil }
        let prefixes = Set([root.path + "/", root.resolvingSymlinksInPath().path + "/"])
        var out: [(String, String, AttachmentKind)] = []
        for token in last.dropFirst(Self.markerPrefix.count).split(separator: " ") {
            guard prefixes.contains(where: { token.hasPrefix($0) }) else { return nil }
            let file = token.split(separator: "/").last ?? ""
            let id = file.prefix(16)
            guard Self.isId(id), file.dropFirst(16).first == "-" else { return nil }
            let name = String(file.dropFirst(17))
            out.append((String(id), name, .of(name: name)))
        }
        guard !out.isEmpty else { return nil }
        let body = lines.dropLast().joined(separator: "\n").trimmingCharacters(in: .whitespacesAndNewlines)
        return Parsed(attachments: out, text: body)
    }
}

public struct Machine: Codable, Sendable, Equatable {
    public var id: String
    public var name: String
    public var kind: String
    public var model: String
    public var os: String

    /// Newer Macs report ids like `Mac17,2` for laptops too, so an internal battery decides;
    /// the old `*Book*` names are the fallback.
    public static func kind(forModel model: String, hasBattery: Bool = false) -> String {
        hasBattery || model.contains("Book") ? "laptop" : "desktop"
    }

    static func hasInternalBattery() -> Bool {
        guard let info = IOPSCopyPowerSourcesInfo()?.takeRetainedValue(),
              let list = IOPSCopyPowerSourcesList(info)?.takeRetainedValue() as? [CFTypeRef]
        else { return false }
        return list.contains { src in
            let d = IOPSGetPowerSourceDescription(info, src)?.takeUnretainedValue() as? [String: Any]
            return d?[kIOPSTypeKey] as? String == kIOPSInternalBatteryType
        }
    }

    public static func current() -> Machine {
        let model = sysctlString("hw.model") ?? "Mac"
        var uuid = [UInt8](repeating: 0, count: 16)
        var wait = timespec(tv_sec: 1, tv_nsec: 0)
        let id: String
        if gethostuuid(&uuid, &wait) == 0 {
            id = NSUUID(uuidBytes: uuid).uuidString.lowercased()
        } else {
            id = ProcessInfo.processInfo.hostName
        }
        let v = ProcessInfo.processInfo.operatingSystemVersion
        let os = "macOS \(v.majorVersion).\(v.minorVersion)" + (v.patchVersion > 0 ? ".\(v.patchVersion)" : "")
        return Machine(id: id, name: Host.current().localizedName ?? ProcessInfo.processInfo.hostName,
                       kind: kind(forModel: model, hasBattery: hasInternalBattery()), model: model, os: os)
    }

    static func sysctlString(_ name: String) -> String? {
        var size = 0
        guard sysctlbyname(name, nil, &size, nil, 0) == 0, size > 0 else { return nil }
        var buf = [UInt8](repeating: 0, count: size)
        guard sysctlbyname(name, &buf, &size, nil, 0) == 0 else { return nil }
        return String(decoding: buf.prefix { $0 != 0 }, as: UTF8.self)
    }
}

/// Claude Code wraps multi-line pastes in `<pasted_content id="…">…</pasted_content id="…">`.
public enum PastedContent {
    public static func strip(_ text: String) -> String {
        guard text.contains("pasted_content") else { return text }
        return text.replacingOccurrences(of: #"</?pasted_content[^>]*>\n?"#, with: "", options: .regularExpression)
            .trimmingCharacters(in: .whitespacesAndNewlines)
    }
}
