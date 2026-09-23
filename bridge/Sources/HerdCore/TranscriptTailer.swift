import Foundation

/// Watches one transcript file and emits messages that were created or grew.
/// Reads only appended bytes; a partial trailing line waits for the rest.
public final class TranscriptTailer: @unchecked Sendable {
    public let url: URL
    private let queue: DispatchQueue
    private let onMessage: @Sendable (Message) -> Void
    // Everything below is only touched on `queue`.
    private var parser: TranscriptParser
    private var offset: UInt64 = 0
    private var partial = Data()
    private var source: DispatchSourceFileSystemObject?
    private var handle: FileHandle?
    private var _dead = false

    public init(url: URL, format: TranscriptFormat, cwd: String?, onMessage: @escaping @Sendable (Message) -> Void) {
        self.url = url
        self.parser = TranscriptParser(format: format, cwd: cwd)
        self.onMessage = onMessage
        self.queue = DispatchQueue(label: "herd.tail.\(url.lastPathComponent)")
    }

    /// True once the file was deleted or renamed; the owner should recreate the tailer.
    public var isDead: Bool { queue.sync { _dead } }

    public func start() {
        queue.sync {
            guard let handle = try? FileHandle(forReadingFrom: url) else {
                _dead = true
                return
            }
            self.handle = handle
            // Seed the parser with what exists; only new growth is emitted.
            _ = readAppended()
            let fd = open(url.path, O_EVTONLY)
            guard fd >= 0 else {
                _dead = true
                return
            }
            let src = DispatchSource.makeFileSystemObjectSource(fileDescriptor: fd, eventMask: [.extend, .write, .delete, .rename, .revoke], queue: queue)
            src.setEventHandler { [weak self] in
                guard let self, let src = self.source else { return }
                let ev = src.data
                if !ev.intersection([.delete, .rename, .revoke]).isEmpty {
                    self._dead = true
                    src.cancel()
                    return
                }
                for m in self.readAppended() { self.onMessage(m) }
            }
            src.setCancelHandler { close(fd) }
            source = src
            src.resume()
        }
    }

    public func stop() {
        queue.sync {
            source?.cancel()
            source = nil
            try? handle?.close()
            handle = nil
            _dead = true
        }
    }

    deinit {
        source?.cancel()
    }

    /// Reads bytes past `offset`, feeds complete lines to the parser, returns changed messages.
    private func readAppended() -> [Message] {
        guard let handle else { return [] }
        let size = (try? handle.seekToEnd()) ?? 0
        if size < offset {
            // Truncated or replaced: start over.
            parser = TranscriptParser(format: parser.format, cwd: parser.scrubber.cwd)
            offset = 0
            partial.removeAll()
        }
        guard size > offset else { return [] }
        do {
            try handle.seek(toOffset: offset)
            guard let data = try handle.read(upToCount: Int(size - offset)) else { return [] }
            offset += UInt64(data.count)
            partial.append(data)
        } catch {
            return []
        }
        guard let lastNewline = partial.lastIndex(of: UInt8(ascii: "\n")) else { return [] }
        let complete = partial[partial.startIndex...lastNewline]
        let changed = parser.consume(data: Data(complete))
        partial = Data(partial[partial.index(after: lastNewline)...])
        return changed
    }
}
