import HerdKit
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
                } else {
                    EmptyChat(hasAgents: !store.state.agents.isEmpty, onNewChat: { onNewChat(nil) })
                }
            }
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    Button(action: onOpenSidebar) {
                        Image(systemName: "line.3.horizontal")
                    }
                    .accessibilityLabel("Open sidebar")
                }
                ToolbarItem(placement: .principal) {
                    if let agent = store.selectedAgent {
                        TitleMenu(agent: agent, onStop: { store.interrupt(agent.id) })
                    } else {
                        Text("Herd").font(.headline)
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

/// Centre title: agent title + chevron; the menu shows where the agent lives.
private struct TitleMenu: View {
    let agent: Agent
    let onStop: () -> Void

    var body: some View {
        Menu {
            Section {
                Label(agent.workspaceName, systemImage: "folder")
                Label(agent.kind.capitalized, systemImage: "cpu")
                Label(statusText, systemImage: statusSymbol)
            }
            if agent.status == .working {
                Button(role: .destructive, action: onStop) {
                    Label("Stop", systemImage: "stop.circle")
                }
            }
        } label: {
            HStack(spacing: 5) {
                Text(agent.displayTitle)
                    .font(.headline)
                    .lineLimit(1)
                Image(systemName: "chevron.right")
                    .font(.caption.weight(.bold))
                    .foregroundStyle(.secondary)
            }
            .foregroundStyle(.primary)
            .padding(.horizontal, 14)
            .padding(.vertical, 9)
            .frame(maxWidth: 240)
            .glassEffect(.regular.interactive(), in: .capsule)
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
    @State private var position = ScrollPosition(edge: .bottom)
    @State private var isAtBottom = true
    @State private var width: CGFloat = 390

    private var messages: [Message] { store.messages(for: agent.id) }
    private var items: [ChatItem] {
        var items = ChatItem.build(from: messages)
        items = items.map { item in
            if case .user(let id, let text, _) = item, id.hasPrefix("local-") {
                return .user(id: id, text: text, pending: true)
            }
            return item
        }
        return items
    }

    var body: some View {
        let items = items
        let lastId = items.last?.id
        let working = agent.status == .working
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 22) {
                if store.hasMore(agent.id) {
                    ProgressView()
                        .frame(maxWidth: .infinity)
                        .task { await store.loadEarlier(agent.id) }
                }
                if !agent.hasTranscript {
                    Label("No transcript. Showing what's on screen.", systemImage: "text.viewfinder")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                        .frame(maxWidth: .infinity)
                }
                ForEach(items) { item in
                    row(item, isLast: item.id == lastId, working: working)
                }
                if working, !(items.last?.isAssistantText ?? false), !lastIsLiveTools(items, working: working) {
                    PulsingDot()
                        .padding(.leading, 2)
                        .transition(.opacity)
                }
                if !store.isLoaded(agent.id) {
                    ProgressView().frame(maxWidth: .infinity).padding(.top, 80)
                }
            }
            .padding(.horizontal, 20)
            .padding(.top, 12)
            .padding(.bottom, 12)
            .animation(.smooth, value: items.count)
        }
        .scrollPosition($position)
        .defaultScrollAnchor(LaunchOptions.current.isDemo("top") ? .top : .bottom, for: .initialOffset)
        .defaultScrollAnchor(.top, for: .alignment)
        .scrollDismissesKeyboard(.interactively)
        .scrollEdgeEffectStyle(.soft, for: .all)
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
        .onScrollGeometryChange(for: Bool.self) { geo in
            // containerSize already excludes the insets. Short chats rest at -top inset.
            let maxOffset = max(geo.contentSize.height - geo.containerSize.height, 0) - geo.contentInsets.top
            return geo.contentOffset.y >= maxOffset - 60
        } action: { _, atBottom in
            isAtBottom = atBottom
        }
        .onChange(of: contentSignature(items)) {
            guard isAtBottom else { return }
            withAnimation(.smooth) { position.scrollTo(edge: .bottom) }
        }
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
                    onSend: { store.send($0, to: agent.id) },
                    onStop: { store.interrupt(agent.id) },
                    onNewChat: { onNewChat(agent.workspaceId) }
                )
            }
            .padding(.horizontal, 12)
            .padding(.bottom, 8)
            .overlay(alignment: .top) {
                if !isAtBottom {
                    Button {
                        withAnimation(.smooth) { position.scrollTo(edge: .bottom) }
                    } label: {
                        Image(systemName: "arrow.down")
                            .font(.body.weight(.semibold))
                            .frame(width: 40, height: 40)
                    }
                    .buttonStyle(.plain)
                    .glassEffect(.regular.interactive(), in: .circle)
                    .offset(y: -52)
                    .transition(.scale.combined(with: .opacity))
                    .accessibilityLabel("Scroll to bottom")
                }
            }
            .animation(.smooth, value: isAtBottom)
            .animation(.smooth, value: store.approval)
        }
    }

    @ViewBuilder
    private func row(_ item: ChatItem, isLast: Bool, working: Bool) -> some View {
        switch item {
        case .user(_, let text, let pending):
            UserBubble(text: text, pending: pending, maxWidth: width * 0.8)
        case .text(_, let markdown):
            MarkdownView(markdown)
                .textSelection(.enabled)
        case .thinking(_, let text):
            ThinkingRow(text: text)
        case .tools(let id, let steps, let messageIds):
            ToolGroupView(
                steps: steps,
                isLive: isLast && working,
                duration: duration(of: messageIds),
                expanded: LaunchOptions.current.isDemo("tools")
            )
            .id("\(id)-\(steps.count)-\(isLast && working)")
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

    /// Changes whenever something visible at the bottom grows.
    private func contentSignature(_ items: [ChatItem]) -> Int {
        var h = Hasher()
        h.combine(items.count)
        h.combine(items.last)
        h.combine(agent.status)
        return h.finalize()
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
