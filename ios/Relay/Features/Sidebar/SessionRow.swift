import RelayKit
import SwiftUI

/// One chat in a sidebar card. Two shapes from the design:
/// - `showsProject`: 60 pt, status glyph, title over "project · 2m" (Now, filter and search lists).
/// - otherwise: 50 pt, title with a trailing age (project cards). Idle chats get no glyph.
/// Swipe or long-press to archive; long-press to stop a working agent.
struct SessionRow: View {
    let agent: Agent
    let store: AppStore
    let showsProject: Bool
    let onSelect: (String) -> Void
    /// A hairline under the row; off for the last row in a card.
    var separator = true

    @Environment(\.dynamicTypeSize) private var typeSize
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    /// One line as designed; up to three at accessibility sizes, where one line holds a word or two.
    private var titleLines: Int { typeSize.isAccessibilitySize ? 3 : 1 }

    var body: some View {
        let unseen = store.isUnseen(agent)
        let selected = agent.id == store.selectedAgentId
        let archived = store.isArchived(agent.id)
        Button { onSelect(agent.id) } label: {
            Group {
                if showsProject { twoLine(unseen: unseen) } else { compact(unseen: unseen) }
            }
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        // Two-line rows: the hairline starts under the text (18 inset + 18 glyph + 14 gap).
        .sidebarCardRow(selected: selected, leading: showsProject ? 18 : 20, separator: separator ? (showsProject ? 32 : 0) : nil)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(agent.displayTitle)
        .accessibilityValue(accessibilityValue(unseen: unseen))
        .accessibilityAddTraits(selected ? [.isButton, .isSelected] : .isButton)
        .accessibilityIdentifier("session-\(agent.id)")
        .swipeActions(edge: .trailing, allowsFullSwipe: true) {
            Button { setArchived(!archived) } label: {
                Label(archived ? "Unarchive" : "Archive", systemImage: archived ? "tray.and.arrow.up" : "archivebox")
            }
            .tint(archived ? .blue : .indigo)
        }
        .contextMenu {
            Button { setArchived(!archived) } label: {
                Label(archived ? "Unarchive" : "Archive", systemImage: archived ? "tray.and.arrow.up" : "archivebox")
            }
            Button(role: .destructive) { store.interrupt(agent.id) } label: {
                Label("Stop", systemImage: "stop.circle")
            }
            .disabled(agent.status != .working)
        } preview: {
            AgentPreview(agent: agent)
        }
    }

    private func twoLine(unseen: Bool) -> some View {
        HStack(spacing: 14) {
            StatusGlyph(status: agent.status, unseen: unseen)
            // SwiftUI's line heights run taller than the design's; -2 brings the gap back to it.
            VStack(alignment: .leading, spacing: -2) {
                Text(agent.displayTitle)
                    .font(.body)
                    .lineLimit(titleLines)
                Text("\(agent.workspaceName) · \(RelativeTime.compact(agent.updatedAt))")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(typeSize.isAccessibilitySize ? 2 : 1)
            }
            Spacer(minLength: 0)
        }
        .padding(.vertical, 8)
        .frame(minHeight: 60)
    }

    private func compact(unseen: Bool) -> some View {
        HStack(spacing: 8) {
            if agent.status != .idle {
                StatusGlyph(status: agent.status, unseen: unseen)
                    .padding(.trailing, 2)
            }
            Text(agent.displayTitle)
                .font(.body)
                .lineLimit(titleLines)
            Spacer(minLength: 0)
            Text(RelativeTime.compact(agent.updatedAt))
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
        .padding(.vertical, 8)
        .frame(minHeight: 50)
    }

    private func accessibilityValue(unseen: Bool) -> String {
        var parts = [StatusGlyph.label(agent.status, unseen: unseen)]
        if showsProject { parts.append(agent.workspaceName) }
        parts.append(RelativeTime.compact(agent.updatedAt))
        return parts.joined(separator: ", ")
    }

    private func setArchived(_ value: Bool) {
        withAnimation(reduceMotion ? nil : .smooth) { store.setArchived(agent.id, value) }
    }
}

/// Flat list for a filter other than All, or search results: a header and one card of two-line rows.
struct SidebarSessionList: View {
    let title: String
    let agents: [Agent]
    let emptyText: String
    let store: AppStore
    let onSelect: (String) -> Void

    var body: some View {
        Section {
            if agents.isEmpty {
                Text(emptyText)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .frame(maxWidth: .infinity, minHeight: 50, alignment: .leading)
                    .sidebarCardRow(leading: 20, separator: nil)
            }
            ForEach(agents) { agent in
                SessionRow(agent: agent, store: store, showsProject: true, onSelect: onSelect, separator: agent.id != agents.last?.id)
            }
        } header: {
            SidebarSectionHeader(top: 16) {
                Text(title)
                    .font(.headline)
                    .accessibilityAddTraits(.isHeader)
                Spacer(minLength: 8)
                if !agents.isEmpty {
                    Text("\(agents.count)")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                }
            }
        }
    }
}

// MARK: - Shared pieces

/// Row chrome inside a card: design insets, the `surface` fill, a highlight for the open chat, and a
/// hairline under the row. The list's own separators come out 1 pt; the design's are a hairline.
struct SidebarCardRowModifier: ViewModifier {
    var selected = false
    var leading: CGFloat
    /// Where the hairline starts, from the row's content edge; nil for none (the card's last row).
    var separator: CGFloat?
    static let trailing: CGFloat = 18

    @Environment(\.displayScale) private var displayScale

    func body(content: Content) -> some View {
        content
            .overlay(alignment: .bottom) {
                if let separator {
                    Rectangle()
                        .fill(Color(.separator))
                        .frame(height: 1 / displayScale)
                        // Runs to the card's edge, past the trailing inset.
                        .padding(.leading, separator)
                        .padding(.trailing, -Self.trailing)
                        .accessibilityHidden(true)
                }
            }
            .listRowSeparator(.hidden)
            .listRowInsets(EdgeInsets(top: 0, leading: leading, bottom: 0, trailing: Self.trailing))
            .listRowBackground(
                Color(.secondarySystemGroupedBackground)
                    .overlay(selected ? Color(.systemFill) : .clear)
            )
    }
}

extension View {
    func sidebarCardRow(selected: Bool = false, leading: CGFloat, separator: CGFloat?) -> some View {
        modifier(SidebarCardRowModifier(selected: selected, leading: leading, separator: separator))
    }
}

/// A section header above a card: `top` pt of space, a 36 pt row 20 pt in from the card edge
/// (36 from the sidebar edge), then 4 pt to the card.
struct SidebarSectionHeader<Content: View>: View {
    var top: CGFloat = 22
    @ViewBuilder let content: Content

    var body: some View {
        HStack(spacing: 8) { content }
            .foregroundStyle(.primary)
            .textCase(nil)
            .frame(minHeight: 36)
            .padding(.leading, 20)
            .padding(.trailing, 20)
            .padding(.top, top)
            .padding(.bottom, 4)
            .listRowInsets(EdgeInsets())
    }
}

/// Remembered sidebar expansion (Show all, completed folds, opened all-in-Now projects).
/// Kept as one comma-joined string per key so `@AppStorage` can hold it.
struct ExpansionSet {
    var raw: String

    func contains(_ id: String) -> Bool { raw.split(separator: ",").contains { $0 == id } }

    func toggled(_ id: String) -> String {
        var ids = raw.split(separator: ",").map(String.init)
        if let i = ids.firstIndex(of: id) { ids.remove(at: i) } else { ids.append(id) }
        return ids.sorted().joined(separator: ",")
    }
}

private struct AgentPreview: View {
    let agent: Agent

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(agent.displayTitle).font(.headline)
            Text("\(agent.workspaceName) · \(agent.kind)").foregroundStyle(.secondary)
            if let name = agent.name {
                Text(name).font(.footnote.monospaced()).foregroundStyle(.tertiary)
            }
        }
        .padding(20)
        .frame(width: 280, alignment: .leading)
    }
}
