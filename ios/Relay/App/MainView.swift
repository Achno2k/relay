import RelayKit
import SwiftUI

/// Chat with a sidebar drawer underneath, animated like the Codex app: opening slides and fades the sidebar
/// in while the chat recedes (smaller, rounded, dimmed); closing eases the sidebar down and out while the chat
/// springs back. Everything follows one `progress`, so edge swipes and drags track the finger.
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

    var body: some View {
        GeometryReader { geo in
            let width = min(geo.size.width * 0.84, 360)
            let progress = openProgress(width: width)
            ZStack(alignment: .leading) {
                SidebarView(
                    store: store,
                    onSelect: { id in
                        store.open(id)
                        setSidebar(false)
                    },
                    onNewChat: { presentNewChat($0) },
                    onUnpair: { model.unpair() }
                )
                .frame(width: width)
                .opacity(reduceMotion ? progress : 0.3 + 0.7 * progress)
                .scaleEffect(reduceMotion ? 1 : 0.97 + 0.03 * progress, anchor: .top)
                .offset(x: reduceMotion ? 0 : -width * 0.12 * (1 - progress), y: reduceMotion ? 0 : 18 * (1 - progress))
                .zIndex(reduceMotion ? 1 : 0)
                .accessibilityHidden(progress == 0)
                // No drag-to-close on the sidebar itself: a left swipe there archives a chat.
                // The dimmed chat on the right still closes the drawer on tap or drag.

                ChatView(
                    store: store,
                    onOpenSidebar: { setSidebar(true) },
                    onNewChat: { presentNewChat($0) }
                )
                .overlay {
                    Color.black
                        .opacity(0.28 * progress)
                        .ignoresSafeArea()
                        .allowsHitTesting(progress > 0)
                        .onTapGesture { setSidebar(false) }
                        .gesture(drawerDrag(width: width))
                }
                .clipShape(.rect(cornerRadius: reduceMotion ? 0 : 38 * progress, style: .continuous))
                // A hairline edge, so the receded chat still reads as a card on a black sidebar in dark mode.
                .overlay {
                    if !reduceMotion && progress > 0 {
                        RoundedRectangle(cornerRadius: 38 * progress, style: .continuous)
                            .strokeBorder(Color(.separator).opacity(progress), lineWidth: 0.5)
                            .ignoresSafeArea()
                            .allowsHitTesting(false)
                    }
                }
                .scaleEffect(reduceMotion ? 1 : 1 - 0.05 * progress, anchor: .leading)
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
                .offset(x: reduceMotion ? 0 : width * progress)
                .shadow(color: .black.opacity(reduceMotion ? 0 : 0.18 * progress), radius: 24)
            }
        }
        .overlay(alignment: .top) { ConnectionBanner(store: store) }
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
        .sensoryFeedback(.impact(weight: .light), trigger: sidebarOpen)
        .task {
            store.start()
            if LaunchOptions.current.isDemo("newChat") {
                try? await Task.sleep(for: .milliseconds(600))
                presentNewChat(nil)
            }
        }
        .onChange(of: store.isApprovalSheetPresented) { _, presented in
            // The keyboard would cover the options.
            if presented { hideKeyboard() }
        }
        .onChange(of: scenePhase) { _, phase in
            if phase == .active { Task { await store.refresh() } }
        }
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

private struct NewChatRequest: Identifiable {
    let id = UUID()
    var workspaceId: String?
}

/// "Reconnecting…" pill while the socket is down, plus transient errors.
private struct ConnectionBanner: View {
    @Bindable var store: AppStore

    var body: some View {
        VStack(spacing: 8) {
            if store.connection == .reconnecting {
                Label("Reconnecting…", systemImage: "wifi.exclamationmark")
                    .font(.footnote.weight(.semibold))
                    .padding(.horizontal, 14)
                    .padding(.vertical, 8)
                    .glassEffect(.regular, in: .capsule)
                    .transition(.move(edge: .top).combined(with: .opacity))
            }
            if let error = store.errorMessage {
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
        .animation(.smooth, value: store.connection)
        .animation(.smooth, value: store.errorMessage)
    }
}
