import SwiftUI

@main
struct HerdApp: App {
    @State private var model = AppModel()

    var body: some Scene {
        WindowGroup {
            Group {
                if let store = model.store {
                    MainView(store: store)
                } else {
                    PairingView()
                }
            }
            .environment(model)
            .onOpenURL { model.handle(url: $0) }
        }
    }
}
