import RelayKit
import SwiftUI

/// Subscription usage for Claude and Codex/pi, one card per subscription across machines. See api.md
/// "Usage" and "Multiple machines".
struct UsageView: View {
    @Bindable var store: UsageStore
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(spacing: 16) {
                    let cards = store.cards
                    if store.isLoading && cards.isEmpty {
                        ProgressView().padding(.top, 60)
                    } else if cards.isEmpty {
                        ContentUnavailableView(
                            "No usage yet", systemImage: "gauge",
                            description: Text(store.errorMessage ?? "Pull to refresh once the bridge has had a chance to check.")
                        )
                        .padding(.top, 40)
                    } else {
                        ForEach(cards) { card in
                            UsageProviderCard(
                                provider: card.provider,
                                machines: store.showsMachines ? card.machineNames : []
                            )
                        }
                    }
                }
                .padding(16)
            }
            .background(Color(.systemGroupedBackground))
            .navigationTitle("Usage")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) {
                    Button("Done") { dismiss() }
                }
            }
            .refreshable { await store.refresh() }
            .task { await store.load() }
        }
    }
}

private struct UsageProviderCard: View {
    let provider: UsageProvider
    /// The machines on this subscription; empty with only one machine paired.
    var machines: [String] = []

    /// A permanently-unavailable card (e.g. OpenCode Go: no fetch exists, so nothing can go stale) reads
    /// calm and settled, not like a failed check — no dimming, no "Stale" badge, a neutral icon.
    private var isCalmUnavailable: Bool { provider.windows.isEmpty && !provider.stale && provider.unavailableReason != nil }

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            header
            if provider.windows.isEmpty {
                Label(provider.unavailableReason ?? "No usage data yet.", systemImage: isCalmUnavailable ? "info.circle" : "clock.arrow.circlepath")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .labelStyle(.titleAndIcon)
            } else {
                ForEach(provider.windows) { UsageWindowRow(window: $0) }
            }
            footer
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
        .glassEffect(.regular, in: .rect(cornerRadius: 24))
        .opacity(provider.stale ? 0.6 : 1)
        .accessibilityElement(children: .combine)
    }

    private var header: some View {
        HStack(alignment: .firstTextBaseline) {
            VStack(alignment: .leading, spacing: 2) {
                Text(provider.label).font(.headline)
                if let plan = provider.plan {
                    Text(plan).font(.footnote).foregroundStyle(.secondary)
                }
                if !provider.usedBy.isEmpty {
                    UsedByRow(usedBy: provider.usedBy)
                }
                if !machines.isEmpty {
                    Label(machines.joined(separator: " · "), systemImage: machines.count > 1 ? "desktopcomputer.and.macbook" : "desktopcomputer")
                        .font(.caption2.weight(.medium))
                        .foregroundStyle(.secondary)
                        .labelStyle(.titleAndIcon)
                        .accessibilityLabel(machines.count > 1 ? "On \(machines.joined(separator: ", "))" : "On \(machines[0])")
                        .accessibilityIdentifier("usageMachines")
                }
            }
            Spacer()
            if provider.stale {
                Label("Stale", systemImage: "exclamationmark.triangle")
                    .font(.caption.weight(.medium))
                    .foregroundStyle(.orange)
                    .labelStyle(.titleAndIcon)
            }
        }
    }

    private var footer: some View {
        Text("Updated \(RelativeTime.short(provider.updatedAt))")
            .font(.caption)
            .foregroundStyle(.tertiary)
    }
}

/// "Used by ⌾ Claude Code · π pi" — which harnesses on this Mac draw on this subscription.
private struct UsedByRow: View {
    let usedBy: [String]

    var body: some View {
        HStack(spacing: 4) {
            Text("Used by").foregroundStyle(.tertiary)
            ForEach(Array(usedBy.enumerated()), id: \.offset) { index, kind in
                if index > 0 { Text("·").foregroundStyle(.tertiary) }
                UsedByGlyph(kind: kind)
                Text(Self.name(kind))
            }
        }
        .font(.caption2.weight(.medium))
        .foregroundStyle(.secondary)
        .padding(.top, 1)
    }

    static func name(_ kind: String) -> String {
        switch kind {
        case "claude": "Claude Code"
        case "codex": "Codex"
        case "pi": "pi"
        default: kind.capitalized
        }
    }
}

private struct UsedByGlyph: View {
    let kind: String

    var body: some View {
        Group {
            switch kind {
            case "pi":
                Text("π").font(.system(size: 11, weight: .semibold, design: .serif))
            case "codex":
                Image(systemName: "chevron.left.forwardslash.chevron.right").font(.system(size: 10, weight: .semibold))
            case "claude":
                Image(systemName: "sparkle").font(.system(size: 11, weight: .semibold))
            default:
                Image(systemName: "cpu").font(.system(size: 10, weight: .semibold))
            }
        }
        .frame(width: 12)
        .accessibilityHidden(true)
    }
}

/// Never green — a usage bar isn't a "good/bad" gauge, just neutral until it's actually high.
enum UsageLevel: Equatable {
    case neutral, amber, red

    static func classify(_ percent: Double) -> UsageLevel {
        switch percent {
        case ..<75: .neutral
        case ..<90: .amber
        default: .red
        }
    }

    var color: Color {
        switch self {
        case .neutral: .accentColor
        case .amber: .orange
        case .red: .red
        }
    }
}

struct UsageWindowRow: View {
    let window: UsageWindow

    private var percent: Double { min(100, max(0, window.usedPercent ?? 0)) }
    private var level: Color { UsageLevel.classify(percent).color }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline) {
                Text(window.label).font(.subheadline.weight(.medium))
                Spacer()
                if window.usedPercent != nil {
                    Text("\(Int(percent.rounded()))%")
                        .font(.subheadline.monospacedDigit().weight(.semibold))
                        .foregroundStyle(level)
                } else {
                    Text("—").font(.subheadline).foregroundStyle(.secondary)
                }
            }
            UsageBar(percent: percent, color: level)
            if let resetsAt = window.resetsAt {
                Text("Resets \(Self.countdown(to: resetsAt))")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .accessibilityElement(children: .combine)
    }

    /// "in 2h", "in 3d", "soon" for anything under a minute out (or already past, which a stale
    /// snapshot can show briefly until the next poll catches up).
    static func countdown(to date: Date, now: Date = Date()) -> String {
        let seconds = date.timeIntervalSince(now)
        if seconds < 60 { return "soon" }
        if seconds < 3600 { return "in \(Int(seconds / 60))m" }
        if seconds < 86_400 { return "in \(Int(seconds / 3600))h" }
        return "in \(Int(seconds / 86_400))d"
    }
}

/// A thin rounded percentage bar. Fills with a spring on appear and whenever `percent` changes.
private struct UsageBar: View {
    let percent: Double
    let color: Color
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    @State private var fill: CGFloat = 0

    private static let trackHeight: CGFloat = 6

    var body: some View {
        GeometryReader { geo in
            ZStack(alignment: .leading) {
                Capsule().fill(color.opacity(0.16))
                Capsule().fill(color)
                    .frame(width: geo.size.width * fill)
            }
        }
        .frame(height: Self.trackHeight)
        .onAppear { setFill(animated: !reduceMotion) }
        .onChange(of: percent) { _, _ in setFill(animated: !reduceMotion) }
        .onChange(of: reduceMotion) { _, _ in setFill(animated: false) }
    }

    private func setFill(animated: Bool) {
        let target = CGFloat(percent / 100)
        if animated {
            withAnimation(.spring(duration: 0.6, bounce: 0.2)) { fill = target }
        } else {
            fill = target
        }
    }
}
