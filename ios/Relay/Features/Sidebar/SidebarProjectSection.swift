import RelayKit
import SwiftUI

/// One project on the home screen: a header with `+` (new chat there) over a card of its chats that
/// aren't running (those are in Now). Seen, finished chats fold into "✓ N completed". A project whose
/// chats are all in Now is a single "name · N running ›" card that opens into the full section.
struct SidebarProjectSection: View {
    let store: AppStore
    let section: SidebarModel.ProjectSection
    let onSelect: (String) -> Void
    let onNewChat: (String) -> Void

    @AppStorage("sidebarCompletedOpen", store: AppDefaults.standard) private var completedOpen = ""
    @AppStorage("sidebarProjectsOpen", store: AppDefaults.standard) private var projectsOpen = ""
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private var showsCompleted: Bool { ExpansionSet(raw: completedOpen).contains(section.id) }
    /// Only means something for an all-in-Now project: opened past its one-row summary.
    private var isOpen: Bool { ExpansionSet(raw: projectsOpen).contains(section.id) }

    var body: some View {
        if section.isAllInNow && !isOpen {
            Section {
                allInNowRow
            } header: {
                Color.clear.frame(height: 12).listRowInsets(EdgeInsets())
            }
        } else {
            Section {
                if section.isEmpty {
                    Text("No chats")
                        .font(.body)
                        .foregroundStyle(.tertiary)
                        .frame(maxWidth: .infinity, minHeight: 50, alignment: .leading)
                        .sidebarCardRow(leading: 20)
                }
                if section.isAllInNow {
                    ForEach(section.running) { agent in
                        SessionRow(agent: agent, store: store, showsProject: false, onSelect: onSelect)
                    }
                }
                ForEach(section.rows) { agent in
                    SessionRow(agent: agent, store: store, showsProject: false, onSelect: onSelect)
                }
                if !section.completed.isEmpty {
                    completedRow
                    if showsCompleted {
                        ForEach(section.completed) { agent in
                            SessionRow(agent: agent, store: store, showsProject: false, onSelect: onSelect)
                        }
                    }
                }
            } header: {
                header
            }
        }
    }

    // MARK: - Pieces

    private var header: some View {
        SidebarSectionHeader {
            Group {
                if section.isAllInNow {
                    // Opened from its summary row: the name folds it back.
                    Button { toggleOpen() } label: {
                        HStack(spacing: 6) {
                            Text(section.name)
                            Image(systemName: "chevron.down")
                                .font(.footnote.weight(.bold))
                                .foregroundStyle(.tertiary)
                        }
                        .frame(minHeight: 44)
                        .contentShape(.rect)
                    }
                    .buttonStyle(.plain)
                    .accessibilityValue("expanded")
                } else {
                    Text(section.name)
                }
            }
            .font(.headline)
            .lineLimit(1)
            .accessibilityLabel(section.name)
            .accessibilityAddTraits(.isHeader)
            .accessibilityIdentifier("project-\(section.name)")
            Spacer(minLength: 8)
            Button { onNewChat(section.id) } label: {
                Image(systemName: "plus")
                    .font(.body.weight(.medium))
                    .foregroundStyle(.secondary)
                    .frame(width: 44, height: 44)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            // The 44 pt target overhangs so the glyph sits 20 pt in from the card's edge, as in the design.
            .padding(.trailing, -12)
            .accessibilityLabel("New chat in \(section.name)")
            .accessibilityIdentifier("newChat-\(section.name)")
        }
    }

    private var allInNowRow: some View {
        Button { toggleOpen() } label: {
            HStack(spacing: 8) {
                Text(section.name)
                    .font(.headline)
                    .lineLimit(1)
                Spacer(minLength: 8)
                Text("\(section.running.count) running")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.bold))
                    .foregroundStyle(.tertiary)
            }
            .frame(minHeight: 50)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .sidebarCardRow(leading: 20)
        .accessibilityLabel(section.name)
        .accessibilityValue("\(section.running.count) running, in Now")
        .accessibilityHint("Shows this project")
        .accessibilityIdentifier("project-\(section.name)")
    }

    private var completedRow: some View {
        Button {
            withAnimation(reduceMotion ? nil : .smooth) { completedOpen = ExpansionSet(raw: completedOpen).toggled(section.id) }
        } label: {
            HStack(spacing: 8) {
                Image(systemName: "checkmark.circle")
                    .font(.body)
                    .foregroundStyle(.secondary)
                Text("\(section.completed.count) completed")
                    .font(.body)
                    .foregroundStyle(.secondary)
                Spacer(minLength: 0)
                Image(systemName: "chevron.down")
                    .font(.footnote.weight(.bold))
                    .foregroundStyle(.tertiary)
                    .rotationEffect(.degrees(showsCompleted ? 180 : 0))
            }
            .frame(minHeight: 50)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .sidebarCardRow(leading: 20)
        .sensoryFeedback(.selection, trigger: showsCompleted)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(section.completed.count) completed")
        .accessibilityValue(showsCompleted ? "expanded" : "collapsed")
        .accessibilityAddTraits(.isButton)
        .accessibilityIdentifier("completed-\(section.name)")
    }

    private func toggleOpen() {
        withAnimation(reduceMotion ? nil : .smooth) { projectsOpen = ExpansionSet(raw: projectsOpen).toggled(section.id) }
    }
}
