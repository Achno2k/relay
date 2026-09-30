import RelayKit
import SwiftUI

/// Chat with a sidebar drawer underneath, animated like the Codex app: opening slides and fades the sidebar
/// in while the chat recedes into a rounded card peeking on the right (design B); closing eases the sidebar
/// down and out while the chat springs back. Everything follows one `progress`, so edge swipes and drags track the finger.
/// Reduce Motion: no movement or scaling, the sidebar just crossfades over the dimmed chat.
struct MainView: View {
    @Bindable var store: AppStore
    @Environment(AppModel.self) private var model
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    @State private var sidebarOpen = LaunchOptions.current.isDemo("sidebar")
    @State private var dragX: CGFloat = 0
    @State private var dragging = false
    @State private var newChatWorkspace: NewChatRequest?
    @State private var showUsage = false
    /// The banner's "pair again" for the machine whose token was rejected.
    @State private var showRePair = false

    var body: some View {
        GeometryReader { geo in
            let width = sidebarWidth(geo.size.width)
            let progress = openProgress(width: width)
            let peek = PeekShape(
                progress: reduceMotion ? 0 : progress,
                safeTop: geo.safeAreaInsets.top,
                safeBottom: geo.safeAreaInsets.bottom,
                bottomGap: peekBottomGap,
                radius: peekRadius
            )
            ZStack(alignment: .leading) {
                // The grouped ground shows in the gap between the sidebar and the chat peek.
                Color(.systemGroupedBackground).ignoresSafeArea()

                SidebarView(
                    store: store,
                    onSelect: { id in
                        store.open(id)
                        setSidebar(false)
                    },
                    onNewChat: { presentNewChat($0) },
                    onUsage: { showUsage = true }
                )
                .frame(width: width)
                .opacity(reduceMotion ? progress : 0.3 + 0.7 * progress)
                .scaleEffect(reduceMotion ? 1 : 0.97 + 0.03 * progress, anchor: .top)
                .offset(x: reduceMotion ? 0 : -width * 0.12 * (1 - progress), y: reduceMotion ? 0 : 18 * (1 - progress))
                .zIndex(reduceMotion ? 1 : 0)
                .accessibilityHidden(progress == 0)
                // No drag-to-close on the sidebar itself: a left swipe there archives a chat.
                // The chat peek on the right still closes the drawer on tap or drag.

                ChatView(
                    store: store,
                    onOpenSidebar: { setSidebar(true) },
                    onNewChat: { presentNewChat($0) }
                )
                .fileViewer(store: store)
                .overlay {
                    // The peek stays undimmed, as in the mockup. Reduce Motion keeps the dim: there the sidebar
                    // crossfades over the full chat instead of moving it aside.
                    Color.black
                        .opacity(reduceMotion ? 0.28 * progress : 0)
                        .contentShape(.rect)
                        .ignoresSafeArea()
                        .allowsHitTesting(progress > 0)
                        .onTapGesture { setSidebar(false) }
                        .gesture(drawerDrag(width: width))
                        .accessibilityElement()
                        .accessibilityLabel(peekLabel)
                        .accessibilityAddTraits(.isButton)
                        .accessibilityAction { setSidebar(false) }
                        .accessibilityIdentifier("sidebarPeek")
                        .accessibilityHidden(progress == 0)
                }
                .clipShape(peek)
                // A hairline edge, so the peek reads as a card on the grouped ground in dark mode.
                .overlay {
                    if !reduceMotion && progress > 0 {
                        peek.stroke(Color(.separator).opacity(progress), lineWidth: 0.5)
                            .allowsHitTesting(false)
                    }
                }
                .overlay(alignment: .leading) {
                    if !sidebarOpen {
                        // Edge swipe to open. Starts below the toolbar so the sidebar button stays tappable.
                        Color.clear
                            .frame(width: 18)
                            .contentShape(.rect)
                            .padding(.top, 110)
                            .gesture(drawerDrag(width: width))
                    }
                }
                .offset(x: reduceMotion ? 0 : (width + peekGap) * progress)
            }
        }
        .overlay(alignment: .top) {
            ConnectionBanner(store: store) {
                model.rePairMachineId = store.rePairMachineId
                showRePair = true
            }
        }
        .sheet(item: $newChatWorkspace) { request in
            NewChatSheet(store: store, initialWorkspaceId: request.workspaceId)
        }
        .sheet(isPresented: $store.isApprovalSheetPresented) {
            if let approval = store.approval {
                ApprovalSheet(approval: approval, agentTitle: store.selectedAgent?.displayTitle ?? "Needs you") { option, text in
                    store.answer(option, text: text)
                }
            }
        }
        .sheet(isPresented: $showRePair, onDismiss: { model.rePairMachineId = nil }) {
            PairingView()
        }
        .onChange(of: model.rePairMachineId) { _, id in
            // Re-paired: the model clears the id, which closes the sheet.
            if id == nil { showRePair = false }
        }
        .sheet(isPresented: $showUsage) {
            UsageView(store: store.usage)
        }
        .sensoryFeedback(.impact(weight: .light), trigger: sidebarOpen)
        .task {
            store.start()
            if LaunchOptions.current.isDemo("newChat") {
                try? await Task.sleep(for: .milliseconds(600))
                presentNewChat(nil)
            }
            if LaunchOptions.current.isDemo("usage") {
                try? await Task.sleep(for: .milliseconds(600))
                showUsage = true
            }
        }
        .onChange(of: store.isApprovalSheetPresented) { _, presented in
            // The keyboard would cover the options.
            if presented { hideKeyboard() }
        }
        .onChange(of: scenePhase) { _, phase in
            switch phase {
            case .active: Task { await store.resume() }
            case .background: store.suspend()
            default: break
            }
        }
        .onReceive(NotificationCenter.default.publisher(for: UIApplication.didReceiveMemoryWarningNotification)) { _ in
            store.handleMemoryWarning()
        }
    }

    // Design B: a 342 pt sidebar, then a 10 pt gap and the chat as a card (radius 36) whose top meets the
    // safe area and whose bottom stops 18 pt above the screen edge. On a 390 pt phone 38 pt of it peeks.
    private let peekGap: CGFloat = 10
    private let peekRadius: CGFloat = 36
    private let peekBottomGap: CGFloat = 18

    private func sidebarWidth(_ screen: CGFloat) -> CGFloat { min(screen - 48, 342) }

    private var peekLabel: String {
        if let title = store.selectedAgent?.displayTitle { return "Return to \(title)" }
        return "Return to chat"
    }

    private func openProgress(width: CGFloat) -> CGFloat {
        let base: CGFloat = sidebarOpen ? width : 0
        return min(max((base + dragX) / width, 0), 1)
    }

    private func setSidebar(_ open: Bool) {
        if open { hideKeyboard() }
        withAnimation(reduceMotion ? .easeInOut(duration: 0.2) : .spring(duration: 0.42, bounce: 0.12)) {
            sidebarOpen = open
            dragX = 0
        }
    }

    /// Global coordinates: the chat moves and scales under the finger, so local translations shrank as it
    /// moved and an edge swipe fell back closed.
    private func drawerDrag(width: CGFloat) -> some Gesture {
        DragGesture(minimumDistance: 12, coordinateSpace: .global)
            .onChanged { value in
                if !dragging {
                    guard abs(value.translation.width) > abs(value.translation.height) else { return }
                    dragging = true
                    hideKeyboard()
                }
                dragX = value.translation.width
            }
            .onEnded { value in
                guard dragging else { return }
                dragging = false
                let base: CGFloat = sidebarOpen ? width : 0
                let predicted = base + value.predictedEndTranslation.width
                setSidebar(predicted > width / 2)
            }
    }

    private func presentNewChat(_ workspaceId: String?) {
        setSidebar(false)
        newChatWorkspace = NewChatRequest(workspaceId: workspaceId)
    }

    private func hideKeyboard() {
        UIApplication.shared.sendAction(#selector(UIResponder.resignFirstResponder), to: nil, from: nil, for: nil)
    }
}

/// The chat's outline as it recedes. The chat's frame is the safe area, but it draws edge to edge, so when
/// closed this is the whole screen. Open, the card runs from the safe-area top to `bottomGap` above the
/// screen's bottom edge, with rounded corners.
private struct PeekShape: Shape {
    var progress: CGFloat
    var safeTop: CGFloat
    var safeBottom: CGFloat
    var bottomGap: CGFloat
    var radius: CGFloat

    var animatableData: CGFloat {
        get { progress }
        set { progress = newValue }
    }

    func path(in rect: CGRect) -> Path {
        let top = rect.minY - safeTop * (1 - progress)
        let bottom = rect.maxY + safeBottom - (safeBottom - bottomGap) * progress
        let card = CGRect(x: rect.minX, y: top, width: rect.width, height: bottom - top)
        return Path(roundedRect: card, cornerRadius: radius * progress, style: .continuous)
    }
}

private struct NewChatRequest: Identifiable {
    let id = UUID()
    var workspaceId: String?
}

/// "Reconnecting…" (or "<machine> is offline") pill while the socket is down, a sticky re-pair prompt once the token is
/// rejected, and transient errors otherwise.
private struct ConnectionBanner: View {
    @Bindable var store: AppStore
    let onRePair: () -> Void

    /// The machine whose token was rejected, named when there's more than one.
    private var rePairText: String {
        guard store.machines.count > 1, let id = store.rePairMachineId, let name = store.connection(id: id)?.entry.displayName else {
            return "Token rejected. Tap to pair again."
        }
        return "\(name) rejected the token. Tap to pair again."
    }

    var body: some View {
        VStack(spacing: 8) {
            // Retrying can't help while the token is rejected; the re-pair prompt says what will.
            if let notice = store.connectionNotice {
                Label(notice.text, systemImage: "wifi.exclamationmark")
                    .font(.footnote.weight(.semibold))
                    .padding(.horizontal, 14)
                    .padding(.vertical, 8)
                    .glassEffect(.regular, in: .capsule)
                    .transition(.move(edge: .top).combined(with: .opacity))
            }
            // Sticky, never auto-dismissed: unlike a transient failure, this doesn't clear itself on
            // retry, so hiding it after a few seconds would leave the person staring at a chat that
            // silently can't do anything until they re-pair.
            if store.needsRePairing {
                Button(action: onRePair) {
                    Label(rePairText, systemImage: "key.slash")
                        .font(.footnote.weight(.semibold))
                        .padding(.horizontal, 14)
                        .padding(.vertical, 8)
                }
                .buttonStyle(.plain)
                .glassEffect(.regular.tint(.orange.opacity(0.25)).interactive(), in: .capsule)
                .transition(.move(edge: .top).combined(with: .opacity))
            } else if let error = store.errorMessage {
                Label(error, systemImage: "exclamationmark.triangle.fill")
                    .font(.footnote.weight(.medium))
                    .lineLimit(2)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 8)
                    .glassEffect(.regular.tint(.red.opacity(0.25)).interactive(), in: .capsule)
                    .onTapGesture { store.errorMessage = nil }
                    .task(id: error) {
                        try? await Task.sleep(for: .seconds(4))
                        if store.errorMessage == error { store.errorMessage = nil }
                    }
                    .transition(.move(edge: .top).combined(with: .opacity))
            }
        }
        .padding(.top, 54)
        .padding(.horizontal, 20)
        .animation(.smooth, value: store.connectionNotice)
        .animation(.smooth, value: store.errorMessage)
        .animation(.smooth, value: store.needsRePairing)
    }
}
