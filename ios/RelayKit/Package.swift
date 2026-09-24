// swift-tools-version: 6.0
import PackageDescription

// Shared, UI-free code for the app and future WidgetKit / ActivityKit extensions.
let package = Package(
    name: "HerdKit",
    platforms: [.iOS("26.0"), .macOS("26.0")],
    products: [
        .library(name: "HerdKit", targets: ["HerdKit"])
    ],
    targets: [
        .target(name: "HerdKit")
    ]
)
