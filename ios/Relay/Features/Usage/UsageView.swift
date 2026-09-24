import RelayKit
import SwiftUI

/// Subscription usage for Claude and Codex/pi. See api.md "Usage".
struct UsageView: View {
    @Bindable var store: UsageStore
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(spacing: 16) {
                    if store.isLoading && store.providers.isEmpty {
                        ProgressView().padding(.top, 60)
                    } else if store.providers.isEmpty {
                        ContentUnavailableView(
                            "No usage yet", systemImage: "gauge",
                            description: Text(store.errorMessage ?? "Pull to refresh once the bridge has had a chance to check.")
                        )
                        .padding(.top, 40)
                    } else {
                        ForEach(store.providers) { UsageProviderCard(provider: $0) }
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

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            header
            if provider.windows.isEmpty {
                Text(provider.unavailableReason ?? "No usage data yet.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
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
