import Foundation

/// codex: no direct commands, so the bridge drives `/model` (model, then reasoning level, applied
/// with `s` = "for this session only") and `/permissions`. The footer reads `<Model> <effort> · <cwd>`.
struct CodexDriver: ControlDriver {
    static let modes = [ControlChoice("ask", "Ask for approval"), ControlChoice("approveForMe", "Approve for me"), ControlChoice("fullAccess", "Full Access")]
    static let effortWords = ["none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"]

    private static let footerLine = try! NSRegularExpression(pattern: #"^\s*(\S+)\s+(none|minimal|low|medium|high|xhigh|max|ultra)\s+·"#)

    /// `GPT-5.6-Terra high · <cwd>` → (token, effort). The token is a display name or a slug.
    static func footer(_ screen: String) -> (model: String, effort: String)? {
        for line in screen.components(separatedBy: "\n").suffix(6).reversed() {
            let ns = line as NSString
            guard let m = footerLine.firstMatch(in: line, range: NSRange(location: 0, length: ns.length)) else { continue }
            return (ns.substring(with: m.range(at: 1)), ns.substring(with: m.range(at: 2)))
        }
        return nil
    }

    static func slug(for token: String, in catalog: [ModelCatalogs.CodexModel]) -> String {
        catalog.first { $0.displayName.lowercased() == token.lowercased() || $0.slug == token.lowercased() }?.slug ?? token.lowercased()
    }

    struct TurnContext: Equatable {
        var model: String?
        var effort: String?
        var mode: String?
        var at: Date?
    }

    /// The last `turn_context` in a rollout.
    static func lastTurnContext(_ data: Data) -> TurnContext? {
        var out: TurnContext?
        for line in data.split(separator: UInt8(ascii: "\n")) where line.count > 20 {
            guard let o = (try? JSONSerialization.jsonObject(with: Data(line))) as? [String: Any],
                  o["type"] as? String == "turn_context", let p = o["payload"] as? [String: Any] else { continue }
            let sandbox = (p["sandbox_policy"] as? [String: Any])?["type"] as? String
            let mode = sandbox == "danger-full-access" ? "fullAccess" : (p["approvals_reviewer"] as? String == "auto_review" ? "approveForMe" : "ask")
            out = TurnContext(model: p["model"] as? String, effort: p["effort"] as? String, mode: mode,
                              at: (o["timestamp"] as? String).flatMap(Timestamps.parse))
        }
        return out
    }

    func read(_ a: HerdrAgent, ref: TranscriptRef?, screen: String?, service: AgentService) async -> ControlState {
        let catalog = service.catalogs.codex()
        var s = ControlState()
        let ctx = a.agentSession.flatMap { service.catalogs.codexRollout($0.value) }.flatMap { Self.lastTurnContext(service.readTail($0)) }
        if let f = screen.flatMap(Self.footer) {
            s.model = Self.slug(for: f.model, in: catalog)
            s.effort = f.effort
        } else {
            s.model = ctx?.model
            s.effort = ctx?.effort
        }
        // turn_context is only written per turn; a mode the bridge just set wins until a newer turn.
        s.permissionMode = service.stickyMode(a.paneId, session: a.agentSession?.value, newerThan: ctx?.at) ?? ctx?.mode
        return s
    }

    func label(forModel id: String, service: AgentService) -> String? {
        service.catalogs.codex().first { $0.slug == id }?.displayName ?? id
    }

    func capabilities(_ a: HerdrAgent, current: ControlState, service: AgentService) async -> AgentControls {
        let catalog = service.catalogs.codex()
        var models = catalog.filter(\.visible).map { ControlChoice($0.slug, $0.displayName) }
        if let cur = current.model, !models.contains(where: { $0.id == cur }) {
            models.insert(ControlChoice(cur, catalog.first { $0.slug == cur }?.displayName ?? cur), at: 0)
        }
        // The effort step needs the current model's row in the picker.
        let entry = catalog.first { $0.slug == current.model && $0.visible }
        let efforts = Effort.choices(entry?.efforts ?? [])
        return AgentControls(models: models, efforts: efforts, modes: Self.modes,
                             supports: .init(model: catalog.contains(where: \.visible), effort: !efforts.isEmpty, mode: true, compact: true, clear: true))
    }

    func apply(_ request: ControlRequest, to a: HerdrAgent, current: ControlState, service: AgentService) async throws {
        let catalog = service.catalogs.codex()
        switch request {
        case .model(let slug):
            guard let entry = catalog.first(where: { $0.slug == slug && $0.visible }) else {
                throw APIError(.badRequest, "unsupported", "\(slug) isn't in codex's /model picker, so it can't be selected")
            }
            let effort = current.effort.flatMap { entry.efforts.contains($0) ? $0 : nil } ?? entry.defaultEffort ?? entry.efforts.first ?? "medium"
            try await pickModel(a, entry, effort: effort, service: service)
        case .effort(let level):
            guard let entry = catalog.first(where: { $0.slug == current.model && $0.visible }) else {
                throw APIError(.badRequest, "unsupported", "the current model isn't in codex's /model picker, so its effort can't be changed")
            }
            try await pickModel(a, entry, effort: level, service: service)
        case .permissionMode(let mode):
            let label = Self.modes.first { $0.id == mode }!.label
            let before = try await service.screen(a)
            let requested = "Permission selection requested: \(label)"
            try await service.submit(a, "/permissions")
            try await service.drivePicker(a, title: "Permissions", pick: { $0 == label }, key: "enter", what: label)
            try await service.waitFor(AgentService.controlTimeout, what: "/permissions \(label)") {
                let screen = try await service.screen(a)
                // Full Access can ask to confirm first.
                if let p = Picker.parse(screen), p.title?.contains("Permissions") != true,
                   let yes = p.labels.firstIndex(where: { $0.hasPrefix("Yes") || $0.hasPrefix("Continue") || $0.hasPrefix("Allow") }) {
                    try await service.drivePicker(a, title: p.title ?? "", pick: { $0 == p.labels[yes] }, key: "enter", what: p.labels[yes])
                    return false
                }
                return AgentService.count(requested, in: screen) > AgentService.count(requested, in: before)
            }
            service.setStickyMode(a.paneId, mode, session: a.agentSession?.value)
        case .compact:
            let before = try await service.screen(a)
            let done = "Context compacted"
            try await service.submit(a, "/compact")
            try await service.waitForScreen(a, before: before, timeout: AgentService.compactTimeout, what: "/compact") {
                AgentService.count(done, in: $0) > AgentService.count(done, in: before)
            }
        case .clear:
            let before = try await service.screen(a)
            let resume = "To continue this session, run codex resume"
            try await service.submit(a, "/new")
            // "Where should the new conversation run?" appears in git checkouts.
            if (try? await service.drivePicker(a, title: "Where should the new conversation run", pick: { $0.hasPrefix("Current checkout") }, key: "enter", what: "Current checkout")) == nil {
                // No picker: /new went straight through.
            }
            service.dropConfirmed(a.paneId)
            service.setStickyMode(a.paneId, nil, session: nil)
            try await service.waitForScreen(a, before: before, what: "/new") {
                AgentService.count(resume, in: $0) > AgentService.count(resume, in: before)
            }
        }
    }

    func pickModel(_ a: HerdrAgent, _ entry: ModelCatalogs.CodexModel, effort: String, service: AgentService) async throws {
        let before = try await service.screen(a)
        do {
            try await choose(a, entry, effort: effort, service: service)
        } catch {
            // Never leave a picker open: a later Enter would commit it as the saved default.
            if Picker.parse(try await service.screen(a)) != nil {
                try? await service.herdr.sendKeys(a.paneId, keys: ["esc"])
                try? await Task.sleep(for: .milliseconds(200))
                if Picker.parse(try await service.screen(a)) != nil { try? await service.herdr.sendKeys(a.paneId, keys: ["esc"]) }
            }
            throw error
        }
        // The footer shows the display name (or slug) and effort once applied.
        try await service.waitForScreen(a, before: before, what: "/model \(entry.slug) \(effort)") { s in
            guard let f = Self.footer(s) else { return false }
            return (f.model.lowercased() == entry.displayName.lowercased() || f.model.lowercased() == entry.slug) && f.effort == effort
        }
        service.holdConfirmed(a.paneId, ControlState(model: entry.slug, effort: effort))
    }

    func choose(_ a: HerdrAgent, _ entry: ModelCatalogs.CodexModel, effort: String, service: AgentService) async throws {
        try await service.submit(a, "/model")
        try await service.drivePicker(a, title: "Select Model", pick: { $0 == entry.displayName }, key: "enter", what: entry.displayName)
        let label = Effort.labels[effort] ?? effort.capitalized
        let screen = try await waitForPicker(a, title: "Reasoning Level", service: service)
        if !screen.labels.contains(label) {
            // Max and Ultra sit behind "More reasoning…".
            try await service.drivePicker(a, title: "Reasoning Level", pick: { $0.hasPrefix("More reasoning") }, key: "enter", what: "More reasoning")
            // Wait for the submenu itself (the old picker can linger for a frame).
            var found = false
            for _ in 0..<30 where !found {
                try await Task.sleep(for: .milliseconds(100))
                found = Picker.parse(try await service.screen(a))?.labels.contains(label) == true
            }
            try await service.drivePicker(a, title: "", pick: { $0 == label }, key: "s", what: label)
        } else {
            try await service.drivePicker(a, title: "Reasoning Level", pick: { $0 == label }, key: "s", what: label)
        }
    }

    func waitForPicker(_ a: HerdrAgent, title: String, service: AgentService) async throws -> Picker {
        for _ in 0..<30 {
            if let p = Picker.parse(try await service.screen(a)), AgentService.titleMatches(p, title) { return p }
            try await Task.sleep(for: .milliseconds(150))
        }
        throw APIError(.gatewayTimeout, "control_timeout", "the \(title) picker never opened")
    }
}
