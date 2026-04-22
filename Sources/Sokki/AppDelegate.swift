import AppKit
import Foundation

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    private var statusItemController: StatusItemController!
    let matchStore = MatchStore()
    let hudStore = HUDStore()
    private var hudPanelController: HUDPanelController!
    private var paletteController: PaletteController!
    let activationCoordinator = ActivationPolicyCoordinator()
    private let keystrokeMonitor = KeystrokeMonitor()
    private let bootstrapper = Bootstrapper()
    private var fsEventStream: FSEventStreamRef?
    private var debounceTimer: Timer?
    private var updater: UpdaterService?

    private lazy var recencyStore = RecencyStore(configDir: configDir)

    private let configDir = FileManager.default.homeDirectoryForCurrentUser
        .appendingPathComponent(".config/sokki")

    func applicationDidFinishLaunching(_ notification: Notification) {
        // Bootstrap config on first launch
        bootstrapper.run()

        // Check accessibility
        checkAccessibilityPermission()

        // Sparkle is only wired up for release bundles that carry SUFeedURL in Info.plist —
        // `swift run` dev builds skip the init to avoid the assertion crash.
        if Bundle.main.object(forInfoDictionaryKey: "SUFeedURL") != nil {
            updater = UpdaterService()
        }

        // Setup status item
        statusItemController = StatusItemController()
        setupStatusItemActions()

        // Load config
        matchStore.loadAll()
        applyConfig()

        // Setup FSEvents watcher
        startFSEventsWatcher()

        // Observe focus changes
        NSWorkspace.shared.notificationCenter.addObserver(
            self,
            selector: #selector(appDidActivate),
            name: NSWorkspace.didActivateApplicationNotification,
            object: nil
        )

        // Observe matches changes
        NotificationCenter.default.addObserver(
            self,
            selector: #selector(matchesDidChange),
            name: .matchesDidChange,
            object: nil
        )

        // Observe "Check for Updates" requests (posted from menu + Settings)
        NotificationCenter.default.addObserver(
            forName: .checkForUpdates,
            object: nil,
            queue: .main
        ) { [weak self] _ in
            Task { @MainActor in
                NSApp.activate()
                self?.updater?.checkForUpdates()
            }
        }

        // HUD panel. The closure must NOT capture self — HUDPanelController is owned by
        // AppDelegate, which is owned by NSApp; a self-capture here creates a retain cycle.
        hudPanelController = HUDPanelController(store: hudStore) { buffer in
            NotificationCenter.default.post(name: .openSettings, object: nil)
            // Defer one runloop turn so MatchListView's .onReceive subscription
            // has landed (first-open race when the Settings window is lazy-created).
            DispatchQueue.main.async {
                NotificationCenter.default.post(
                    name: .openMatchEditor,
                    object: nil,
                    userInfo: ["trigger": buffer]
                )
            }
        }

        // Palette controller (Spotlight-style fuzzy finder).
        paletteController = PaletteController(
            matchStore: matchStore,
            recency: recencyStore,
            renderer: keystrokeMonitor.variableRenderer,
            injector: keystrokeMonitor.textInjector,
            activation: activationCoordinator,
            aiExecutor: keystrokeMonitor
        )

        // Start monitoring
        keystrokeMonitor.onSecureInputChanged = { [weak self] isSecure in
            self?.statusItemController.updateSecureInput(isSecure)
        }
        keystrokeMonitor.onToggled = { [weak self] isEnabled in
            self?.statusItemController.updateIcon(enabled: isEnabled)
        }
        keystrokeMonitor.onHUDRequested = { [weak self] in
            self?.hudPanelController.toggle()
        }
        keystrokeMonitor.onHUDEvent = { [weak self] event in
            self?.hudStore.record(event)
        }
        keystrokeMonitor.onPaletteToggleRequested = { [weak self] in
            self?.paletteController.toggle()
        }
        keystrokeMonitor.start()

        Logger.shared.debug("Sokki started")
    }

    func applicationWillTerminate(_ notification: Notification) {
        keystrokeMonitor.stop()
        stopFSEventsWatcher()
    }

    nonisolated func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        return false
    }

    // MARK: - Config

    private func applyConfig() {
        let config = matchStore.config
        Logger.level = config.logLevel == "debug" ? .debug : .error
        keystrokeMonitor.updateConfig(config)
        keystrokeMonitor.updateMatches(matchStore.matches)
        statusItemController.updateIcon(enabled: config.isEnabled)
        statusItemController.setLastError(matchStore.lastError)
    }

    // MARK: - Status Item Actions

    private func setupStatusItemActions() {
        statusItemController.onToggleEnabled = { [weak self] in
            guard let self else { return }
            self.matchStore.config.isEnabled.toggle()
            let enabled = self.matchStore.config.isEnabled
            self.keystrokeMonitor.tapContext.updateConfig(self.matchStore.config)
            self.statusItemController.updateIcon(enabled: enabled)
        }

        statusItemController.onReloadConfig = { [weak self] in
            self?.reloadConfig()
        }

        statusItemController.onOpenConfigFolder = { [weak self] in
            guard let self else { return }
            NSWorkspace.shared.open(self.configDir)
        }

        statusItemController.onShowLog = {
            let logFile = FileManager.default.homeDirectoryForCurrentUser
                .appendingPathComponent(".config/sokki/logs/sokki.log")
            NSWorkspace.shared.open(logFile)
        }

        statusItemController.onOpenSettings = {
            NotificationCenter.default.post(name: .openSettings, object: nil)
        }

        statusItemController.onCheckForUpdates = {
            NotificationCenter.default.post(name: .checkForUpdates, object: nil)
        }

        statusItemController.onQuit = {
            NSApplication.shared.terminate(nil)
        }
    }

    // MARK: - Reload

    private func reloadConfig() {
        matchStore.loadAll()
        applyConfig()
        Logger.shared.debug("Config reloaded")
    }

    // MARK: - Notifications

    @objc private func appDidActivate(_ notification: Notification) {
        keystrokeMonitor.tapContext.clearBuffer()
    }

    @objc private func matchesDidChange() {
        keystrokeMonitor.updateMatches(matchStore.matches)
    }

    // MARK: - Accessibility

    private func checkAccessibilityPermission() {
        let options = [
            "AXTrustedCheckOptionPrompt": true
        ] as CFDictionary
        let trusted = AXIsProcessTrustedWithOptions(options)
        if !trusted {
            Logger.shared.warning("Accessibility permission not granted")
        }
    }

    // MARK: - FSEvents

    private func startFSEventsWatcher() {
        let paths = [configDir.path] as CFArray

        var context = FSEventStreamContext()
        context.info = Unmanaged.passUnretained(self).toOpaque()

        let flags: FSEventStreamCreateFlags =
            UInt32(kFSEventStreamCreateFlagUseCFTypes) |
            UInt32(kFSEventStreamCreateFlagFileEvents)

        guard let stream = FSEventStreamCreate(
            nil,
            { _, info, _, _, _, _ in
                guard let info else { return }
                let delegate = Unmanaged<AppDelegate>.fromOpaque(info).takeUnretainedValue()
                DispatchQueue.main.async {
                    delegate.debouncedReload()
                }
            },
            &context,
            paths,
            FSEventStreamEventId(kFSEventStreamEventIdSinceNow),
            0.3, // 300ms latency (acts as debounce)
            flags
        ) else {
            Logger.shared.error("Failed to create FSEvents stream")
            return
        }

        self.fsEventStream = stream
        FSEventStreamSetDispatchQueue(stream, DispatchQueue.main)
        FSEventStreamStart(stream)

        Logger.shared.debug("FSEvents watcher started for \(configDir.path)")
    }

    private func stopFSEventsWatcher() {
        guard let stream = fsEventStream else { return }
        FSEventStreamStop(stream)
        FSEventStreamInvalidate(stream)
        FSEventStreamRelease(stream)
        fsEventStream = nil
    }

    private func debouncedReload() {
        debounceTimer?.invalidate()
        debounceTimer = Timer.scheduledTimer(withTimeInterval: 0.3, repeats: false) { [weak self] _ in
            MainActor.assumeIsolated {
                self?.reloadConfig()
            }
        }
    }
}
