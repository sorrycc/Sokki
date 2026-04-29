import AppKit
import Carbon.HIToolbox
import Foundation

@MainActor
final class KeystrokeMonitor {
    let tapContext = TapContext()

    private var tapThread: Thread?
    private var isRunning = false
    private var pendingCancelMouseMonitor: Any?
    private var appSwitchObserver: NSObjectProtocol?

    let variableRenderer = VariableRenderer()
    let textInjector = TextInjector()

    let aiResolver = ClaudeBinaryResolver()
    lazy var aiRewriter: AIRewriter = AIRewriter(
        resolver: aiResolver,
        scratchCwd: aiScratchDirectory()
    )
    private var appConfig = AppConfig()
    private let aiClipboardManager = ClipboardManager()

    // Stored as nonisolated-accessible via the TapState wrapper
    private let tapState = TapState()

    var onSecureInputChanged: ((Bool) -> Void)?
    var onToggled: ((Bool) -> Void)?
    var onHUDRequested: (() -> Void)?
    var onHUDEvent: ((HUDEvent) -> Void)?
    var onPaletteToggleRequested: (() -> Void)?

    func start() {
        guard !isRunning else { return }
        isRunning = true

        let state = tapState
        let context = tapContext
        let onMatch: @Sendable (Match, Int) -> Void = { [weak self] match, triggerLength in
            DispatchQueue.main.async {
                guard let self else { return }
                if match.isAI {
                    Task { @MainActor in
                        await self.handleAIMatch(match: match, triggerLength: triggerLength)
                    }
                    return
                }
                let rendered = self.variableRenderer.render(match.replace)
                self.textInjector.inject(replacement: rendered, triggerLength: triggerLength)
                self.tapContext.recordExpansion(trigger: match.trigger, replacement: rendered)
                self.tapContext.setSuppressMatching(false)
            }
        }
        let onAICancel: @Sendable () -> Void = { [weak self] in
            DispatchQueue.main.async {
                self?.aiRewriter.cancelCurrent()
            }
        }
        let onUndo: @Sendable (String) -> Void = { [weak self] trigger in
            DispatchQueue.main.async {
                guard let self else { return }
                self.injectText(trigger)
                self.tapContext.setSuppressMatching(false)
            }
        }
        let onSecure: @Sendable (Bool) -> Void = { [weak self] isSecure in
            DispatchQueue.main.async {
                self?.onSecureInputChanged?(isSecure)
            }
        }
        let onToggle: @Sendable (Bool) -> Void = { [weak self] enabled in
            DispatchQueue.main.async {
                self?.onToggled?(enabled)
            }
        }
        let onHUDRequest: @Sendable () -> Void = { [weak self] in
            DispatchQueue.main.async {
                self?.onHUDRequested?()
            }
        }
        let onHUDEv: @Sendable (HUDEvent) -> Void = { [weak self] event in
            DispatchQueue.main.async {
                self?.onHUDEvent?(event)
            }
        }
        let onPaletteToggle: @Sendable () -> Void = { [weak self] in
            DispatchQueue.main.async {
                self?.onPaletteToggleRequested?()
            }
        }

        let tapThread = Thread {
            KeystrokeMonitor.runEventTap(
                state: state,
                context: context,
                onMatch: onMatch,
                onUndo: onUndo,
                onSecureInputChanged: onSecure,
                onToggled: onToggle,
                onHUDRequested: onHUDRequest,
                onHUDEvent: onHUDEv,
                onPaletteToggleRequested: onPaletteToggle,
                onAICancelRequested: onAICancel
            )
        }
        tapThread.name = "Sokki.EventTap"
        tapThread.qualityOfService = .userInteractive
        self.tapThread = tapThread
        tapThread.start()

        // Pending-match timer: fire callback (mirrors keystroke matched branch
        // ordering at TapHandler.handleEvent :750,757,758). `onMatch`,
        // `onHUDEv`, and `context` are already-@Sendable locals from this scope.
        let timeoutFire: @Sendable (Match, Int) -> Void = { [context, onMatch, onHUDEv] match, triggerLength in
            onHUDEv(HUDEvent(
                id: UUID(),
                timestamp: Date(),
                verdict: .matched,
                capturedBuffer: match.trigger,
                matchedTrigger: match.trigger
            ))
            context.setSuppressMatching(true)
            onMatch(match, triggerLength)
        }
        context.setPendingMatchFiredCallback(timeoutFire)

        // Mouse-click cancellation: any click anywhere drops a pending match.
        // Distinct from `aiMouseMonitor` (scoped to AI-request lifetime).
        pendingCancelMouseMonitor = NSEvent.addGlobalMonitorForEvents(
            matching: [.leftMouseDown, .rightMouseDown, .otherMouseDown]
        ) { [context] _ in
            context.cancelPendingMatch()
        }

        // App-switch cancellation: Cmd+Tab away with no mouse click.
        // `queue: nil` posts on the posting thread; `cancelPendingMatch` is
        // thread-safe via the TapContext lock.
        appSwitchObserver = NSWorkspace.shared.notificationCenter.addObserver(
            forName: NSWorkspace.didDeactivateApplicationNotification,
            object: nil,
            queue: nil
        ) { [context] _ in
            context.cancelPendingMatch()
        }

        Logger.shared.debug("KeystrokeMonitor started")
    }

    func stop() {
        isRunning = false
        tapState.disable()
        if let m = pendingCancelMouseMonitor {
            NSEvent.removeMonitor(m)
            pendingCancelMouseMonitor = nil
        }
        if let obs = appSwitchObserver {
            NSWorkspace.shared.notificationCenter.removeObserver(obs)
            appSwitchObserver = nil
        }
        tapContext.cancelPendingMatch()
        tapThread = nil
        Logger.shared.debug("KeystrokeMonitor stopped")
    }

    func updateConfig(_ config: AppConfig) {
        tapContext.updateConfig(config)
        textInjector.config = config
        appConfig = config
    }

    private func aiScratchDirectory() -> URL {
        let appSupport = FileManager.default.urls(
            for: .applicationSupportDirectory,
            in: .userDomainMask
        ).first ?? FileManager.default.temporaryDirectory
        let dir = appSupport.appendingPathComponent("Sokki/ai-scratch", isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        return dir
    }

    func updateMatches(_ matches: [Match]) {
        tapContext.updateMatches(matches)
    }

    // MARK: - AI flow

    private static let placeholderWord = "generating…"
    private static let brailleFrames: [Character] = ["⠋","⠙","⠹","⠸","⠼","⠴","⠦","⠧","⠇","⠏"]
    private static let blockFrames:   [Character] = ["▏","▎","▍","▌","▋","▊","▉","█"]

    private static func placeholderFrame(style: PlaceholderStyle, index: Int) -> String {
        switch style {
        case .static:
            return placeholderWord
        case .braille:
            return String(brailleFrames[index % brailleFrames.count])
        case .blockSweep:
            return String(blockFrames[index % blockFrames.count])
        }
    }

    private var animationTask: Task<Void, Never>?
    private var aiPlaceholderLength: Int = 0

    enum AIInvocation: Sendable {
        case typedTrigger(length: Int)
        case palette

        var label: String {
            switch self {
            case .typedTrigger: return "typed"
            case .palette:      return "palette"
            }
        }
    }

    private var aiMouseMonitor: Any?

    /// Entry point for typed-trigger AI matches. Called from `onMatch` on
    /// MainActor. `suppressMatching` is already true (set on the tap thread
    /// before dispatch).
    private func handleAIMatch(match: Match, triggerLength: Int) async {
        await runAIFlow(match: match, invocation: .typedTrigger(length: triggerLength))
    }

    /// Entry point for palette-committed AI matches. Polls until the target
    /// app regains frontmost status (up to ~80 ms), then delegates.
    func handleAIMatchFromPalette(match: Match, target: NSRunningApplication) async {
        let deadline = Date().addingTimeInterval(0.08)
        while Date() < deadline,
              NSWorkspace.shared.frontmostApplication?.processIdentifier != target.processIdentifier {
            try? await Task.sleep(nanoseconds: 10_000_000)
        }
        await runAIFlow(match: match, invocation: .palette)
    }

    private func runAIFlow(match: Match, invocation: AIInvocation) async {
        guard let promptRaw = match.prompt else {
            tapContext.setSuppressMatching(false)
            return
        }

        // Parse the template. Validated at load, so parse cannot fail for a
        // match that reached us; treat a throw as a protocol error.
        let template: PromptTemplate
        do {
            template = try PromptTemplate.parse(promptRaw)
        } catch {
            Logger.shared.error("[AI] template parse failed (should not happen): \(error.localizedDescription)")
            emitHUD(.aiFailed, trigger: match.trigger, note: "bad prompt")
            tapContext.setSuppressMatching(false)
            return
        }

        // §5.2 Concurrency guard: refuse overlapping AI runs. Checked BEFORE
        // any state mutation / clipboard save / injection, so an in-flight
        // run is not disturbed.
        if tapContext.isAIActive {
            Logger.shared.warning("[AI] refused — another AI run is in flight")
            emitHUD(.aiFailed, trigger: match.trigger, note: "AI busy")
            return
        }

        let started = Date()
        Logger.shared.debug(
            "[AI] match trigger=\(match.trigger) invocation=\(invocation.label) promptLen=\(promptRaw.count) placeholders=\(template.placeholders.count) model=\(appConfig.ai.model)"
        )

        // Idempotent: palette path needs this set; typed path already has it.
        tapContext.setSuppressMatching(true)

        // Erase the typed trigger, if there was one.
        switch invocation {
        case .typedTrigger(let n):
            textInjector.eraseInjected(count: n)
        case .palette:
            break
        }

        // Capture inputs, in this order: clipboard FIRST (before Cmd-C
        // overwrites the pasteboard), then selection via Cmd-C.
        var clipboardText: String? = nil
        if template.needsClipboard {
            let s = NSPasteboard.general.string(forType: .string) ?? ""
            clipboardText = s
            Logger.shared.debug("[AI] clipboard snapshot len=\(s.count)")
        }

        var selectionText: String? = nil
        if template.needsSelection {
            let captureStart = Date()
            let captured = await captureSelectionViaCmdC(
                timeoutMs: appConfig.ai.selectionCaptureTimeoutMs,
                invocation: invocation
            )
            let captureMs = Int(Date().timeIntervalSince(captureStart) * 1000)
            if let captured {
                selectionText = captured
                Logger.shared.debug("[AI] selection captured len=\(captured.count) in \(captureMs)ms")
            } else if template.requiresSelection {
                Logger.shared.warning("[AI] no selection (timeout=\(appConfig.ai.selectionCaptureTimeoutMs)ms, elapsed=\(captureMs)ms)")
                if case .typedTrigger = invocation {
                    textInjector.injectDelta(match.trigger)
                }
                emitHUD(.aiFailed, trigger: match.trigger, note: "no selection")
                tapContext.setSuppressMatching(false)
                return
            } else {
                // Selection missing but some chain has a non-selection
                // fallback (e.g. `{{s|c}}`). Leave selectionText = nil and let
                // render() pick the next source; if every chain is unresolved
                // the render-abort branch below will surface it.
                Logger.shared.warning("[AI] selection missing — falling back to chain (elapsed=\(captureMs)ms)")
            }
        }

        // Render. If any placeholder chain is all-empty, abort with a HUD
        // note naming the offending placeholder.
        guard let rendered = template.render(selection: selectionText, clipboard: clipboardText) else {
            let chain = template.firstUnresolvedChain(selection: selectionText, clipboard: clipboardText) ?? "{{…}}"
            Logger.shared.warning("[AI] render aborted: no input for \(chain)")
            if case .typedTrigger = invocation {
                textInjector.injectDelta(match.trigger)
            }
            emitHUD(.aiFailed, trigger: match.trigger, note: "no input for \(chain)")
            tapContext.setSuppressMatching(false)
            return
        }

        // Inject placeholder (frame 0) while still suppressed. For palette+selection
        // this single unicode-string event replaces the still-live selection.
        let style = appConfig.ai.placeholderStyle
        let initialFrame = Self.placeholderFrame(style: style, index: 0)
        textInjector.injectDelta(initialFrame)
        aiPlaceholderLength = initialFrame.count

        // Drain the HID pipeline so our own synthesized events arrive at the
        // tap while suppressMatching is still true.
        try? await Task.sleep(nanoseconds: 50_000_000) // 50ms

        tapContext.setActiveAIRequest(true)
        tapContext.setSuppressMatching(false)
        beginAIMouseWatch()

        emitHUD(.aiThinking, trigger: match.trigger, note: nil)
        Logger.shared.debug("[AI] placeholder injected (style=\(style.rawValue)), dispatching request")
        startAnimationTaskIfNeeded(style: style)

        let request = AIRewriter.Request(
            prompt: rendered,
            model: appConfig.ai.model,
            timeout: TimeInterval(appConfig.ai.timeoutSeconds),
            userArgs: appConfig.ai.args
        )
        let event = await aiRewriter.run(request, aiBinaryPath: appConfig.ai.binary)

        // Fence suppression BEFORE cancelling the animation so a cooperative
        // resume in the listen window cannot flip suppressMatching back to false.
        tapContext.setSuppressMatching(true)
        animationTask?.cancel()
        animationTask = nil
        endAIMouseWatch()
        let typed = tapContext.consumeUserTypedDuringAI()
        let placeholderLength = aiPlaceholderLength
        let elapsedMs = Int(Date().timeIntervalSince(started) * 1000)

        switch event {
        case .completed(let text):
            Logger.shared.debug("[AI] completed len=\(text.count) typedDuringWait=\(typed) elapsed=\(elapsedMs)ms")
            if !typed {
                textInjector.eraseInjected(count: placeholderLength)
                textInjector.injectDelta(text)
                emitHUD(.aiCompleted, trigger: match.trigger, note: nil)
            } else {
                textInjector.injectDelta(text)
                emitHUD(
                    .aiCompleted,
                    trigger: match.trigger,
                    note: "result appended; loading indicator left inline — delete manually"
                )
            }
            if case .typedTrigger = invocation {
                tapContext.recordExpansion(trigger: match.trigger, replacement: text)
            }
            tapContext.clearBuffer()

        case .failed(let reason):
            Logger.shared.warning("[AI] failed reason=\(reasonString(reason)) typedDuringWait=\(typed) elapsed=\(elapsedMs)ms")
            restoreAfterAIFailure(
                trigger: match.trigger,
                selection: selectionText,
                placeholderLength: placeholderLength,
                typed: typed,
                invocation: invocation
            )
            emitHUD(.aiFailed, trigger: match.trigger, note: reasonString(reason))

        case .cancelled:
            Logger.shared.warning("[AI] cancelled typedDuringWait=\(typed) elapsed=\(elapsedMs)ms")
            restoreAfterAIFailure(
                trigger: match.trigger,
                selection: selectionText,
                placeholderLength: placeholderLength,
                typed: typed,
                invocation: invocation
            )
            emitHUD(.aiFailed, trigger: match.trigger, note: "cancelled")
        }

        tapContext.setSuppressMatching(false)
    }

    private func restoreAfterAIFailure(
        trigger: String,
        selection: String?,
        placeholderLength: Int,
        typed: Bool,
        invocation: AIInvocation
    ) {
        if typed { return }
        textInjector.eraseInjected(count: placeholderLength)
        if let selection, !selection.isEmpty {
            textInjector.injectDelta(selection)
        } else if case .typedTrigger = invocation {
            // Typed path: re-inject the trigger literal so the user's context
            // is preserved. Palette path: nothing to re-inject.
            textInjector.injectDelta(trigger)
        }
    }

    /// Drive the animated placeholder (no-op for `.static`). Each frame cycle:
    /// suppress → erase prior frame → inject next frame → drain 70ms → unsuppress →
    /// listen 50ms. Self-cancels on user typing via a non-destructive peek so the
    /// completion branch's atomic `consumeUserTypedDuringAI` is preserved.
    private func startAnimationTaskIfNeeded(style: PlaceholderStyle) {
        guard style != .static else { return }
        animationTask?.cancel()
        animationTask = Task { @MainActor [weak self] in
            var index = 1 // frame 0 already injected by runAIFlow
            // Show frame 0 for a full listen window before the first swap.
            try? await Task.sleep(nanoseconds: 50_000_000)

            while !Task.isCancelled {
                guard let self else { return }
                if self.tapContext.peekUserTypedDuringAI() { return }

                self.tapContext.setSuppressMatching(true)
                self.textInjector.eraseInjected(count: self.aiPlaceholderLength)
                let frame = Self.placeholderFrame(style: style, index: index)
                self.textInjector.injectDelta(frame)
                self.aiPlaceholderLength = frame.count

                try? await Task.sleep(nanoseconds: 70_000_000)
                if Task.isCancelled { return }
                self.tapContext.setSuppressMatching(false)

                try? await Task.sleep(nanoseconds: 50_000_000)
                index &+= 1
            }
        }
    }

    // §5.1 Mouse-click detection during AI wait. A click in the target app
    // can move the caret, which would make `eraseInjected(placeholderLength)`
    // delete unrelated user text on completion. Treat clicks the same as
    // keystrokes for the detect-and-degrade path. Global monitor — Sokki's
    // own windows don't fire it, so HUD / palette clicks don't false-positive.
    private func beginAIMouseWatch() {
        guard aiMouseMonitor == nil else { return }
        aiMouseMonitor = NSEvent.addGlobalMonitorForEvents(
            matching: [.leftMouseDown, .rightMouseDown, .otherMouseDown]
        ) { [weak self] _ in
            self?.tapContext.noteUserTypedDuringAI()
        }
    }

    private func endAIMouseWatch() {
        if let m = aiMouseMonitor {
            NSEvent.removeMonitor(m)
            aiMouseMonitor = nil
        }
    }

    private func reasonString(_ reason: AIRewriter.Reason) -> String {
        switch reason {
        case .timeout: return "timeout"
        case .binaryMissing: return "`claude` binary not found"
        case .authRequired: return "run `claude auth login` to sign in"
        case .nonZeroExit(let code, let tail):
            let trimmed = tail.prefix(120)
            return "exit \(code): \(trimmed)"
        case .protocolError(let s):
            return "protocol: \(s)"
        }
    }

    private func emitHUD(_ verdict: HUDEvent.Verdict, trigger: String, note: String?) {
        let event = HUDEvent(
            verdict: verdict,
            capturedBuffer: trigger,
            matchedTrigger: trigger,
            note: note
        )
        onHUDEvent?(event)
    }

    /// Save clipboard → Cmd+C → poll → read selection → restore.
    /// Returns nil when nothing was selected (changeCount didn't tick).
    ///
    /// For typed-trigger invocations, the trigger has already been erased so
    /// the selection is still live after Cmd+C; we post a Backspace to delete
    /// it before the caller injects the placeholder. For palette
    /// invocations, the placeholder's unicode-string event replaces the
    /// still-live selection on its own — no Backspace needed (posting one
    /// would delete a character of the preceding text).
    private func captureSelectionViaCmdC(timeoutMs: Int, invocation: AIInvocation) async -> String? {
        let pb = NSPasteboard.general
        let before = pb.changeCount
        aiClipboardManager.save()

        postCmdC()

        let deadline = Date().addingTimeInterval(Double(timeoutMs) / 1000.0)
        while Date() < deadline, pb.changeCount == before {
            try? await Task.sleep(nanoseconds: 10_000_000) // 10ms
        }

        guard pb.changeCount > before,
              let text = pb.string(forType: .string),
              !text.isEmpty
        else {
            aiClipboardManager.restore()
            return nil
        }

        if case .typedTrigger = invocation {
            postBackspaceOnce()
        }
        aiClipboardManager.restore()
        return text
    }

    private func postCmdC() {
        let source = CGEventSource(stateID: .hidSystemState)
        let cKeyCode: CGKeyCode = 0x08
        if let down = CGEvent(keyboardEventSource: source, virtualKey: cKeyCode, keyDown: true),
           let up = CGEvent(keyboardEventSource: source, virtualKey: cKeyCode, keyDown: false) {
            down.flags = .maskCommand
            up.flags = .maskCommand
            down.post(tap: .cghidEventTap)
            up.post(tap: .cghidEventTap)
        }
    }

    private func postBackspaceOnce() {
        let source = CGEventSource(stateID: .hidSystemState)
        let bs: CGKeyCode = 0x33
        if let down = CGEvent(keyboardEventSource: source, virtualKey: bs, keyDown: true) {
            down.post(tap: .cghidEventTap)
        }
        if let up = CGEvent(keyboardEventSource: source, virtualKey: bs, keyDown: false) {
            up.post(tap: .cghidEventTap)
        }
    }

    private func injectText(_ text: String) {
        let source = CGEventSource(stateID: .hidSystemState)
        for char in text {
            let str = String(char)
            let utf16 = Array(str.utf16)
            guard let event = CGEvent(keyboardEventSource: source, virtualKey: 0, keyDown: true) else {
                continue
            }
            event.keyboardSetUnicodeString(stringLength: utf16.count, unicodeString: utf16)
            event.post(tap: .cghidEventTap)

            if let upEvent = CGEvent(keyboardEventSource: source, virtualKey: 0, keyDown: false) {
                upEvent.post(tap: .cghidEventTap)
            }
        }
    }

    // MARK: - Static tap setup (runs on tap thread)

    private nonisolated static func runEventTap(
        state: TapState,
        context: TapContext,
        onMatch: @escaping @Sendable (Match, Int) -> Void,
        onUndo: @escaping @Sendable (String) -> Void,
        onSecureInputChanged: @escaping @Sendable (Bool) -> Void,
        onToggled: @escaping @Sendable (Bool) -> Void,
        onHUDRequested: @escaping @Sendable () -> Void,
        onHUDEvent: @escaping @Sendable (HUDEvent) -> Void,
        onPaletteToggleRequested: @escaping @Sendable () -> Void,
        onAICancelRequested: @escaping @Sendable () -> Void
    ) {
        let eventMask: CGEventMask =
            (1 << CGEventType.keyDown.rawValue) |
            (1 << CGEventType.flagsChanged.rawValue)

        let handler = TapHandler(
            context: context,
            onMatch: onMatch,
            onUndo: onUndo,
            onSecureInputChanged: onSecureInputChanged,
            onToggled: onToggled,
            onHUDRequested: onHUDRequested,
            onHUDEvent: onHUDEvent,
            onPaletteToggleRequested: onPaletteToggleRequested,
            onAICancelRequested: onAICancelRequested
        )
        let handlerPtr = Unmanaged.passRetained(handler).toOpaque()

        guard let tap = CGEvent.tapCreate(
            tap: .cgSessionEventTap,
            place: .headInsertEventTap,
            options: .defaultTap,
            eventsOfInterest: eventMask,
            callback: { _, type, event, refcon -> Unmanaged<CGEvent>? in
                guard let refcon else { return Unmanaged.passRetained(event) }
                let handler = Unmanaged<TapHandler>.fromOpaque(refcon).takeUnretainedValue()
                return handler.handleEvent(type: type, event: event)
            },
            userInfo: handlerPtr
        ) else {
            Logger.shared.error("Failed to create CGEvent tap. Check Accessibility permissions.")
            Unmanaged<TapHandler>.fromOpaque(handlerPtr).release()
            return
        }

        state.setTap(tap)

        let source = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, tap, 0)
        CFRunLoopAddSource(CFRunLoopGetCurrent(), source, .commonModes)
        CGEvent.tapEnable(tap: tap, enable: true)
        CFRunLoopRun()

        // Cleanup
        Unmanaged<TapHandler>.fromOpaque(handlerPtr).release()
    }
}

// MARK: - TapState (thread-safe tap reference)

private final class TapState: @unchecked Sendable {
    private var tap: CFMachPort?
    private let lock = NSLock()

    func setTap(_ tap: CFMachPort) {
        lock.lock()
        self.tap = tap
        lock.unlock()
    }

    func reenable() {
        lock.lock()
        if let tap { CGEvent.tapEnable(tap: tap, enable: true) }
        lock.unlock()
    }

    func disable() {
        lock.lock()
        if let tap { CGEvent.tapEnable(tap: tap, enable: false) }
        lock.unlock()
    }
}

// MARK: - TapHandler (event processing, lives on tap thread)

private final class TapHandler: @unchecked Sendable {
    let context: TapContext
    let onMatch: @Sendable (Match, Int) -> Void
    let onUndo: @Sendable (String) -> Void
    let onSecureInputChanged: @Sendable (Bool) -> Void
    let onToggled: @Sendable (Bool) -> Void
    let onHUDRequested: @Sendable () -> Void
    let onHUDEvent: @Sendable (HUDEvent) -> Void
    let onPaletteToggleRequested: @Sendable () -> Void
    let onAICancelRequested: @Sendable () -> Void

    init(
        context: TapContext,
        onMatch: @escaping @Sendable (Match, Int) -> Void,
        onUndo: @escaping @Sendable (String) -> Void,
        onSecureInputChanged: @escaping @Sendable (Bool) -> Void,
        onToggled: @escaping @Sendable (Bool) -> Void,
        onHUDRequested: @escaping @Sendable () -> Void,
        onHUDEvent: @escaping @Sendable (HUDEvent) -> Void,
        onPaletteToggleRequested: @escaping @Sendable () -> Void,
        onAICancelRequested: @escaping @Sendable () -> Void
    ) {
        self.context = context
        self.onMatch = onMatch
        self.onUndo = onUndo
        self.onSecureInputChanged = onSecureInputChanged
        self.onToggled = onToggled
        self.onHUDRequested = onHUDRequested
        self.onHUDEvent = onHUDEvent
        self.onPaletteToggleRequested = onPaletteToggleRequested
        self.onAICancelRequested = onAICancelRequested
    }

    func handleEvent(type: CGEventType, event: CGEvent) -> Unmanaged<CGEvent>? {
        // Re-enable tap if disabled
        if type == .tapDisabledByTimeout || type == .tapDisabledByUserInput {
            return Unmanaged.passRetained(event)
        }

        // Handle modifier changes (toggle + HUD hotkeys)
        if type == .flagsChanged {
            let flags = event.flags
            let result = context.handleModifierTap(flags: flags)
            switch result.action {
            case .toggled(let enabled):
                onToggled(enabled)
            case .hudRequested:
                onHUDRequested()
            default:
                break
            }
            return Unmanaged.passRetained(event)
        }

        guard type == .keyDown else {
            return Unmanaged.passRetained(event)
        }

        let flags = event.flags
        let keyCode = UInt16(event.getIntegerValueField(.keyboardEventKeycode))

        // AI: Esc cancels an in-flight request and is swallowed so it doesn't
        // reach the focused app. Any other key during an AI wait is noted as
        // user-typing (gated against self-echo inside noteUserTypedDuringAI).
        let escKeyCode: UInt16 = 0x35
        if context.isAIActive {
            if keyCode == escKeyCode {
                onAICancelRequested()
                return nil
            }
            context.noteUserTypedDuringAI()
        }

        // Palette chord — swallow the event by returning nil so the system never
        // sees, e.g., ⌥Space (which would otherwise switch input source).
        if context.handlePaletteChord(keyCode: keyCode, flags: flags) {
            onPaletteToggleRequested()
            return nil
        }

        // Detect Cmd+V (paste)
        if flags.contains(.maskCommand) && keyCode == 0x09 {
            context.handlePasteDetected()
            return Unmanaged.passRetained(event)
        }

        // Skip keys with Cmd/Ctrl held (except backspace). User taking a
        // shortcut action — drop any pending match.
        let backspaceKeyCode: UInt16 = 0x33
        if keyCode != backspaceKeyCode && (flags.contains(.maskCommand) || flags.contains(.maskControl)) {
            context.cancelPendingMatch()
            return Unmanaged.passRetained(event)
        }

        // Extract character
        var unicodeLength: Int = 0
        var unicodeBuffer = [UniChar](repeating: 0, count: 4)
        event.keyboardGetUnicodeString(
            maxStringLength: 4,
            actualStringLength: &unicodeLength,
            unicodeString: &unicodeBuffer
        )

        let character: Character
        if keyCode == backspaceKeyCode {
            character = "\u{8}"
        } else if unicodeLength > 0 {
            let str = String(utf16CodeUnits: unicodeBuffer, count: unicodeLength)
            guard let firstChar = str.first else {
                return Unmanaged.passRetained(event)
            }
            character = firstChar
        } else {
            return Unmanaged.passRetained(event)
        }

        Logger.shared.debug("Key: \(character) (code: \(keyCode))")

        let result = context.feed(character: character, keyCode: keyCode, modifierFlags: flags)

        switch result.action {
        case .none:
            break
        case .matched(let match, let triggerLength):
            Logger.shared.debug("Match found: \(match.trigger) -> \(match.replace)")
            // capturedBuffer is the trigger, not the full rolling buffer — the
            // remaining 100+ chars of prior typing are not diagnostically useful here.
            onHUDEvent(HUDEvent(
                id: UUID(),
                timestamp: Date(),
                verdict: .matched,
                capturedBuffer: match.trigger,
                matchedTrigger: match.trigger
            ))
            context.setSuppressMatching(true)
            onMatch(match, triggerLength)
        case .undo(let trigger):
            Logger.shared.debug("Undo: restoring trigger \(trigger)")
            context.setSuppressMatching(true)
            onUndo(trigger)
        case .secureInputChanged(let isSecure):
            onSecureInputChanged(isSecure)
        case .toggled(let enabled):
            onToggled(enabled)
        case .hudRequested:
            onHUDRequested()
        case .blockedTooSoon(let snapshot):
            onHUDEvent(HUDEvent(
                id: UUID(),
                timestamp: Date(),
                verdict: .tooSoon,
                capturedBuffer: snapshot,
                matchedTrigger: nil
            ))
        case .blockedDisabled(let snapshot):
            onHUDEvent(HUDEvent(
                id: UUID(),
                timestamp: Date(),
                verdict: .disabled,
                capturedBuffer: snapshot,
                matchedTrigger: nil
            ))
        }

        return Unmanaged.passRetained(event)
    }
}
