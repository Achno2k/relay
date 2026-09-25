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
        .sidebarCardRow(selected: selected, leading: showsProject ? 18 : 20)
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
            VStack(alignment: .leading, spacing: 1) {
                Text(agent.displayTitle)
                    .font(.body)
                    .lineLimit(1)
                Text("\(agent.workspaceName) · \(RelativeTime.compact(agent.updatedAt))")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            .alignmentGuide(.listRowSeparatorLeading) { $0[.leading] }
            Spacer(minLength: 0)
        }
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
                .lineLimit(1)
            Spacer(minLength: 0)
            Text(RelativeTime.compact(agent.updatedAt))
                .font(.subheadline)
                .foregroundStyle(.secondary)
        }
        .alignmentGuide(.listRowSeparatorLeading) { $0[.leading] }
        .frame(minHeight: 50)
    }

    private func accessibilityValue(unseen: Bool) -> String {
        var parts = [StatusGlyph.label(agent.status, unseen: unseen)]
        if showsProject { parts.append(agent.workspaceName) }
        parts.append(RelativeTime.compact(agent.updatedAt))
        return parts.joined(separator: ", ")
    }

    private func setArchived(_ value: Bool) {
        withAnimation(.smooth) { store.setArchived(agent.id, value) }
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
                    .sidebarCardRow(leading: 20)
            }
            ForEach(agents) { agent in
                SessionRow(agent: agent, store: store, showsProject: true, onSelect: onSelect)
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

/// Row chrome inside a card: design insets, the `surface` fill, a highlight for the open chat.
struct SidebarCardRowModifier: ViewModifier {
    var selected = false
    var leading: CGFloat

    func body(content: Content) -> some View {
        content
            .listRowInsets(EdgeInsets(top: 0, leading: leading, bottom: 0, trailing: 18))
            // Separators run to the card's edge, past the trailing inset.
            .alignmentGuide(.listRowSeparatorTrailing) { $0[.trailing] + 18 }
            .listRowBackground(
                Color(.secondarySystemGroupedBackground)
                    .overlay(selected ? Color(.systemFill) : .clear)
            )
    }
}

extension View {
    func sidebarCardRow(selected: Bool = false, leading: CGFloat) -> some View {
        modifier(SidebarCardRowModifier(selected: selected, leading: leading))
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
