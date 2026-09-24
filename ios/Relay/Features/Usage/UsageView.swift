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

private struct UsageWindowRow: View {
    let window: UsageWindow

    private var percent: Double { min(100, max(0, window.usedPercent ?? 0)) }

    private var level: Color {
        switch percent {
        case ..<60: .green
        case ..<85: .orange
        default: .red
        }
    }

    var body: some View {
        HStack(spacing: 14) {
            UsageRing(percent: percent, color: level)
                .frame(width: 40, height: 40)
            VStack(alignment: .leading, spacing: 2) {
                Text(window.label).font(.subheadline.weight(.medium))
                if let resetsAt = window.resetsAt {
                    Text("Resets \(Self.countdown(to: resetsAt))")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            Spacer()
            if window.usedPercent != nil {
                Text("\(Int(percent.rounded()))%")
                    .font(.subheadline.monospacedDigit().weight(.semibold))
                    .foregroundStyle(level)
            } else {
                Text("—").font(.subheadline).foregroundStyle(.secondary)
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

private struct UsageRing: View {
    let percent: Double
    let color: Color

    var body: some View {
        ZStack {
            Circle().stroke(color.opacity(0.18), lineWidth: 5)
            Circle()
                .trim(from: 0, to: percent / 100)
                .stroke(color, style: StrokeStyle(lineWidth: 5, lineCap: .round))
                .rotationEffect(.degrees(-90))
        }
        .animation(.smooth, value: percent)
    }
}
