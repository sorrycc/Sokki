// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "EspansoX",
    platforms: [
        .macOS(.v14),
    ],
    dependencies: [
        .package(url: "https://github.com/jpsim/Yams", from: "5.0.0"),
    ],
    targets: [
        .executableTarget(
            name: "EspansoX",
            dependencies: [
                "Yams",
            ],
            path: "Sources/EspansoX",
            swiftSettings: [
                .swiftLanguageMode(.v6),
            ]
        ),
    ]
)
