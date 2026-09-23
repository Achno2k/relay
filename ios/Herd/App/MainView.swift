import HerdKit
import SwiftUI

/// Chat with a sidebar drawer underneath. Opening it pushes the chat right and dims it, like ChatGPT.
struct MainView: View {
    @Bindable var store: AppStore
    @Environment(AppModel.self) private var model
    @Environment(\.scenePhase) private var scenePhase

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
                    onNewChat: { presentNewChat(store.selectedAgent?.workspaceId) },
                    onUnpair: { model.unpair() }
                )
                .frame(width: width)
                .offset(x: -width * 0.25 * (1 - progress))
                .accessibilityHidden(progress == 0)
                .simultaneousGesture(sidebarOpen ? drawerDrag(width: width) : nil)

                ChatView(
                    store: store,
                    onOpenSidebar: { setSidebar(true) },
                    onNewChat: { presentNewChat($0) }
                )
                .overlay {
                    Color.black
                        .opacity(0.3 * progress)
                        .ignoresSafeArea()
                        .allowsHitTesting(progress > 0)
                        .onTapGesture { setSidebar(false) }
                        .gesture(drawerDrag(width: width))
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
                .offset(x: width * progress)
                .shadow(color: .black.opacity(0.12 * progress), radius: 20)
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
        withAnimation(.snappy(duration: 0.32)) {
            sidebarOpen = open
            dragX = 0
        }
    }

    private func drawerDrag(width: CGFloat) -> some Gesture {
        DragGesture(minimumDistance: 12)
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
