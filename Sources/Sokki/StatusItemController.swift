import AppKit

@MainActor
final class StatusItemController: NSObject, NSMenuDelegate {
    private let statusItem: NSStatusItem
    private let menu = NSMenu()
    private var isEnabled = true
    private var isSecureInput = false
    private var lastError: String?

    var onToggleEnabled: (() -> Void)?
    var onReloadConfig: (() -> Void)?
    var onOpenConfigFolder: (() -> Void)?
    var onShowLog: (() -> Void)?
    var onOpenSettings: (() -> Void)?
    var onCheckForUpdates: (() -> Void)?
    var onQuit: (() -> Void)?

    override init() {
        self.statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
        super.init()

        if let button = statusItem.button {
            button.image = Self.makeMenuBarIcon()
        }

        menu.delegate = self
        statusItem.menu = menu
    }

    func updateIcon(enabled: Bool) {
        self.isEnabled = enabled
        if let button = statusItem.button {
            button.image = Self.makeMenuBarIcon()
            button.appearsDisabled = !enabled
        }
    }

    // Monochrome keycap with tab glyph (⇥) for the menu bar (18x18 template image)
    private static func makeMenuBarIcon() -> NSImage {
        let size = NSSize(width: 18, height: 18)
        let image = NSImage(size: size, flipped: true) { _ in
            NSColor.black.setStroke()
            NSColor.black.setFill()

            // Keycap outline
            let keycap = NSBezierPath(
                roundedRect: NSRect(x: 1, y: 2.5, width: 16, height: 13),
                xRadius: 2.5,
                yRadius: 2.5
            )
            keycap.lineWidth = 1.25
            keycap.stroke()

            // Arrow shaft
            let shaft = NSBezierPath()
            shaft.lineWidth = 1.25
            shaft.lineCapStyle = .round
            shaft.move(to: NSPoint(x: 3.5, y: 9))
            shaft.line(to: NSPoint(x: 10, y: 9))
            shaft.stroke()

            // Arrowhead (filled triangle)
            let head = NSBezierPath()
            head.move(to: NSPoint(x: 10, y: 7))
            head.line(to: NSPoint(x: 12.5, y: 9))
            head.line(to: NSPoint(x: 10, y: 11))
            head.close()
            head.fill()

            // Terminator bar
            let bar = NSBezierPath()
            bar.lineWidth = 1.25
            bar.lineCapStyle = .round
            bar.move(to: NSPoint(x: 14, y: 6))
            bar.line(to: NSPoint(x: 14, y: 12))
            bar.stroke()

            return true
        }
        image.isTemplate = true
        return image
    }

    func updateSecureInput(_ isSecure: Bool) {
        self.isSecureInput = isSecure
        if let button = statusItem.button {
            button.appearsDisabled = isSecure || !isEnabled
        }
    }

    func setLastError(_ error: String?) {
        self.lastError = error
        if let button = statusItem.button, let error {
            button.toolTip = "Sokki: \(error)"
        } else {
            statusItem.button?.toolTip = "Sokki"
        }
    }

    // MARK: - NSMenuDelegate

    func menuNeedsUpdate(_ menu: NSMenu) {
        menu.removeAllItems()

        let version = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "1.0.0"
        let versionItem = NSMenuItem(title: "Sokki v\(version)", action: nil, keyEquivalent: "")
        versionItem.isEnabled = false
        menu.addItem(versionItem)

        if isSecureInput {
            let secureItem = NSMenuItem(title: "Secure Input Active", action: nil, keyEquivalent: "")
            secureItem.isEnabled = false
            menu.addItem(secureItem)
        }

        menu.addItem(.separator())

        let enabledItem = NSMenuItem(
            title: "Enabled",
            action: #selector(toggleEnabled),
            keyEquivalent: ""
        )
        enabledItem.target = self
        enabledItem.state = isEnabled ? .on : .off
        menu.addItem(enabledItem)

        let reloadItem = NSMenuItem(
            title: "Reload Config",
            action: #selector(reloadConfig),
            keyEquivalent: "r"
        )
        reloadItem.target = self
        menu.addItem(reloadItem)

        let openConfigItem = NSMenuItem(
            title: "Open Config Folder",
            action: #selector(openConfigFolder),
            keyEquivalent: ""
        )
        openConfigItem.target = self
        menu.addItem(openConfigItem)

        let showLogItem = NSMenuItem(
            title: "Show Log",
            action: #selector(showLog),
            keyEquivalent: ""
        )
        showLogItem.target = self
        menu.addItem(showLogItem)

        // Only show "Check for Updates…" in release bundles that carry SUFeedURL.
        if Bundle.main.object(forInfoDictionaryKey: "SUFeedURL") != nil {
            let checkUpdatesItem = NSMenuItem(
                title: "Check for Updates…",
                action: #selector(checkForUpdates),
                keyEquivalent: ""
            )
            checkUpdatesItem.target = self
            menu.addItem(checkUpdatesItem)
        }

        menu.addItem(.separator())

        let settingsItem = NSMenuItem(
            title: "Settings...",
            action: #selector(openSettings),
            keyEquivalent: ","
        )
        settingsItem.target = self
        menu.addItem(settingsItem)

        let quitItem = NSMenuItem(
            title: "Quit",
            action: #selector(quitApp),
            keyEquivalent: "q"
        )
        quitItem.target = self
        menu.addItem(quitItem)
    }

    @objc private func toggleEnabled() {
        onToggleEnabled?()
    }

    @objc private func reloadConfig() {
        onReloadConfig?()
    }

    @objc private func openConfigFolder() {
        onOpenConfigFolder?()
    }

    @objc private func showLog() {
        onShowLog?()
    }

    @objc private func openSettings() {
        onOpenSettings?()
    }

    @objc private func checkForUpdates() {
        onCheckForUpdates?()
    }

    @objc private func quitApp() {
        onQuit?()
    }
}
