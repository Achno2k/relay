import Foundation

/// pi: `/model <provider>/<id>` and `/thinking <level>` switch directly (session only; pi saves a
/// default only on Ctrl+S). The footer's right side is `(<provider>) <id> • <thinking>`.
struct PiDriver: ControlDriver {
    static let efforts = ["off", "minimal", "low", "medium", "high", "xhigh", "max"]

    private static let footer = try! NSRegularExpression(pattern: #"\(([\w.\-]+)\)\s+([\w.:\-]+)(?:\s+•\s+(?:thinking\s+)?([a-z]+))?\s*$"#)

    /// Model and thinking from pi's footer (`• high`, or `• thinking off`).
    static func footer(_ screen: String) -> ControlState? {
        for line in screen.components(separatedBy: "\n").suffix(8).reversed() {
            let ns = line as NSString
            guard let m = footer.firstMatch(in: line, range: NSRange(location: 0, length: ns.length)) else { continue }
            let effort = m.range(at: 3).location == NSNotFound ? nil : ns.substring(with: m.range(at: 3))
            return ControlState(model: ns.substring(with: m.range(at: 1)) + "/" + ns.substring(with: m.range(at: 2)),
                                effort: effort.flatMap { efforts.contains($0) ? $0 : nil })
        }
        return nil
    }

    /// `Error: Unknown thinking level "off". Available levels: minimal, low, medium, high, xhigh, max.`
    static func availableLevels(_ line: String) -> [String] {
        guard let r = line.range(of: "Available levels:") else { return [] }
        return line[r.upperBound...].trimmingCharacters(in: CharacterSet(charactersIn: " .")).components(separatedBy: ",")
            .map { $0.trimmingCharacters(in: .whitespaces) }.filter { !$0.isEmpty }
    }

    /// Last `model_change` / `thinking_level_change` in a pi session file.
    static func scan(_ data: Data) -> ControlState {
        var s = ControlState()
        for line in data.split(separator: UInt8(ascii: "\n")) {
            guard let o = (try? JSONSerialization.jsonObject(with: Data(line))) as? [String: Any] else { continue }
            switch o["type"] as? String {
            case "model_change":
                if let p = o["provider"] as? String, let m = o["modelId"] as? String { s.model = "\(p)/\(m)" }
            case "thinking_level_change":
                if let t = o["thinkingLevel"] as? String { s.effort = t }
            default: break
            }
        }
        return s
    }

    func read(_ a: HerdrAgent, ref: TranscriptRef?, screen: String?, service: AgentService) async -> ControlState {
        let file = ref.map { Self.scan(service.readTail($0.url)) } ?? ControlState()
        return (screen.flatMap(Self.footer) ?? ControlState()).merged(over: file)
    }

    func label(forModel id: String, service: AgentService) -> String? {
        id.split(separator: "/", maxSplits: 1).last.map(String.init)
    }

    /// The saved default and scoped models first, then everything else in `--list-models` order.
    static func ordered(_ models: [ModelCatalogs.PiModel], settings: ModelCatalogs.PiSettings, current: String?) -> [ControlChoice] {
        func matches(_ m: ModelCatalogs.PiModel, _ pattern: String) -> Bool {
            let p = pattern.split(separator: ":").first.map(String.init) ?? pattern
            if p.hasSuffix("*") { return m.full.hasPrefix(String(p.dropLast())) || m.id.hasPrefix(String(p.dropLast())) }
            return m.full == p || m.id == p
        }
        var first: [ModelCatalogs.PiModel] = []
        if let d = settings.defaultModel, let m = models.first(where: { $0.full == d }) { first.append(m) }
        for p in settings.enabledModels {
            for m in models where matches(m, p) && !first.contains(m) { first.append(m) }
        }
        let rest = models.filter { !first.contains($0) }
        let ids = Dictionary(grouping: models, by: \.id).mapValues(\.count)
        var out = (first + rest).map { m in
            ControlChoice(m.full, (ids[m.id] ?? 0) > 1 ? "\(m.id) (\(m.provider))" : m.id)
        }
        if let current, !out.contains(where: { $0.id == current }) {
            out.insert(ControlChoice(current, current.split(separator: "/").last.map(String.init) ?? current), at: 0)
        }
        return out
    }

    func capabilities(_ a: HerdrAgent, current: ControlState, service: AgentService) async -> AgentControls {
        let models = service.catalogs.pi()
        let thinking = current.model.flatMap { cur in models.first { $0.full == cur }?.thinking } ?? true
        // Per model: e.g. claude-fable-5 has no "off", gpt-5.6-sol has off…max.
        let levels = current.model.flatMap(service.catalogs.piLevels) ?? (thinking ? Self.efforts : ["off"])
        return AgentControls(
            models: Self.ordered(models, settings: service.catalogs.piSettings(), current: current.model),
            efforts: Effort.choices(levels),
            modes: [],
            supports: .init(model: !models.isEmpty || current.model != nil, effort: levels.count > 1, mode: false, compact: true, clear: true))
    }

    func kindControls(service: AgentService) -> AgentControls {
        let models = service.catalogs.pi()
        let settings = service.catalogs.piSettings()
        func levels(_ id: String) -> [String] {
            service.catalogs.piLevels(id) ?? ((models.first { $0.full == id }?.thinking ?? true) ? Self.efforts : ["off"])
        }
        let ordered = Self.ordered(models, settings: settings, current: nil)
        let byModel = Dictionary(uniqueKeysWithValues: ordered.map { ($0.id, Effort.choices(levels($0.id))) })
        let def = settings.defaultModel
        return AgentControls(
            models: ordered, efforts: Effort.choices(def.map(levels) ?? Self.efforts), modes: [],
            supports: .init(model: !models.isEmpty, effort: true, mode: false, compact: true, clear: true),
            defaultModel: def, defaultEffort: settings.defaultThinking, effortsByModel: byModel)
    }

    func launchArgs(model: String?, effort: String?) -> [String] {
        if let model { return ["--model", effort.map { "\(model):\($0)" } ?? model] }
        return effort.map { ["--thinking", $0] } ?? []
    }

    func apply(_ request: ControlRequest, to a: HerdrAgent, current: ControlState, service: AgentService) async throws {
        switch request {
        case .model(let id):
            try await service.submit(a, "/model \(id)")
            try await service.waitFor(AgentService.controlTimeout, what: "/model \(id)") {
                Self.footer(try await service.screen(a))?.model == id
            }
            // Switching model resets thinking to that model's default; the footer shows it.
            service.holdConfirmed(a.paneId, ControlState(model: id))
        case .effort(let level):
            let before = try await service.screen(a)
            let unknown = "Unknown thinking level"
            try await service.submit(a, "/thinking \(level)")
            try await service.waitFor(AgentService.controlTimeout, what: "/thinking \(level)") {
                let screen = try await service.screen(a)
                if AgentService.count(unknown, in: screen) > AgentService.count(unknown, in: before),
                   let line = screen.components(separatedBy: "\n").last(where: { $0.contains(unknown) }) {
                    let available = Self.availableLevels(line)
                    if let model = current.model, !available.isEmpty { service.catalogs.learnPiLevels(model, available) }
                    throw APIError(.badRequest, "unsupported",
                                   "\(current.model.flatMap { label(forModel: $0, service: service) } ?? "this model") doesn't support thinking \(level); available: \(available.joined(separator: ", "))")
                }
                return Self.footer(screen)?.effort == level
            }
            service.holdConfirmed(a.paneId, ControlState(effort: level))
        case .compact:
            let ref = service.locator.locate(a)
            let start = ref.map { service.fileSize($0.url) }
            let before = try await service.screen(a)
            let failed = "Compaction failed"
            try await service.submit(a, "/compact")
            try await service.waitFor(AgentService.compactTimeout, what: "/compact") {
                if let ref, let start, service.read(ref.url, from: start).contains("\"compaction\"") { return true }
                // Only a new failure line counts (e.g. "Nothing to compact (session too small)").
                let screen = try await service.screen(a)
                if AgentService.count(failed, in: screen) > AgentService.count(failed, in: before),
                   let err = screen.components(separatedBy: "\n").last(where: { $0.contains(failed) }) {
                    throw APIError(.badGateway, "control_failed", err.trimmingCharacters(in: .whitespaces))
                }
                return false
            }
        case .clear:
            let before = a.agentSession?.value
            try await service.submit(a, "/new")
            service.dropConfirmed(a.paneId)
            try await service.waitFor(AgentService.controlTimeout, what: "/new") {
                let now = try await service.herdr.agent(a.paneId).agentSession?.value
                return now != nil && now != before
            }
        case .permissionMode:
            throw APIError(.badRequest, "unsupported", "pi has no permission modes")
        }
    }
}
