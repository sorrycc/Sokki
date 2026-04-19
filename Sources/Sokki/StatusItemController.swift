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

    // Monochrome keycap + wand icon for the menu bar (18x18 template image)
    private static func makeMenuBarIcon() -> NSImage {
        let size = NSSize(width: 18, height: 18)
        let image = NSImage(size: size, flipped: true) { rect in
            NSColor.black.setFill()
            NSColor.black.setStroke()

            // Keycap body (bottom-left area)
            let keycapOuter = NSBezierPath(roundedRect: NSRect(x: 0.5, y: 7, width: 10, height: 8), xRadius: 2, yRadius: 2)
            keycapOuter.lineWidth = 1.5
            keycapOuter.stroke()

            // Keycap top face (inset)
            let keycapTop = NSBezierPath(roundedRect: NSRect(x: 2.5, y: 8.5, width: 6, height: 4), xRadius: 1, yRadius: 1)
            keycapTop.fill()

            // Wand shaft (diagonal from keycap to upper-right)
            let wand = NSBezierPath()
            wand.lineWidth = 2
            wand.move(to: NSPoint(x: 8, y: 8))
            wand.line(to: NSPoint(x: 15, y: 1))
            wand.lineCapStyle = .round
            wand.stroke()

            // Sparkle at wand tip (small cross)
            let sparkle = NSBezierPath()
            sparkle.lineWidth = 1.2
            sparkle.lineCapStyle = .round
            sparkle.move(to: NSPoint(x: 13.5, y: 0.5))
            sparkle.line(to: NSPoint(x: 17.5, y: 0.5))
            sparkle.move(to: NSPoint(x: 15.5, y: -1.5))
            sparkle.line(to: NSPoint(x: 15.5, y: 2.5))
            sparkle.stroke()

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
