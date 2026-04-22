import AppKit
import SwiftUI

/// Owns the Spotlight-style palette panel + the `⌘↵` editor window.
/// All state mutations happen on the main thread; `isShowing` is the
/// authoritative open/closed flag (NOT `panel.isVisible`, which has subtle
/// semantics during orderOut animations).
@MainActor
final class PaletteController {
    private let matchStore: MatchStore
    private let recency: RecencyStore
    private let renderer: VariableRenderer
    private let injector: TextInjector
    private let activation: ActivationPolicyCoordinator
    private let aiExecutor: KeystrokeMonitor

    private lazy var panel: NSPanel = makePanel()

    /// Borderless NSPanel defaults `canBecomeKey` to false, so AppKit refuses
    /// to route keystrokes to it even when the app is active. Override to
    /// force key + main status so the SwiftUI TextField's @FocusState sticks.
    private final class FocusablePanel: NSPanel {
        override var canBecomeKey: Bool { true }
        override var canBecomeMain: Bool { true }
    }
    private var editorWindow: NSWindow?
    private var mouseMonitor: Any?
    private var isShowing = false
    private var targetApp: NSRunningApplication?
    private var clipboardSnapshot: String?

    init(
        matchStore: MatchStore,
        recency: RecencyStore,
        renderer: VariableRenderer,
        injector: TextInjector,
        activation: ActivationPolicyCoordinator,
        aiExecutor: KeystrokeMonitor
    ) {
        self.matchStore = matchStore
        self.recency = recency
        self.renderer = renderer
        self.injector = injector
        self.activation = activation
        self.aiExecutor = aiExecutor

        // Force-init the lazy panel and observe key transitions on it directly
        // (avoids capturing `note` across actor boundaries).
        let p = panel
        NotificationCenter.default.addObserver(
            forName: NSWindow.didBecomeKeyNotification, object: p, queue: .main
        ) { [weak self] _ in
            MainActor.assumeIsolated {
                guard let self else { return }
                Logger.shared.error("[Palette] panel didBecomeKey — firstResponder=\(String(describing: self.panel.firstResponder))")
            }
        }
        NotificationCenter.default.addObserver(
            forName: NSWindow.didResignKeyNotification, object: p, queue: .main
        ) { _ in
            MainActor.assumeIsolated {
                Logger.shared.error("[Palette] panel didResignKey")
            }
        }
    }

    // MARK: - Toggle

    func toggle() {
        Logger.shared.error("[Palette] toggle() isShowing=\(isShowing)")
        if isShowing { close() } else { show() }
    }

    // MARK: - Show / close

    private func show() {
        Logger.shared.error("[Palette] show() entry — NSApp.isActive=\(NSApp.isActive) policy=\(NSApp.activationPolicy().rawValue)")
        if isShowing {
            panel.makeKeyAndOrderFront(nil)
            return
        }

        // Snapshot the user's prior app BEFORE we activate Sokki — once we flip
        // to .regular and activate, frontmost becomes Sokki itself.
        targetApp = NSWorkspace.shared.frontmostApplication
        clipboardSnapshot = NSPasteboard.general.string(forType: .string)
        Logger.shared.error("[Palette] snapshot target=\(targetApp?.bundleIdentifier ?? "nil")")

        let frame = computeFrame()
        panel.setFrame(frame, display: false)

        let view = PaletteView(
            matchStore: matchStore,
            recency: recency,
            renderer: renderer,
            clipboardSnapshot: clipboardSnapshot,
            onCommit: { [weak self] match in self?.commitInjection(match) },
            onEdit: { [weak self] match in self?.commitOpenEditor(match) },
            onClose: { [weak self] in self?.close() }
        )
        let hosting = NSHostingView(rootView: view)
        hosting.frame = NSRect(origin: .zero, size: frame.size)
        panel.contentView = hosting

        isShowing = true
        // macOS Tahoe gates key-window status on app activation: an .accessory
        // app's panel cannot become key (and TextField cannot receive keys)
        // unless the app is .regular and active. Mirrors SokkiApp.swift:22-38.
        Logger.shared.error("[Palette] before acquire — panel.canBecomeKey=\(panel.canBecomeKey) isKeyWindow=\(panel.isKeyWindow)")
        activation.acquire()
        Logger.shared.error("[Palette] after acquire — NSApp.isActive=\(NSApp.isActive) policy=\(NSApp.activationPolicy().rawValue)")
        panel.makeKeyAndOrderFront(nil)
        Logger.shared.error("[Palette] after makeKeyAndOrderFront — isVisible=\(panel.isVisible) isKeyWindow=\(panel.isKeyWindow) firstResponder=\(String(describing: panel.firstResponder))")

        // Global mouse monitor: any click in another app dismisses.
        mouseMonitor = NSEvent.addGlobalMonitorForEvents(
            matching: [.leftMouseDown, .rightMouseDown, .otherMouseDown]
        ) { [weak self] _ in
            Task { @MainActor in self?.close() }
        }

        // Settle check after a runloop turn — captures the state SwiftUI sees.
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            Logger.shared.error("[Palette] +async — NSApp.isActive=\(NSApp.isActive) panel.isKeyWindow=\(self.panel.isKeyWindow) firstResponder=\(String(describing: self.panel.firstResponder))")
        }
        DispatchQueue.main.asyncAfter(deadline: .now() + .milliseconds(150)) { [weak self] in
            guard let self else { return }
            Logger.shared.error("[Palette] +150ms — NSApp.isActive=\(NSApp.isActive) panel.isKeyWindow=\(self.panel.isKeyWindow) firstResponder=\(String(describing: self.panel.firstResponder))")
        }
    }

    private func close(reactivateTarget: Bool = true) {
        guard isShowing else { return }
        isShowing = false
        panel.orderOut(nil)
        if let monitor = mouseMonitor {
            NSEvent.removeMonitor(monitor)
            mouseMonitor = nil
        }
        // Release lease first so Sokki is no longer the active app, then bounce
        // focus back to the user's prior app (per user direction: always restore).
        activation.release()
        if reactivateTarget, let target = targetApp {
            target.activate(options: [])
        }
    }

    // MARK: - Commit

    private func commitInjection(_ match: Match) {
        // AI matches go through the keystroke monitor's AI flow. The monitor
        // owns focus-poll + selection capture + placeholder + run + cleanup.
        if match.ai != nil {
            let target = targetApp
            let trigger = match.trigger
            close(reactivateTarget: true)
            Task { @MainActor [weak self] in
                guard let self, let target else { return }
                await self.aiExecutor.handleAIMatchFromPalette(match: match, target: target)
                self.recency.bump(trigger)
            }
            return
        }

        close(reactivateTarget: true)

        let resolved = renderer.render(match.replace)
        let trigger = match.trigger
        // Small delay to let the target app reclaim focus after activate().
        // 30ms is generous; injection happens almost instantly otherwise.
        DispatchQueue.main.asyncAfter(deadline: .now() + .milliseconds(30)) { [weak self] in
            self?.injector.inject(replacement: resolved, triggerLength: 0)
            self?.recency.bump(trigger)
        }
    }

    private func commitOpenEditor(_ match: Match) {
        // Don't bounce focus back to the user's prior app — focus is going to
        // the editor window (which acquires its own lease below).
        close(reactivateTarget: false)

        let editor = MatchEditorView(
            match: match,
            availableFiles: matchStore.availableSourceFiles,
            prefillSourceFile: match.sourceFile ?? "default.yml",
            onSave: { [weak self] updated in
                self?.matchStore.update(updated)
                self?.editorWindow?.close()
            },
            onCancel: { [weak self] in
                self?.editorWindow?.close()
            }
        )

        if editorWindow == nil {
            let win = NSWindow(
                contentRect: NSRect(x: 0, y: 0, width: 480, height: 400),
                styleMask: [.titled, .closable],
                backing: .buffered,
                defer: false
            )
            win.title = "Edit Match"
            win.isReleasedWhenClosed = false
            win.center()
            editorWindow = win

            NotificationCenter.default.addObserver(
                forName: NSWindow.willCloseNotification,
                object: win,
                queue: .main
            ) { [weak self] _ in
                Task { @MainActor in self?.activation.release() }
            }
        }

        editorWindow?.contentView = NSHostingView(rootView: editor)
        activation.acquire()
        editorWindow?.makeKeyAndOrderFront(nil)
    }

    // MARK: - Panel construction

    private func makePanel() -> NSPanel {
        let p = FocusablePanel(
            contentRect: NSRect(x: 0, y: 0, width: 720, height: 420),
            styleMask: [.borderless],
            backing: .buffered,
            defer: false
        )
        p.level = .floating
        p.isFloatingPanel = true
        p.hidesOnDeactivate = false
        p.isMovableByWindowBackground = false
        p.becomesKeyOnlyIfNeeded = false
        p.collectionBehavior = [.canJoinAllSpaces, .stationary, .ignoresCycle]
        p.backgroundColor = .clear
        p.hasShadow = true
        return p
    }

    private func computeFrame() -> NSRect {
        let mouse = NSEvent.mouseLocation
        let screen = NSScreen.screens.first(where: { NSMouseInRect(mouse, $0.frame, false) })
            ?? NSScreen.main
            ?? NSScreen.screens.first!
        let visible = screen.visibleFrame
        let width: CGFloat = 720
        let height: CGFloat = 420
        let x = visible.minX + (visible.width - width) / 2
        // Position top edge ~25% down from the top of the visible frame.
        let topInset = visible.height * 0.25
        let y = visible.maxY - topInset - height
        return NSRect(x: x, y: y, width: width, height: height)
    }
}

// MARK: - Activation policy coordinator

/// Ref-counted activation-policy lease so multiple windows that need
/// `.regular` (Settings, palette editor) can coexist without flicker.
@MainActor
final class ActivationPolicyCoordinator {
    private var leases = 0

    func acquire() {
        leases += 1
        if leases == 1 {
            NSApp.setActivationPolicy(.regular)
            NSApp.activate(ignoringOtherApps: true)
        }
    }

    func release() {
        leases = max(0, leases - 1)
        if leases == 0 {
            NSApp.setActivationPolicy(.accessory)
        }
    }
}
