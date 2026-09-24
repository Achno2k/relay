import RelayKit
import SwiftUI

/// The open conversation: system toolbar, message list, floating composer.
struct ChatView: View {
    @Bindable var store: AppStore
    let onOpenSidebar: () -> Void
    let onNewChat: (_ workspaceId: String?) -> Void

    var body: some View {
        NavigationStack {
            Group {
                if let agent = store.selectedAgent {
                    ChatTranscript(store: store, agent: agent, onNewChat: onNewChat)
                        .id(agent.id)
                } else if store.hasLoadedAgents {
                    EmptyChat(hasAgents: !store.state.agents.isEmpty, onNewChat: { onNewChat(nil) })
                } else {
                    ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            }
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    Button(action: onOpenSidebar) {
                        Image(systemName: "line.3.horizontal")
                            .overlay(alignment: .topTrailing) {
                                // Something needs input somewhere (never counting the chat you're in).
                                if store.sidebar.sessions(.needsInput).contains(where: { $0.id != store.selectedAgentId }) {
                                    Circle().fill(.orange).frame(width: 8, height: 8).offset(x: 5, y: -4)
                                }
                            }
                    }
                    .accessibilityLabel("Open sidebar")
                }
                ToolbarItem(placement: .principal) {
                    if let agent = store.selectedAgent {
                        TitleMenu(store: store, agent: agent)
                    } else {
                        Text("Relay").font(.headline)
                    }
                }
                ToolbarItem(placement: .topBarTrailing) {
                    Button {
                        onNewChat(store.selectedAgent?.workspaceId)
                    } label: {
                        Image(systemName: "square.and.pencil")
                    }
                    .accessibilityLabel("New chat")
                }
            }
        }
    }
}

/// Centre pill: agent title + chevron, with "Opus 5.5 · Auto" underneath. The menu switches
/// model, mode and effort and runs /compact and /clear (ChatGPT's model picker, for agents).
private struct TitleMenu: View {
    let store: AppStore
    let agent: Agent
    @State private var confirmClear = false
    @State private var showAllModels = false

    private var controls: AgentControls {
        store.controlsState(for: agent)
    }

    var body: some View {
        let controls = controls
        Menu {
            if controls.isAvailable {
                ControlMenuItems(
                    store: store, controls: controls,
                    onClear: { confirmClear = true },
                    onAllModels: { showAllModels = true }
                )
            }
            Section {
                Label(agent.workspaceName, systemImage: "folder")
                Label(agent.kind.capitalized, systemImage: "cpu")
                Label(statusText, systemImage: statusSymbol)
            }
            if agent.status == .working {
                Button(role: .destructive) {
                    store.interrupt(agent.id)
                } label: {
                    Label("Stop", systemImage: "stop.circle")
                }
            }
        } label: {
            VStack(spacing: 1) {
                HStack(spacing: 5) {
                    Text(agent.displayTitle)
                        .font(.headline)
                        .lineLimit(1)
                    Image(systemName: "chevron.right")
                        .font(.caption.weight(.bold))
                        .foregroundStyle(.secondary)
                }
                if let subtitle = controls.subtitle {
                    HStack(spacing: 4) {
                        if controls.pending != nil {
                            ProgressView().controlSize(.mini)
                        }
                        Text(subtitle)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .lineLimit(1)
                            .contentTransition(.opacity)
                            .accessibilityIdentifier("titleSubtitle")
                    }
                }
            }
            .foregroundStyle(.primary)
            .padding(.horizontal, 14)
            .padding(.vertical, controls.subtitle == nil ? 9 : 5)
            .frame(maxWidth: 240)
            .glassEffect(.regular.interactive(), in: .capsule)
            .animation(.smooth, value: controls.subtitle)
        }
        .accessibilityIdentifier("titleMenu")
        .accessibilityValue(controls.pending == nil ? "idle" : "applying")
        .sheet(isPresented: $showAllModels) {
            ModelSearchSheet(models: controls.info?.models ?? [], currentId: controls.modelId) {
                store.control(.model($0), for: agent.id)
            }
        }
        .confirmationDialog("Clear this conversation?", isPresented: $confirmClear, titleVisibility: .visible) {
            Button("Clear conversation", role: .destructive) { store.control(.command(.clear), for: agent.id) }
        } message: {
            Text("\(agent.displayName) starts a new session and forgets this chat. The old transcript stays on your Mac.")
        }
    }

    private var statusText: String {
        switch agent.status {
        case .idle: "Idle"
        case .working: "Working"
        case .blocked: "Needs you"
        case .done: "Done"
        case .unknown: "Unknown"
        }
    }

    private var statusSymbol: String {
        switch agent.status {
        case .idle: "moon.zzz"
        case .working: "bolt.fill"
        case .blocked: "hand.raised.fill"
        case .done: "checkmark.circle"
        case .unknown: "questionmark.circle"
        }
    }
}

private struct ChatTranscript: View {
    @Bindable var store: AppStore
    let agent: Agent
    let onNewChat: (_ workspaceId: String?) -> Void

    @State private var draft = ""
    @State private var attachments: ComposerAttachments
    /// True until the user scrolls away from the latest message; drives autoscroll and the ↓ button.
    @State private var followsBottom = true
    @State private var scrollPhase: ScrollPhase = .idle
    @State private var width: CGFloat = 390
    @State private var loadingEarlier = false

    /// Last view in the transcript, after the working indicator: "the bottom" for every programmatic scroll.
    private static let bottomID = "transcript-bottom"

    init(store: AppStore, agent: Agent, onNewChat: @escaping (_ workspaceId: String?) -> Void) {
        self.store = store
        self.agent = agent
        self.onNewChat = onNewChat
        _attachments = State(initialValue: ComposerAttachments(agentId: agent.id, store: store))
    }

    private var messages: [Message] { store.messages(for: agent.id) }
    private var items: [ChatItem] {
        var items = ChatItem.build(from: messages)
        items = items.map { item in
            if case .user(let id, let text, _, let attachments) = item, id.hasPrefix("local-") {
                return .user(id: id, text: text, pending: true, attachments: attachments)
            }
            return item
        }
        return items
    }

    var body: some View {
        let items = items
        let working = agent.status == .working
        // ScrollViewReader, not ScrollPosition: re-assigning the same edge to a ScrollPosition after the
        // user has scrolled can be a no-op, which left the ↓ button doing nothing.
        ScrollViewReader { proxy in
            Group {
                if store.isLoaded(agent.id) {
                    transcript(items, working: working, proxy: proxy)
                } else {
                    // The list appears only once there's content, so its first layout already starts at the bottom.
                    ProgressView()
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                }
            }
            // Inside the composer's safe-area inset, so it floats just above it with no offset tricks
            // (an offset moved the button outside its hit area).
            .overlay(alignment: .bottom) {
                if !followsBottom {
                    Button {
                        followsBottom = true
                        withAnimation(.smooth) { proxy.scrollTo(Self.bottomID, anchor: .bottom) }
                    } label: {
                        Image(systemName: "arrow.down")
                            .font(.body.weight(.semibold))
                            .frame(width: 40, height: 40)
                            .contentShape(.circle)
                    }
                    .buttonStyle(.plain)
                    .glassEffect(.regular.interactive(), in: .circle)
                    .padding(.bottom, 10)
                    .transition(.scale.combined(with: .opacity))
                    .accessibilityLabel("Scroll to bottom")
                }
            }
            .animation(.smooth, value: followsBottom)
            .safeAreaInset(edge: .bottom, spacing: 0) {
                VStack(spacing: 10) {
                    if agent.status == .blocked, let approval = store.approval, approval.agentId == agent.id {
                        ApprovalCard(question: approval.question) { store.isApprovalSheetPresented = true }
                            .transition(.move(edge: .bottom).combined(with: .opacity))
                    }
                    ComposerView(
                        text: $draft,
                        placeholder: "Message \(agent.displayName)",
                        workspaceName: agent.workspaceName,
                        isWorking: working,
                        attachments: attachments,
                        onSend: { text, files in
                            followsBottom = true
                            store.send(text, attachments: files, to: agent.id)
                        },
                        onStop: { store.interrupt(agent.id) },
                        onNewChat: { onNewChat(agent.workspaceId) }
                    )
                }
                .padding(.horizontal, 12)
                .padding(.bottom, 8)
                .animation(.smooth, value: store.approval)
            }
        }
    }

    /// A plain `VStack`, not a lazy one: lazy rows are measured with estimates, so on a long, real transcript
    /// the initial bottom offset landed short of the end. Real chats are one page (50 messages) at a time.
    private func transcript(_ items: [ChatItem], working: Bool, proxy: ScrollViewProxy) -> some View {
        let lastId = items.last?.id
        return ScrollView {
            VStack(alignment: .leading, spacing: 22) {
                if store.hasMore(agent.id) {
                    ProgressView()
                        .frame(maxWidth: .infinity)
                        .onScrollVisibilityChange(threshold: 0.5) { visible in
                            if visible { loadEarlier(anchor: items.first?.id, proxy: proxy) }
                        }
                }
                if items.isEmpty && !working && agent.transcript != .unsupported {
                    AgentEmptyState(agent: agent, subtitle: store.controlsState(for: agent).subtitle)
                        .padding(.top, 120)
                }
                ForEach(items) { item in
                    row(item, isLast: item.id == lastId, working: working)
                        .id(item.id)
                }
                if working, !(items.last?.isAssistantText ?? false), !lastIsLiveTools(items, working: working) {
                    PulsingDot()
                        .padding(.leading, 2)
                        .transition(.opacity)
                }
                Color.clear
                    .frame(height: 1)
                    .id(Self.bottomID)
                    .accessibilityHidden(true)
            }
            .padding(.horizontal, 20)
            .padding(.top, 12)
            .padding(.bottom, 12)
        }
        .accessibilityIdentifier("transcript")
        .defaultScrollAnchor(LaunchOptions.current.isDemo("top") ? .top : .bottom, for: .initialOffset)
        .defaultScrollAnchor(.top, for: .alignment)
        .scrollDismissesKeyboard(.interactively)
        .scrollEdgeEffectStyle(.soft, for: .all)
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
        .onScrollPhaseChange { _, phase in
            scrollPhase = phase
        }
        .onScrollGeometryChange(for: ScrollSnapshot.self) { ScrollSnapshot($0) } action: { old, new in
            // Only the user's own scrolling decides whether we follow the latest message. Programmatic
            // scrolls pass through "not at bottom" offsets mid-animation and must not turn following off.
            if scrollPhase.isUserDriven {
                followsBottom = new.isAtBottom
                return
            }
            // New content, the approval card or the keyboard moved the end of the chat: stay on it.
            let endMoved = new.maxOffset != old.maxOffset || new.insetBottom != old.insetBottom
            if followsBottom, endMoved, !new.isAtBottom {
                withAnimation(.smooth(duration: 0.25)) { proxy.scrollTo(Self.bottomID, anchor: .bottom) }
            }
        }
    }

    /// Prepending a page would otherwise leave the offset where it was and jump to older content.
    private func loadEarlier(anchor: String?, proxy: ScrollViewProxy) {
        guard !loadingEarlier else { return }
        loadingEarlier = true
        Task {
            await store.loadEarlier(agent.id)
            if let anchor { proxy.scrollTo(anchor, anchor: .top) }
            loadingEarlier = false
        }
    }

    @ViewBuilder
    private func row(_ item: ChatItem, isLast: Bool, working: Bool) -> some View {
        switch item {
        case .user(_, let text, let pending, let attachments):
            UserBubble(text: text, pending: pending, maxWidth: width * 0.8, attachments: attachments, agentId: agent.id, store: store)
        case .text(_, let markdown):
            if agent.transcript == .unsupported {
                LiveScreenCard(text: LiveScreenCard.screenText(markdown))
            } else {
                MarkdownView(markdown)
                    .textSelection(.enabled)
            }
        case .thinking(_, let text):
            ThinkingRow(text: text)
        case .stopped:
            StoppedMarker()
        case .tools(_, let steps, let messageIds):
            ToolGroupView(
                steps: steps,
                isLive: isLast && working,
                duration: duration(of: messageIds),
                expanded: LaunchOptions.current.isDemo("tools")
            )
        }
    }

    /// Only known for messages this session watched grow over the socket.
    private func duration(of messageIds: [String]) -> TimeInterval? {
        let spans = messageIds.compactMap { store.liveSpans[$0] }
        guard let start = spans.map(\.lowerBound).min(), let end = spans.map(\.upperBound).max() else { return nil }
        return end.timeIntervalSince(start)
    }

    private func lastIsLiveTools(_ items: [ChatItem], working: Bool) -> Bool {
        if case .tools = items.last { return working }
        return false
    }
}

private struct EmptyChat: View {
    let hasAgents: Bool
    let onNewChat: () -> Void

    var body: some View {
        VStack(spacing: 16) {
            Image(systemName: "bubble.left.and.text.bubble.right")
                .font(.system(size: 44, weight: .light))
                .foregroundStyle(.secondary)
            Text(hasAgents ? "Pick an agent" : "No agents running")
                .font(.title2.weight(.semibold))
            Text(hasAgents ? "Open the sidebar to jump into a chat." : "Start one in any herdr workspace.")
                .foregroundStyle(.secondary)
            Button("New chat", systemImage: "square.and.pencil", action: onNewChat)
                .buttonStyle(.glass)
                .controlSize(.large)
                .padding(.top, 8)
        }
        .multilineTextAlignment(.center)
        .padding(32)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

private struct ScrollSnapshot: Equatable {
    var offset: CGFloat
    var maxOffset: CGFloat
    var insetBottom: CGFloat

    init(_ geo: ScrollGeometry) {
        offset = geo.contentOffset.y
        // containerSize already excludes the insets. Short chats rest at -top inset.
        maxOffset = max(geo.contentSize.height - geo.containerSize.height, 0) - geo.contentInsets.top
        insetBottom = geo.contentInsets.bottom
    }

    var isAtBottom: Bool { offset >= maxOffset - 60 }
}

private extension ScrollPhase {
    var isUserDriven: Bool {
        switch self {
        case .tracking, .interacting, .decelerating: true
        default: false
        }
    }
}
