// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "herd",
    platforms: [.macOS(.v15)],
    products: [
        .executable(name: "herd", targets: ["herd"]),
        .library(name: "HerdCore", targets: ["HerdCore"]),
    ],
    dependencies: [
        .package(url: "https://github.com/hummingbird-project/hummingbird.git", from: "2.5.0"),
        .package(url: "https://github.com/hummingbird-project/hummingbird-websocket.git", from: "2.2.0"),
        .package(url: "https://github.com/apple/swift-argument-parser.git", from: "1.5.0"),
        .package(url: "https://github.com/swift-server/swift-service-lifecycle.git", from: "2.6.0"),
    ],
    targets: [
        .target(
            name: "HerdCore",
            dependencies: [
                .product(name: "Hummingbird", package: "hummingbird"),
                .product(name: "HummingbirdWebSocket", package: "hummingbird-websocket"),
                .product(name: "ServiceLifecycle", package: "swift-service-lifecycle"),
            ]
        ),
        .executableTarget(
            name: "herd",
            dependencies: [
                "HerdCore",
                .product(name: "ArgumentParser", package: "swift-argument-parser"),
            ]
        ),
        .testTarget(
            name: "HerdCoreTests",
            dependencies: [
                "HerdCore",
                .product(name: "HummingbirdTesting", package: "hummingbird"),
            ],
            resources: [.copy("Fixtures")]
        ),
    ]
)
