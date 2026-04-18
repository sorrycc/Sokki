// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "Sokki",
    platforms: [
        .macOS(.v14),
    ],
    dependencies: [
        .package(url: "https://github.com/jpsim/Yams", from: "5.0.0"),
    ],
    targets: [
        .executableTarget(
            name: "Sokki",
            dependencies: [
                "Yams",
            ],
            path: "Sources/Sokki",
            swiftSettings: [
                .swiftLanguageMode(.v6),
            ]
        ),
    ]
)
