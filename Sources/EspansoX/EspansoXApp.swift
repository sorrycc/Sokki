import SwiftUI

@main
struct EspansoXApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) var appDelegate
    @Environment(\.openWindow) private var openWindow

    var body: some Scene {
        Window("EspansoX Settings", id: "settings") {
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
                NSApp.activate(ignoringOtherApps: true)
                // The window will be shown by SwiftUI's Window scene
                for window in NSApp.windows {
                    if window.title == "EspansoX Settings" {
                        window.makeKeyAndOrderFront(nil)
                        return
                    }
                }
            }
        }
    }
}
