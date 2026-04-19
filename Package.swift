// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "Sokki",
    platforms: [
        .macOS(.v14),
    ],
    dependencies: [
        .package(url: "https://github.com/jpsim/Yams", from: "5.0.0"),
        .package(url: "https://github.com/sparkle-project/Sparkle", .upToNextMinor(from: "2.7.0")),
    ],
    targets: [
        .executableTarget(
            name: "Sokki",
            dependencies: [
                "Yams",
                "Sparkle",
            ],
            path: "Sources/Sokki",
            swiftSettings: [
                .swiftLanguageMode(.v6),
            ]
        ),
    ]
)
