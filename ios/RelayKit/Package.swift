// swift-tools-version: 6.0
import PackageDescription

// Shared, UI-free code for the app and future WidgetKit / ActivityKit extensions.
let package = Package(
    name: "RelayKit",
    platforms: [.iOS("26.0"), .macOS("26.0")],
    products: [
        .library(name: "RelayKit", targets: ["RelayKit"])
    ],
    targets: [
        .target(name: "RelayKit")
    ]
)
