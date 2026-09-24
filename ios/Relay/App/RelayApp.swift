import SwiftUI

@main
struct RelayApp: App {
    @State private var model = AppModel()

    var body: some Scene {
        WindowGroup {
            Group {
                if let store = model.store {
                    // Re-pairing swaps in a new store; a new identity re-runs MainView's `.task` so it starts.
                    MainView(store: store)
                        .id(ObjectIdentifier(store))
                } else {
                    PairingView()
                }
            }
            .environment(model)
            .onOpenURL { model.handle(url: $0) }
        }
    }
}
