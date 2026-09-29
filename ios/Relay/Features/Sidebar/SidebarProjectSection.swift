import RelayKit
import SwiftUI

/// One project on the home screen: a header with `+` (new chat there) over a card of its chats that
/// aren't running (those are in Now). Seen, finished chats fold into "✓ N completed". A project whose
/// chats are all in Now is a single "name · N running ›" card that opens into the full section. In All with
/// several machines, the header carries the machine's name, so two machines' "website" never read as one.
struct SidebarProjectSection: View {
    let store: AppStore
    let section: SidebarModel.ProjectSection
    let onSelect: (String) -> Void
    let onNewChat: (String) -> Void

    @AppStorage("sidebarCompletedOpen", store: AppDefaults.standard) private var completedOpen = ""
    @AppStorage("sidebarProjectsOpen", store: AppDefaults.standard) private var projectsOpen = ""
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.dynamicTypeSize) private var typeSize

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
                        .sidebarCardRow(leading: 20, separator: nil)
                }
                if section.isAllInNow {
                    // An all-in-Now project has nothing but these, so only the last one goes without a hairline.
                    ForEach(section.running) { agent in
                        row(agent, last: agent.id == section.running.last?.id)
                    }
                }
                ForEach(section.rows) { agent in
                    row(agent, last: agent.id == section.rows.last?.id && section.completed.isEmpty)
                }
                if !section.completed.isEmpty {
                    completedRow
                    if showsCompleted {
                        ForEach(section.completed) { agent in
                            row(agent, last: agent.id == section.completed.last?.id)
                        }
                    }
                }
            } header: {
                header
            }
        }
    }

    // MARK: - Pieces

    private func row(_ agent: Agent, last: Bool) -> some View {
        SessionRow(agent: agent, store: store, showsProject: false, onSelect: onSelect, separator: !last)
    }

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
                    .padding(.vertical, -4)
                    .accessibilityValue("expanded")
                } else {
                    Text(section.name)
                }
            }
            .font(.headline)
            .lineLimit(typeSize.isAccessibilitySize ? 2 : 1)
            .accessibilityLabel(section.name)
            .accessibilityAddTraits(.isHeader)
            .accessibilityIdentifier("project-\(section.name)")
            if let machine = store.machine(of: section.id), store.showsMachineTags {
                MachineTag(machine: machine)
            }
            Spacer(minLength: 8)
            Button { onNewChat(section.id) } label: {
                Image(systemName: "plus")
                    .font(.body.weight(.medium))
                    .foregroundStyle(.secondary)
                    .frame(width: 44, height: 44)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            // The 44 pt target overhangs the 36 pt header (and to the right, so the glyph sits 20 pt in from
            // the card's edge), as in the design.
            .padding(.vertical, -4)
            .padding(.trailing, -12)
            .accessibilityLabel(store.machineTag(section.id).map { "New chat in \(section.name) on \($0)" } ?? "New chat in \(section.name)")
            .accessibilityIdentifier("newChat-\(section.name)")
        }
    }

    private var allInNowRow: some View {
        Button { toggleOpen() } label: {
            HStack(spacing: 8) {
                // At accessibility sizes the count goes under the name rather than squeezing it.
                if typeSize.isAccessibilitySize {
                    VStack(alignment: .leading, spacing: 0) {
                        Text(section.name).font(.headline).lineLimit(2)
                        machineTag
                        runningCount
                    }
                    Spacer(minLength: 8)
                } else {
                    Text(section.name).font(.headline).lineLimit(1)
                    machineTag
                    Spacer(minLength: 8)
                    runningCount
                }
                Image(systemName: "chevron.right")
                    .font(.footnote.weight(.bold))
                    .foregroundStyle(.tertiary)
            }
            .padding(.vertical, 8)
            .frame(minHeight: 50)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .sidebarCardRow(leading: 20, separator: nil)
        .accessibilityLabel(store.machineTag(section.id).map { "\(section.name), \($0)" } ?? section.name)
        .accessibilityValue("\(section.running.count) running, in Now")
        .accessibilityHint("Shows this project")
        .accessibilityIdentifier("project-\(section.name)")
    }

    @ViewBuilder
    private var machineTag: some View {
        if let machine = store.machine(of: section.id), store.showsMachineTags {
            MachineTag(machine: machine)
        }
    }

    private var runningCount: some View {
        Text("\(section.running.count) running")
            .font(.subheadline)
            .foregroundStyle(.secondary)
            .fixedSize()
    }

    private var completedRow: some View {
        Button {
            withAnimation(reduceMotion ? nil : .smooth) { completedOpen = ExpansionSet(raw: completedOpen).toggled(section.id) }
        } label: {
            HStack(spacing: 8) {
                // At accessibility sizes the glyph goes, so "completed" fits on a line instead of hyphenating.
                if !typeSize.isAccessibilitySize {
                    Image(systemName: "checkmark.circle")
                        .font(.body)
                        .foregroundStyle(.secondary)
                }
                Text("\(section.completed.count) completed")
                    .font(.body)
                    .foregroundStyle(.secondary)
                    .fixedSize(horizontal: false, vertical: true)
                Spacer(minLength: 0)
                Image(systemName: "chevron.down")
                    .font(.footnote.weight(.bold))
                    .foregroundStyle(.tertiary)
                    .rotationEffect(.degrees(showsCompleted ? 180 : 0))
            }
            .padding(.vertical, 8)
            .frame(minHeight: 50)
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .sidebarCardRow(leading: 20, separator: showsCompleted ? 0 : nil)
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

/// The small secondary machine name beside a project's name, with its status dot once it isn't online.
struct MachineTag: View {
    let machine: MachineEntry

    var body: some View {
        HStack(spacing: 4) {
            if machine.status != .online { MachineDot(status: machine.status) }
            Text(machine.displayName)
                .lineLimit(1)
        }
        .font(.caption.weight(.medium))
        .foregroundStyle(.secondary)
        .padding(.horizontal, 7)
        .padding(.vertical, 2)
        .background(Color(.tertiarySystemFill), in: .capsule)
        .fixedSize(horizontal: false, vertical: true)
        .layoutPriority(-1)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(machine.displayName)
        .accessibilityValue(machine.status == .online ? "" : machine.status.spoken)
        .accessibilityIdentifier("sectionMachineTag")
    }
}
