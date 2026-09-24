import Foundation

/// Pure parsing of the two providers' raw process output into `UsageProvider`. No I/O.
public enum UsageParsers {
    /// The `account/rateLimits/read` JSON-RPC response line. `piReady` is whether pi's `openai-codex`
    /// login checked out (same ChatGPT account) — adds `"pi"` to `usedBy` when true.
    public static func parseCodex(_ line: Data, now: Date, piReady: Bool = false) -> UsageProvider? {
        guard let root = (try? JSONSerialization.jsonObject(with: line)) as? [String: Any],
              let result = root["result"] as? [String: Any],
              let rateLimits = result["rateLimits"] as? [String: Any]
        else { return nil }
        var windows: [UsageWindow] = []
        if let w = window(rateLimits["primary"], id: "primary") { windows.append(w) }
        if let w = window(rateLimits["secondary"], id: "secondary") { windows.append(w) }
        let plan = (rateLimits["planType"] as? String).map(planLabel)
        return UsageProvider(id: "codex", label: "ChatGPT", plan: plan, windows: windows,
                             updatedAt: Timestamps.format(now), source: "codex app-server", stale: false,
                             usedBy: piReady ? ["codex", "pi"] : ["codex"])
    }

    private static func window(_ raw: Any?, id: String) -> UsageWindow? {
        guard let w = raw as? [String: Any] else { return nil }
        let percent = (w["usedPercent"] as? NSNumber)?.doubleValue
        let minutes = (w["windowDurationMins"] as? NSNumber)?.intValue
        let resetsAt = (w["resetsAt"] as? NSNumber).map { Timestamps.format(Date(timeIntervalSince1970: $0.doubleValue)) }
        return UsageWindow(id: id, label: windowLabel(minutes: minutes, id: id), usedPercent: percent,
                           windowMinutes: minutes, resetsAt: resetsAt)
    }

    /// A human label from the window's length when one is reported, else a generic fallback by id.
    static func windowLabel(minutes: Int?, id: String) -> String {
        guard let minutes, minutes > 0 else { return id == "primary" ? "Primary" : "Secondary" }
        if minutes <= 360 {
            let hours = max(1, minutes / 60)
            return "\(hours)-hour"
        }
        if minutes <= 10_080 { return "Weekly" }
        return "Monthly"
    }

    static func planLabel(_ raw: String) -> String {
        switch raw {
        case "free": return "Free"
        case "go": return "Go"
        case "plus": return "Plus"
        case "pro": return "Pro"
        case "team": return "Team"
        case "business": return "Business"
        case "enterprise": return "Enterprise"
        default: return raw.capitalized
        }
    }

    /// `claude -p "/usage" --output-format stream-json --verbose` stdout: one JSON object per line;
    /// the assistant message carries `usage_report.rate_limits.limits[]`. See api.md "Usage". `piReady`
    /// is whether pi's own `anthropic` login checked out — adds `"pi"` to `usedBy` when true.
    public static func parseClaude(usage usageData: Data?, auth authData: Data?, now: Date, piReady: Bool = false) -> UsageProvider? {
        guard let usageData else { return nil }
        var limits: [[String: Any]]?
        for lineData in usageData.split(separator: UInt8(ascii: "\n")) where lineData.count > 20 {
            guard let obj = try? JSONSerialization.jsonObject(with: Data(lineData)) as? [String: Any],
                  let report = obj["usage_report"] as? [String: Any],
                  let rateLimits = report["rate_limits"] as? [String: Any],
                  let l = rateLimits["limits"] as? [[String: Any]]
            else { continue }
            limits = l
        }
        guard let limits else { return nil }

        func claudeWindow(kind: String, label: String) -> UsageWindow? {
            guard let l = limits.first(where: { $0["kind"] as? String == kind }) else { return nil }
            let percent = (l["percent"] as? NSNumber)?.doubleValue
            let resetsAt = (l["resets_at"] as? String).flatMap(Timestamps.parse).map(Timestamps.format)
            return UsageWindow(id: kind, label: label, usedPercent: percent, windowMinutes: nil, resetsAt: resetsAt)
        }
        var windows: [UsageWindow] = []
        if let w = claudeWindow(kind: "session", label: "Session") { windows.append(w) }
        if let w = claudeWindow(kind: "weekly_all", label: "This week") { windows.append(w) }

        var plan: String?
        if let authData, let authObj = try? JSONSerialization.jsonObject(with: authData) as? [String: Any],
           let subscriptionType = authObj["subscriptionType"] as? String {
            plan = planLabel(subscriptionType)
        }
        return UsageProvider(id: "claude", label: "Claude", plan: plan, windows: windows,
                             updatedAt: Timestamps.format(now), source: "claude -p /usage", stale: false,
                             usedBy: piReady ? ["claude", "pi"] : ["claude"])
    }

    /// No usage/quota API exists for OpenCode Go (flat API-key plan, not OAuth) — see
    /// `docs/tasks/round-5/usage-pi-research.md`. Built entirely from `pi auth check`, never from a
    /// network call or the stored key. `nil` when pi isn't authenticated to it at all, so the card is
    /// omitted rather than shown empty.
    public static func openCodeGoProvider(piReady: Bool, now: Date) -> UsageProvider? {
        guard piReady else { return nil }
        return UsageProvider(id: "opencode-go", label: "OpenCode Go", plan: "OpenCode Go", windows: [],
                             updatedAt: Timestamps.format(now), source: "pi auth check", stale: false,
                             unavailableReason: "Usage not available from OpenCode", usedBy: ["pi"])
    }
}
