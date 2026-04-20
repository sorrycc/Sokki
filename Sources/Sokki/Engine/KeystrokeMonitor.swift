import AppKit
import Carbon.HIToolbox
import Foundation

@MainActor
final class KeystrokeMonitor {
    let tapContext = TapContext()

    private var tapThread: Thread?
    private var isRunning = false

    let variableRenderer = VariableRenderer()
    let textInjector = TextInjector()

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
                let rendered = self.variableRenderer.render(match.replace)
                self.textInjector.inject(replacement: rendered, triggerLength: triggerLength)
                self.tapContext.recordExpansion(trigger: match.trigger, replacement: rendered)
                self.tapContext.setSuppressMatching(false)
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
                onPaletteToggleRequested: onPaletteToggle
            )
        }
        tapThread.name = "Sokki.EventTap"
        tapThread.qualityOfService = .userInteractive
        self.tapThread = tapThread
        tapThread.start()

        Logger.shared.debug("KeystrokeMonitor started")
    }

    func stop() {
        isRunning = false
        tapState.disable()
        tapThread = nil
        Logger.shared.debug("KeystrokeMonitor stopped")
    }

    func updateConfig(_ config: AppConfig) {
        tapContext.updateConfig(config)
        textInjector.config = config
    }

    func updateMatches(_ matches: [Match]) {
        tapContext.updateMatches(matches)
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
        onPaletteToggleRequested: @escaping @Sendable () -> Void
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
            onPaletteToggleRequested: onPaletteToggleRequested
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

    init(
        context: TapContext,
        onMatch: @escaping @Sendable (Match, Int) -> Void,
        onUndo: @escaping @Sendable (String) -> Void,
        onSecureInputChanged: @escaping @Sendable (Bool) -> Void,
        onToggled: @escaping @Sendable (Bool) -> Void,
        onHUDRequested: @escaping @Sendable () -> Void,
        onHUDEvent: @escaping @Sendable (HUDEvent) -> Void,
        onPaletteToggleRequested: @escaping @Sendable () -> Void
    ) {
        self.context = context
        self.onMatch = onMatch
        self.onUndo = onUndo
        self.onSecureInputChanged = onSecureInputChanged
        self.onToggled = onToggled
        self.onHUDRequested = onHUDRequested
        self.onHUDEvent = onHUDEvent
        self.onPaletteToggleRequested = onPaletteToggleRequested
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

        // Skip keys with Cmd/Ctrl held (except backspace)
        let backspaceKeyCode: UInt16 = 0x33
        if keyCode != backspaceKeyCode && (flags.contains(.maskCommand) || flags.contains(.maskControl)) {
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
