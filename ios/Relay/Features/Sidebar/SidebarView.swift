import RelayKit
import SwiftUI

/// The drawer, design B · Grouped: a pinned glass toolbar, a large title, then the "Now" card and one card
/// per project on a grouped ground (filter All), or a flat list for another filter or a search. Search and
/// New chat float at the bottom. The cards and rows come from SidebarNowCard, SidebarProjectSection and
/// SidebarSessionList; this view only lays them out. It also presents the Machines screen and Add machine.
struct SidebarView: View {
    @Bindable var store: AppStore
    let onSelect: (String) -> Void
    let onNewChat: (_ workspaceId: String?) -> Void
    let onUsage: () -> Void

    @State private var query = ""
    /// `-demo machines` / `-demo addMachine` open these for screenshots.
    @State private var showMachines = LaunchOptions.current.isDemo("machines")
    @State private var showAddMachine = LaunchOptions.current.isDemo("addMachine")
    @State private var searching = false
    @FocusState private var searchFocused: Bool
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.dynamicTypeSize) private var typeSize

    var body: some View {
        // The toolbar sits above the list rather than floating over it: scrolled rows never show through the
        // status bar or the toolbar (R7-13), and VoiceOver reads toolbar, list, bottom bar in that order (R7-4).
        VStack(spacing: 0) {
            SidebarToolbar(
                store: store, onUsage: onUsage,
                onAddMachine: { showAddMachine = true }, onMachines: { showMachines = true }
            )
            list
        }
        .sheet(isPresented: $showMachines) {
            MachinesView(store: store)
        }
        .sheet(isPresented: $showAddMachine) {
            PairingView()
        }
        .background(Color(.systemGroupedBackground))
        // Reduce Motion: switching filter or search swaps the list without rows sliding around.
        .animation(reduceMotion ? nil : .smooth, value: store.filter)
        .animation(reduceMotion ? nil : .smooth, value: isSearching)
    }

    private var list: some View {
        let sidebar = store.sidebar
        return List {
            title
            if isSearching {
                SidebarSessionList(
                    title: "Results", agents: sidebar.sessions(.all, query: query),
                    emptyText: "No chats match \u{201C}\(query)\u{201D}.", store: store, onSelect: onSelect
                )
                .environment(\.sidebarLeadsList, true)
            } else if store.filter == .all {
                // Whichever of these comes first sits right under the title, without the gap between cards (B1).
                let grouped = sidebar.grouped()
                let noticesLead = !SidebarMachineNotices.down(in: store).isEmpty
                let nowLeads = !noticesLead && !grouped.now.isEmpty
                SidebarMachineNotices(store: store) { showMachines = true }
                    .environment(\.sidebarLeadsList, noticesLead)
                SidebarNowCard(store: store, onSelect: onSelect)
                    .environment(\.sidebarLeadsList, nowLeads)
                ForEach(grouped.sections) { section in
                    SidebarProjectSection(store: store, section: section, onSelect: onSelect, onNewChat: { onNewChat($0) })
                        .environment(\.sidebarLeadsList, !noticesLead && !nowLeads && section.id == grouped.sections.first?.id)
                }
            } else {
                SidebarSessionList(
                    title: store.filter.title, agents: sidebar.sessions(store.filter),
                    emptyText: emptyText, store: store, onSelect: onSelect
                )
                .environment(\.sidebarLeadsList, true)
            }
        }
        .listStyle(.insetGrouped)
        .listSectionSpacing(0)
        .contentMargins(.horizontal, 16, for: .scrollContent)
        .contentMargins(.top, 0, for: .scrollContent)
        .contentMargins(.bottom, 16, for: .scrollContent)
        .environment(\.defaultMinListRowHeight, 1)
        .environment(\.defaultMinListHeaderHeight, 0)
        .scrollContentBackground(.hidden)
        .scrollDismissesKeyboard(.immediately)
        // The native version of the mockup's bottom fade.
        .scrollEdgeEffectStyle(.soft, for: .bottom)
        .safeAreaBar(edge: .bottom, spacing: 0) {
            SidebarBottomBar(
                query: $query, searching: $searching, searchFocused: $searchFocused,
                iconOnly: typeSize >= .accessibility3
            ) {
                // Across machines the sheet starts on the last used one, not wherever the open chat is.
                onNewChat(store.showsMachineTags ? nil : store.selectedAgent?.workspaceId)
            }
            .padding(.horizontal, 16)
            // 30 pt above the screen's bottom edge; just clear of the keyboard while typing.
            .padding(.bottom, searchFocused ? 8 : 30)
            // Like the toolbar: bigger glyphs would only outgrow the 50 pt search circle.
            .dynamicTypeSize(...DynamicTypeSize.accessibility1)
        }
        .ignoresSafeArea(.container, edges: .bottom)
    }

    private var isSearching: Bool {
        searching && !query.trimmingCharacters(in: .whitespaces).isEmpty
    }

    /// "Relay", 34 pt bold at 20 pt from the sidebar edge (the cards sit at 16).
    private var title: some View {
        Section {
            Text("Relay")
                .font(.largeTitle.bold())
                .accessibilityAddTraits(.isHeader)
                .frame(maxWidth: .infinity, alignment: .leading)
                .listRowInsets(EdgeInsets(top: 0, leading: 4, bottom: 0, trailing: 4))
                .listRowBackground(Color.clear)
                .listRowSeparator(.hidden)
        }
    }

    private var emptyText: String {
        switch store.filter {
        case .needsInput: "Nothing is waiting for you."
        case .readyForReview: "Nothing new to review."
        case .working: "No agent is working."
        case .completed: "No finished chats."
        case .archived: "Swipe a chat left to archive it."
        case .all: "No chats."
        }
    }
}
