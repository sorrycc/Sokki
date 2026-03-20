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
    var onQuit: (() -> Void)?

    override init() {
        self.statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.squareLength)
        super.init()

        if let button = statusItem.button {
            button.image = NSImage(systemSymbolName: "text.cursor", accessibilityDescription: "EspansoX")
        }

        menu.delegate = self
        statusItem.menu = menu
    }

    func updateIcon(enabled: Bool) {
        self.isEnabled = enabled
        let symbolName = enabled ? "text.cursor" : "text.cursor"
        if let button = statusItem.button {
            button.image = NSImage(systemSymbolName: symbolName, accessibilityDescription: "EspansoX")
            button.appearsDisabled = !enabled
        }
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
            button.toolTip = "EspansoX: \(error)"
        } else {
            statusItem.button?.toolTip = "EspansoX"
        }
    }

    // MARK: - NSMenuDelegate

    func menuNeedsUpdate(_ menu: NSMenu) {
        menu.removeAllItems()

        let version = Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "1.0.0"
        let versionItem = NSMenuItem(title: "EspansoX v\(version)", action: nil, keyEquivalent: "")
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

    @objc private func quitApp() {
        onQuit?()
    }
}
