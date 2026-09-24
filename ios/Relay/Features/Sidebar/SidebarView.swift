import RelayKit
import SwiftUI

/// Codex-style home in the drawer: filter and ⋯ buttons, a large title, device chips, then either the
/// Projects tree (filter All) or a flat Sessions list. Search and New chat float at the bottom.
struct SidebarView: View {
    @Bindable var store: AppStore
    let onSelect: (String) -> Void
    let onNewChat: (_ workspaceId: String?) -> Void
    let onUnpair: () -> Void
    let onUsage: () -> Void

    @State private var expanded: Set<String> = Set(AppDefaults.standard.stringArray(forKey: "expandedProjects") ?? [])
    @State private var query = ""
    @State private var searching = false
    @FocusState private var searchFocused: Bool
    @Namespace private var glass

    var body: some View {
        let sidebar = store.sidebar
        List {
            header(sidebar)
            if searching && !query.trimmingCharacters(in: .whitespaces).isEmpty {
                sessions(sidebar.sessions(.all, query: query), title: "Results", sidebar: sidebar)
            } else if store.filter == .all {
                projects(sidebar)
            } else {
                sessions(sidebar.sessions(store.filter), title: store.filter.title, sidebar: sidebar)
            }
        }
        .listStyle(.plain)
        .scrollContentBackground(.hidden)
        .scrollDismissesKeyboard(.immediately)
        .scrollEdgeEffectStyle(.soft, for: .all)
        .environment(\.defaultMinListRowHeight, 1)
        .safeAreaInset(edge: .bottom, spacing: 0) { bottomBar }
        .background(Color(.systemBackground))
        .animation(.smooth, value: store.filter)
        .animation(.smooth, value: expanded)
    }

    // MARK: - Header

    @ViewBuilder
    private func header(_ sidebar: SidebarModel) -> some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack {
                FilterButton(filter: $store.filter, needsInput: sidebar.needsInputCount > 0)
                Spacer()
                moreMenu
            }
            Text("Relay")
                .font(.largeTitle.bold())
                .padding(.top, 4)
            DeviceChips(machines: store.machines, fallbackName: store.hostLabel, connected: store.connection == .connected)
        }
        .padding(.horizontal, 20)
        .padding(.top, 6)
        .padding(.bottom, 8)
        .plainRow()
    }

    private var moreMenu: some View {
        Menu {
            Section {
                if let machine = store.machines.first {
                    Label(machine.name, systemImage: machine.kind == .laptop ? "laptopcomputer" : "desktopcomputer")
                    if let os = machine.os { Label(os, systemImage: "apple.logo") }
                }
                Label(store.hostLabel, systemImage: "network")
                Label(store.connection == .connected ? "Connected" : "Reconnecting…",
                      systemImage: store.connection == .connected ? "checkmark.circle" : "wifi.exclamationmark")
            }
            Section {
                Button(action: onUsage) {
                    Label("Usage", systemImage: "gauge")
                }
                .accessibilityIdentifier("sidebarUsage")
            }
            Section {
                Button(role: .destructive, action: onUnpair) {
                    Label("Unpair", systemImage: "link")
                }
            }
        } label: {
            Image(systemName: "ellipsis")
                .font(.body.weight(.semibold))
                .frame(width: 44, height: 44)
                .contentShape(.circle)
        }
        .tint(.primary)
        .glassEffect(.regular.interactive(), in: .circle)
        .accessibilityLabel("More")
        .accessibilityIdentifier("sidebarMore")
    }

    // MARK: - Projects tree

    @ViewBuilder
    private func projects(_ sidebar: SidebarModel) -> some View {
        Section {
            ForEach(sidebar.projects()) { project in
                ProjectRow(
                    project: project,
                    expanded: expanded.contains(project.id),
                    onToggle: { toggle(project.id) },
                    onNewChat: { onNewChat(project.id) }
                )
                .plainRow()
                if expanded.contains(project.id) {
                    if project.agents.isEmpty {
                        Text("No chats")
                            .font(.subheadline)
                            .foregroundStyle(.tertiary)
                            .padding(.leading, 54)
                            .padding(.vertical, 6)
                            .plainRow()
                    }
                    ForEach(project.agents) { agent in
                        chatRow(agent, subtitleFolder: false, sidebar: sidebar)
                    }
                }
            }
        } header: {
            sectionHeader("Projects")
        }
    }

    private func toggle(_ id: String) {
        withAnimation(.smooth) {
            if expanded.contains(id) { expanded.remove(id) } else { expanded.insert(id) }
        }
        AppDefaults.standard.set(Array(expanded).sorted(), forKey: "expandedProjects")
    }

    // MARK: - Flat sessions

    @ViewBuilder
    private func sessions(_ agents: [Agent], title: String, sidebar: SidebarModel) -> some View {
        Section {
            if agents.isEmpty {
                Text(emptyText)
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 20)
                    .padding(.vertical, 12)
                    .plainRow()
            }
            ForEach(agents) { agent in
                chatRow(agent, subtitleFolder: true, sidebar: sidebar)
            }
        } header: {
            sectionHeader(title)
        }
    }

    private var emptyText: String {
        if searching && !query.isEmpty { return "No chats match \u{201C}\(query)\u{201D}." }
        switch store.filter {
        case .needsInput: return "Nothing is waiting for you."
        case .readyForReview: return "Nothing new to review."
        case .working: return "No agent is working."
        case .completed: return "No finished chats."
        case .archived: return "Swipe a chat left to archive it."
        case .all: return "No chats."
        }
    }

    private func chatRow(_ agent: Agent, subtitleFolder: Bool, sidebar: SidebarModel) -> some View {
        let archived = store.isArchived(agent.id)
        return ChatRow(
            agent: agent,
            unseen: sidebar.isUnseen(agent),
            showsFolder: subtitleFolder,
            selected: agent.id == store.selectedAgentId
        ) {
            onSelect(agent.id)
        }
        .plainRow()
        .swipeActions(edge: .trailing, allowsFullSwipe: true) {
            Button {
                withAnimation(.smooth) { store.setArchived(agent.id, !archived) }
            } label: {
                Label(archived ? "Unarchive" : "Archive", systemImage: archived ? "tray.and.arrow.up" : "archivebox")
            }
            .tint(archived ? .blue : .indigo)
        }
        .contextMenu {
            Button {
                withAnimation(.smooth) { store.setArchived(agent.id, !archived) }
            } label: {
                Label(archived ? "Unarchive" : "Archive", systemImage: archived ? "tray.and.arrow.up" : "archivebox")
            }
            Button(role: .destructive) {
                store.interrupt(agent.id)
            } label: {
                Label("Stop", systemImage: "stop.circle")
            }
            .disabled(agent.status != .working)
        } preview: {
            AgentPreview(agent: agent)
        }
    }

    private func sectionHeader(_ title: String) -> some View {
        Text(title)
            .font(.subheadline.weight(.semibold))
            .foregroundStyle(.secondary)
            .padding(.horizontal, 20)
            .padding(.top, 14)
            .padding(.bottom, 4)
            .frame(maxWidth: .infinity, alignment: .leading)
            .listRowInsets(EdgeInsets())
            .background(Color(.systemBackground))
    }

    // MARK: - Bottom bar

    /// Search is a glass circle bottom-left that grows into a field; New chat is the capsule on the right.
    /// Both sit where a thumb reaches, and the list keeps its full height for chats.
    private var bottomBar: some View {
        GlassEffectContainer(spacing: 12) {
            HStack(spacing: 12) {
                if searching {
                    HStack(spacing: 8) {
                        Image(systemName: "magnifyingglass").foregroundStyle(.secondary)
                        TextField("Search chats", text: $query)
                            .focused($searchFocused)
                            .submitLabel(.search)
                            .accessibilityIdentifier("sidebarSearchField")
                        Button {
                            withAnimation(.smooth) {
                                query = ""
                                searching = false
                                searchFocused = false
                            }
                        } label: {
                            Image(systemName: "xmark.circle.fill")
                                .foregroundStyle(.tertiary)
                                .frame(width: 44, height: 44)
                                .contentShape(.rect)
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel("Close search")
                    }
                    .padding(.horizontal, 16)
                    .frame(height: 48)
                    .glassEffect(.regular.interactive(), in: .capsule)
                    .glassEffectID("search", in: glass)
                } else {
                    Button {
                        withAnimation(.smooth) { searching = true }
                        searchFocused = true
                    } label: {
                        Image(systemName: "magnifyingglass")
                            .font(.body.weight(.semibold))
                            .frame(width: 48, height: 48)
                            .contentShape(.circle)
                    }
                    .buttonStyle(.plain)
                    .glassEffect(.regular.interactive(), in: .circle)
                    .glassEffectID("search", in: glass)
                    .accessibilityLabel("Search")
                    .accessibilityIdentifier("sidebarSearch")

                    Spacer(minLength: 0)

                    Button {
                        onNewChat(store.selectedAgent?.workspaceId)
                    } label: {
                        Label("New chat", systemImage: "plus")
                            .font(.body.weight(.semibold))
                            .foregroundStyle(Color(.systemBackground))
                            .padding(.horizontal, 20)
                            .frame(height: 48)
                            .contentShape(.capsule)
                    }
                    .buttonStyle(.plain)
                    .glassEffect(.regular.tint(.primary).interactive(), in: .capsule)
                    .glassEffectID("new", in: glass)
                    .accessibilityIdentifier("sidebarNewChat")
                }
            }
        }
        .padding(.horizontal, 16)
        .padding(.bottom, 8)
    }
}

private extension View {
    /// Custom list row: no separator, no background, no system insets.
    func plainRow() -> some View {
        listRowSeparator(.hidden)
            .listRowBackground(Color.clear)
            .listRowInsets(EdgeInsets())
    }
}

// MARK: - Pieces

private struct FilterButton: View {
    @Binding var filter: SessionFilter
    let needsInput: Bool

    var body: some View {
        Menu {
            Picker("Filter", selection: $filter) {
                ForEach(SessionFilter.allCases) { f in
                    Label(f.title, systemImage: f.symbol).tag(f)
                }
            }
            .pickerStyle(.inline)
        } label: {
            Image(systemName: "line.3.horizontal.decrease")
                .font(.body.weight(.semibold))
                .frame(width: 44, height: 44)
                .contentShape(.circle)
                .overlay(alignment: .topTrailing) {
                    if needsInput {
                        Circle().fill(.orange).frame(width: 9, height: 9).offset(x: -8, y: 8)
                    }
                }
        }
        .tint(.primary)
        .glassEffect(.regular.interactive(), in: .circle)
        .accessibilityLabel("Filter: \(filter.title)")
        .accessibilityValue(needsInput ? "needs input" : "")
        .accessibilityIdentifier("sidebarFilter")
    }
}

private struct DeviceChips: View {
    let machines: [Machine]
    let fallbackName: String
    let connected: Bool
    @AppStorage("machineFilter", store: AppDefaults.standard) private var selected = "all"

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: 8) {
                chip(id: "all", selected: selected == "all") {
                    Text("All")
                }
                .accessibilityLabel("All devices")
                if machines.isEmpty {
                    chip(id: "this-mac", selected: selected == "this-mac") {
                        machineLabel(name: fallbackName, symbol: "desktopcomputer")
                    }
                }
                ForEach(machines) { machine in
                    chip(id: machine.id, selected: selected == machine.id) {
                        machineLabel(name: machine.name, symbol: machine.kind == .laptop ? "laptopcomputer" : "desktopcomputer")
                    }
                }
            }
        }
        .scrollClipDisabled()
    }

    private func machineLabel(name: String, symbol: String) -> some View {
        HStack(spacing: 7) {
            Circle().fill(connected ? .green : .gray).frame(width: 7, height: 7)
            Image(systemName: symbol)
            Text(name).lineLimit(1)
        }
    }

    private func chip(id: String, selected: Bool, @ViewBuilder label: () -> some View) -> some View {
        Button {
            withAnimation(.smooth) { self.selected = id }
        } label: {
            label()
                .font(.subheadline.weight(.semibold))
                .foregroundStyle(selected ? Color(.systemBackground) : .primary)
                .padding(.horizontal, 14)
                .frame(height: 38)
                .background(selected ? Color.primary : Color(.secondarySystemFill), in: .capsule)
                .contentShape(.capsule)
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

private struct ProjectRow: View {
    let project: SidebarModel.Project
    let expanded: Bool
    let onToggle: () -> Void
    let onNewChat: () -> Void
    /// Matches the SF Symbol at `.body` it replaced, and follows Dynamic Type.
    @ScaledMetric(relativeTo: .body) private var folderSize: CGFloat = 20

    var body: some View {
        HStack(spacing: 12) {
            Button(action: onToggle) {
                HStack(spacing: 12) {
                    // Lucide folder-closed / folder-open (ios/THIRD_PARTY.md), template vectors tinted like text.
                    Image(expanded ? "folder-open" : "folder-closed")
                        .resizable()
                        .renderingMode(.template)
                        .scaledToFit()
                        .frame(width: folderSize, height: folderSize)
                        .foregroundStyle(.secondary)
                        .frame(width: 26)
                        .accessibilityHidden(true)
                    Text(project.name)
                        .font(.body.weight(.medium))
                        .lineLimit(1)
                    Image(systemName: "chevron.right")
                        .font(.caption.weight(.bold))
                        .foregroundStyle(.tertiary)
                        .rotationEffect(.degrees(expanded ? 90 : 0))
                    if project.needsInput > 0 {
                        HStack(spacing: 3) {
                            Image(systemName: "hand.raised.fill")
                            Text("\(project.needsInput)")
                        }
                        .font(.caption2.weight(.bold))
                        .foregroundStyle(.white)
                        .padding(.horizontal, 7)
                        .padding(.vertical, 3)
                        .background(.orange, in: .capsule)
                        .accessibilityLabel("\(project.needsInput) need input")
                    }
                    Spacer(minLength: 8)
                }
                .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityLabel(project.name)
            .accessibilityValue(expanded ? "expanded" : "collapsed")
            .accessibilityIdentifier("project-\(project.name)")

            Button(action: onNewChat) {
                Image(systemName: "square.and.pencil")
                    .font(.body)
                    .foregroundStyle(.secondary)
                    .frame(width: 44, height: 44)
                    .contentShape(.rect)
            }
            .buttonStyle(.plain)
            .accessibilityLabel("New chat in \(project.name)")
            .accessibilityIdentifier("newChat-\(project.name)")
        }
        .padding(.leading, 20)
        .padding(.trailing, 12)
        .padding(.vertical, 8)
        .sensoryFeedback(.selection, trigger: expanded)
    }
}

private struct ChatRow: View {
    let agent: Agent
    let unseen: Bool
    let showsFolder: Bool
    let selected: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(alignment: .firstTextBaseline, spacing: 12) {
                StatusGlyph(status: agent.status, unseen: unseen)
                    .frame(width: 22)
                    .alignmentGuide(.firstTextBaseline) { $0[VerticalAlignment.center] + 5 }
                VStack(alignment: .leading, spacing: 3) {
                    Text(agent.displayTitle)
                        .font(.body)
                        .lineLimit(1)
                    Text(subtitle)
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                Spacer(minLength: 0)
            }
            // Same content edge as the project row above it: no extra indent under an expanded folder.
            .padding(.leading, 20)
            .padding(.trailing, 16)
            .padding(.vertical, 9)
            .background {
                if selected {
                    RoundedRectangle(cornerRadius: 14)
                        .fill(Color(.secondarySystemFill))
                        .padding(.horizontal, 8)
                }
            }
            .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .accessibilityLabel(agent.displayTitle)
        .accessibilityValue(subtitle)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    private var subtitle: String {
        var parts: [String] = []
        switch agent.status {
        case .blocked: parts.append("Waiting for you")
        case .working: parts.append("Working")
        case .done where unseen: parts.append("Ready for review")
        default: break
        }
        if showsFolder { parts.append(agent.workspaceName) }
        parts.append(RelativeTime.compact(agent.updatedAt))
        return parts.joined(separator: " · ")
    }
}

/// Leading status glyph, as in the Codex session list.
struct StatusGlyph: View {
    let status: AgentStatus
    let unseen: Bool

    var body: some View {
        Group {
            switch status {
            case .working:
                ProgressView().controlSize(.small)
            case .blocked:
                Image(systemName: "hand.raised").foregroundStyle(.orange)
            case .done where unseen:
                Image(systemName: "eye").foregroundStyle(.blue)
            case .done:
                Image(systemName: "checkmark.circle").foregroundStyle(.secondary)
            case .idle:
                Image(systemName: "circle").font(.caption.weight(.semibold)).foregroundStyle(.secondary)
            case .unknown:
                Image(systemName: "circle.dashed").foregroundStyle(.tertiary)
            }
        }
        .font(.subheadline.weight(.medium))
        .accessibilityLabel(label)
    }

    private var label: String {
        switch status {
        case .working: "Working"
        case .blocked: "Needs input"
        case .done: unseen ? "Ready for review" : "Completed"
        case .idle: "Idle"
        case .unknown: "Unknown"
        }
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
