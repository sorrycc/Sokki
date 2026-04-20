import AppKit
import SwiftUI

@MainActor
final class HUDPanelController {
    private var panel: NSPanel?
    private let store: HUDStore
    private let onCreateMatch: (String) -> Void

    init(store: HUDStore, onCreateMatch: @escaping (String) -> Void) {
        self.store = store
        self.onCreateMatch = onCreateMatch
    }

    func toggle() {
        if let panel, panel.isVisible {
            panel.orderOut(nil)
            return
        }
        if panel == nil { panel = makePanel() }
        // orderFrontRegardless avoids activating Sokki — the frontmost app keeps key.
        panel?.orderFrontRegardless()
    }

    private func makePanel() -> NSPanel {
        let p = NSPanel(
            contentRect: NSRect(x: 0, y: 0, width: 380, height: 420),
            styleMask: [.titled, .closable, .nonactivatingPanel, .hudWindow],
            backing: .buffered,
            defer: false
        )
        p.title = "Sokki Recent Activity"
        p.level = .floating
        p.isFloatingPanel = true
        p.becomesKeyOnlyIfNeeded = true
        p.hidesOnDeactivate = false
        p.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary]
        p.isReleasedWhenClosed = false
        p.center()
        p.contentView = NSHostingView(rootView: HUDView(
            store: store,
            onCreateMatch: { [weak self] buffer in self?.onCreateMatch(buffer) }
        ))
        return p
    }
}
