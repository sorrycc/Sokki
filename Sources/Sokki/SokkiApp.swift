import SwiftUI

@main
struct SokkiApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) var appDelegate
    @Environment(\.openWindow) private var openWindow

    var body: some Scene {
        Window("Sokki Settings", id: "settings") {
            SettingsView(store: appDelegate.matchStore)
        }
        .defaultSize(width: 550, height: 450)
    }

    init() {
        NotificationCenter.default.addObserver(
            forName: .openSettings,
            object: nil,
            queue: .main
        ) { _ in
            MainActor.assumeIsolated {
                // macOS Tahoe tightened activation for .accessory apps — flip to
                // .regular so the app can actually become frontmost and the window
                // can take key status (reverted on willClose below).
                NSApp.setActivationPolicy(.regular)
                NSApp.activate(ignoringOtherApps: true)
                for window in NSApp.windows {
                    if window.title == "Sokki Settings" {
                        window.makeKeyAndOrderFront(nil)
                        window.orderFrontRegardless()
                        NotificationCenter.default.addObserver(
                            forName: NSWindow.willCloseNotification,
                            object: window,
                            queue: .main
                        ) { _ in
                            MainActor.assumeIsolated {
                                _ = NSApp.setActivationPolicy(.accessory)
                            }
                        }
                        return
                    }
                }
            }
        }
    }
}
