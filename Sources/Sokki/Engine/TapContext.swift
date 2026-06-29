import AppKit
import Foundation
import os

final class TapContext: @unchecked Sendable {
    private let lock = OSAllocatedUnfairLock(initialState: State())
    private let timerQueue = DispatchQueue(label: "sokki.matcher.timer", qos: .userInitiated)

    // `DispatchWorkItem` is not Sendable in Swift 6, but State is only ever
    // mutated inside `lock.withLock`, which provides mutual exclusion.
    struct State: @unchecked Sendable {
        var buffer = RollingBuffer()
        var matcher = TrieMatcher(matches: [])
        var secureInput = SecureInputMonitor()
        var undoManager = ExpansionUndoManager()
        var suppressMatching = false
        var pasteSuppressionUntil: Date?
        var isEnabled = true
        var toggleShortcut: String = "none"
        var hudShortcut: String = "none"
        var lastModifierTapTime: Date?
        var lastModifierKey: CGEventFlags?
        var hasEmittedTooSoonForCurrentPaste = false
        var hasEmittedDisabledSinceLastEnable = false
        var paletteChord: PaletteChord?
        var activeAIRequest = false
        var userTypedDuringAI = false
        var conflictResolveDelayMs: Int = 400
        var pendingTimer: DispatchWorkItem?
        var pendingTimerToken: UInt64 = 0
        var onPendingMatchFired: (@Sendable (Match, Int) -> Void)?
    }

    struct PaletteChord: Sendable, Equatable {
        let keyCode: UInt16
        let modifiers: CGEventFlags

        static func parse(_ string: String) -> PaletteChord? {
            let spaceKeyCode: UInt16 = 0x31 // kVK_Space
            switch string {
            case "opt+space":
                return PaletteChord(keyCode: spaceKeyCode, modifiers: .maskAlternate)
            case "ctrl+space":
                return PaletteChord(keyCode: spaceKeyCode, modifiers: .maskControl)
            case "ctrl+opt+space":
                return PaletteChord(keyCode: spaceKeyCode, modifiers: [.maskControl, .maskAlternate])
            case "ctrl+shift+space":
                return PaletteChord(keyCode: spaceKeyCode, modifiers: [.maskControl, .maskShift])
            default:
                return nil
            }
        }
    }

    struct FeedResult: Sendable {
        let action: FeedAction
    }

    enum FeedAction: Sendable {
        case none
        case matched(match: Match, triggerLength: Int)
        case undo(restoreTrigger: String)
        case secureInputChanged(isSecure: Bool)
        case toggled(isEnabled: Bool)
        case hudRequested
        case blockedTooSoon(bufferSnapshot: String)
        case blockedDisabled(bufferSnapshot: String)
    }

    func feed(character: Character, keyCode: UInt16, modifierFlags: CGEventFlags) -> FeedResult {
        lock.withLock { state in
            // Self-echo guard for our own injection — never surfaces to HUD.
            if state.suppressMatching {
                return FeedResult(action: .none)
            }

            // Secure input FIRST — a secure frame hard-drops everything below,
            // guaranteeing no HUD event ever carries secure-field keystrokes.
            let secureTransition = state.secureInput.check()
            switch secureTransition {
            case .enteredSecure:
                // No character logged — this is a secure (password) field.
                Logger.shared.trace("secure: entered — buffer cleared")
                state.buffer.clear()
                resetMatcherAndTimer(&state)
                return FeedResult(action: .secureInputChanged(isSecure: true))
            case .exitedSecure:
                Logger.shared.trace("secure: exited — buffer cleared")
                state.buffer.clear()
                resetMatcherAndTimer(&state)
                return FeedResult(action: .secureInputChanged(isSecure: false))
            case .stillSecure:
                return FeedResult(action: .none)
            case .notSecure:
                break
            }

            // Paste-too-soon window, guaranteed non-secure here.
            if let until = state.pasteSuppressionUntil, Date() < until {
                if !state.hasEmittedTooSoonForCurrentPaste {
                    state.hasEmittedTooSoonForCurrentPaste = true
                    let snapshot = state.buffer.toString() + String(character)
                    Logger.shared.trace("blocked: paste-too-soon buf=\"\(snapshot)\"")
                    return FeedResult(action: .blockedTooSoon(bufferSnapshot: snapshot))
                }
                return FeedResult(action: .none)
            }
            state.pasteSuppressionUntil = nil

            // Disabled, guaranteed non-secure and not-paste-blocked.
            guard state.isEnabled else {
                if !state.hasEmittedDisabledSinceLastEnable {
                    state.hasEmittedDisabledSinceLastEnable = true
                    let snapshot = state.buffer.toString() + String(character)
                    Logger.shared.trace("blocked: disabled buf=\"\(snapshot)\"")
                    return FeedResult(action: .blockedDisabled(bufferSnapshot: snapshot))
                }
                return FeedResult(action: .none)
            }

            // Handle backspace.
            let backspaceKeyCode: UInt16 = 0x33
            if keyCode == backspaceKeyCode {
                if modifierFlags.contains(.maskAlternate) || modifierFlags.contains(.maskCommand) {
                    invalidateMatchingState(&state)
                    Logger.shared.trace("clear: word/line-delete flags=\(modifierFlags.rawValue)")
                    return FeedResult(action: .none)
                }
                let undoResult = state.undoManager.handleBackspace()
                switch undoResult {
                case .undo(let trigger):
                    state.buffer.clear()
                    resetMatcherAndTimer(&state)
                    return FeedResult(action: .undo(restoreTrigger: trigger))
                case .none:
                    rebuildAfterBackspace(&state)
                    Logger.shared.trace("rewind: popped, remaining=\(state.buffer.currentCount) \(state.matcher.debugSummary)")
                    return FeedResult(action: .none)
                }
            }

            // Caret-move keys invalidate buffer (caret moved away from our
            // tracked position, so subsequent typing is no longer contiguous).
            if isCaretControlKey(keyCode) {
                invalidateMatchingState(&state)
                Logger.shared.trace("clear: caret-move keyCode=\(keyCode)")
                return FeedResult(action: .none)
            }

            // Non-backspace key clears undo tracking.
            state.undoManager.handleNonBackspaceKey()

            // Append to buffer.
            state.buffer.append(character)

            // Per-keystroke decision trace. Capture the tail BEFORE any clear,
            // and the matcher summary AFTER feed, so a held-pending shows up as
            // `-> none` with a non-`-` pending (the ;don vs ;done case). Logging
            // the character is safe here: secure/suppress/paste/disabled all
            // returned above, so this line never carries a password keystroke.
            let tail = state.buffer.tail(min(16, state.buffer.currentCount))

            // Feed to trie matcher. Clear buffer on fire so a later rewind
            // can't resurrect consumed trigger chars into a pending match.
            let mResult = state.matcher.feed(character)
            let summary = state.matcher.debugSummary
            if let result = mResult {
                Logger.shared.trace("feed '\(character)' buf=\"\(tail)\" \(summary) -> matched \(result.match.trigger) len=\(result.triggerLength)")
                state.buffer.clear()
                reschedulePendingTimer(&state)
                return FeedResult(action: .matched(
                    match: result.match,
                    triggerLength: result.triggerLength
                ))
            }

            // Pure typo / still-typing: intentionally not a HUD event (AC10).
            Logger.shared.trace("feed '\(character)' buf=\"\(tail)\" \(summary) -> none")
            reschedulePendingTimer(&state)
            return FeedResult(action: .none)
        }
    }

    func handlePasteDetected() {
        lock.withLock { state in
            // Only re-arm the debounce flag when opening a fresh window; a second
            // Cmd+V inside the still-active window must not emit a second "Too soon"
            // row for the same paste.
            if state.pasteSuppressionUntil == nil || Date() >= state.pasteSuppressionUntil! {
                state.hasEmittedTooSoonForCurrentPaste = false
            }
            state.pasteSuppressionUntil = Date().addingTimeInterval(0.2)
        }
    }

    func handleModifierTap(flags: CGEventFlags) -> FeedResult {
        lock.withLock { state in
            let modifierMask: CGEventFlags = [.maskShift, .maskControl, .maskAlternate, .maskCommand]
            let activeModifiers = flags.intersection(modifierMask)

            func targetFlag(for shortcut: String) -> CGEventFlags? {
                switch shortcut {
                case "double_option":  return .maskAlternate
                case "double_shift":   return .maskShift
                case "double_control": return .maskControl
                default:               return nil
                }
            }

            let hudFlag = targetFlag(for: state.hudShortcut)
            let toggleFlag = targetFlag(for: state.toggleShortcut)

            // HUD wins on collision — new feature must not silently do nothing when
            // the user misconfigures both chords to the same value.
            let resolved: (kind: String, flag: CGEventFlags)?
            if let h = hudFlag, activeModifiers == h {
                resolved = ("hud", h)
            } else if let t = toggleFlag, activeModifiers == t {
                resolved = ("toggle", t)
            } else {
                state.lastModifierTapTime = nil
                return FeedResult(action: .none)
            }

            let r = resolved!
            let now = Date()
            if let lastTime = state.lastModifierTapTime,
               state.lastModifierKey == r.flag,
               now.timeIntervalSince(lastTime) < 0.3 {
                state.lastModifierTapTime = nil
                state.lastModifierKey = nil
                switch r.kind {
                case "hud":
                    return FeedResult(action: .hudRequested)
                case "toggle":
                    state.isEnabled.toggle()
                    notePendingDrop(&state, reason: "toggle-shortcut")
                    state.buffer.clear()
                    resetMatcherAndTimer(&state)
                    if state.isEnabled {
                        state.hasEmittedDisabledSinceLastEnable = false
                    }
                    return FeedResult(action: .toggled(isEnabled: state.isEnabled))
                default:
                    return FeedResult(action: .none)
                }
            }

            state.lastModifierTapTime = now
            state.lastModifierKey = r.flag
            return FeedResult(action: .none)
        }
    }

    func setSuppressMatching(_ suppress: Bool) {
        lock.withLock { state in
            state.suppressMatching = suppress
            // Guard on `pendingMatch != nil` so suppress toggles inside the AI
            // animation loop (~120 ms cadence at KeystrokeMonitor.swift:402,410)
            // remain a cheap no-op when nothing is pending.
            if suppress, state.matcher.pendingMatch != nil {
                notePendingDrop(&state, reason: "suppress")
                resetMatcherAndTimer(&state)
            }
        }
    }

    func clearBuffer() {
        lock.withLock { state in
            notePendingDrop(&state, reason: "focus-change")
            state.buffer.clear()
            resetMatcherAndTimer(&state)
        }
    }

    func updateMatches(_ matches: [Match]) {
        lock.withLock { state in
            // Cancel timer + drop any pending Match referencing the old trie.
            // Deliberately preserve `onPendingMatchFired` (per-monitor lifetime)
            // and `suppressMatching` (mid-injection self-echo gate).
            notePendingDrop(&state, reason: "matches-reload")
            state.pendingTimer?.cancel()
            state.pendingTimer = nil
            state.pendingTimerToken &+= 1
            state.matcher = TrieMatcher(matches: matches)
            state.buffer.clear()
            let enabled = matches.filter(\.isEnabled)
            let triggers = enabled.map(\.trigger).joined(separator: ", ")
            Logger.shared.trace("updateMatches: \(enabled.count) enabled; triggers=[\(triggers)]")
        }
    }

    func updateConfig(_ config: AppConfig) {
        lock.withLock { state in
            let wasDisabled = !state.isEnabled
            state.isEnabled = config.isEnabled
            state.toggleShortcut = config.toggleShortcut
            state.hudShortcut = config.hudShortcut
            state.paletteChord = PaletteChord.parse(config.paletteShortcut)
            state.conflictResolveDelayMs = config.conflictResolveDelayMs
            if wasDisabled && state.isEnabled {
                state.hasEmittedDisabledSinceLastEnable = false
            }
        }
    }

    func handlePaletteChord(keyCode: UInt16, flags: CGEventFlags) -> Bool {
        lock.withLock { state in
            guard let chord = state.paletteChord else { return false }
            let modifierMask: CGEventFlags = [.maskShift, .maskControl, .maskAlternate, .maskCommand]
            let activeModifiers = flags.intersection(modifierMask)
            let matched = keyCode == chord.keyCode && activeModifiers == chord.modifiers
            if matched {
                notePendingDrop(&state, reason: "palette-chord")
                resetMatcherAndTimer(&state)
            }
            return matched
        }
    }

    /// Drop pending match + cancel timer. Idempotent no-op when nothing is
    /// pending. Called from mouse-click / app-switch monitors and from the
    /// modifier-shortcut early-return path. `reason` is recorded in the trace
    /// log only when a pending match was actually dropped.
    func cancelPendingMatch(reason: String = "unspecified") {
        lock.withLock { state in
            notePendingDrop(&state, reason: reason)
            resetMatcherAndTimer(&state)
        }
    }

    func setPendingMatchFiredCallback(_ cb: @escaping @Sendable (Match, Int) -> Void) {
        lock.withLock { state in
            state.onPendingMatchFired = cb
        }
    }

    func recordExpansion(trigger: String, replacement: String) {
        lock.withLock { state in
            state.undoManager.recordExpansion(trigger: trigger, replacement: replacement)
        }
    }

    var isEnabled: Bool {
        lock.withLock { state in
            state.isEnabled
        }
    }

    // MARK: - AI request state

    func setActiveAIRequest(_ active: Bool) {
        lock.withLock { state in
            state.activeAIRequest = active
            if !active { state.userTypedDuringAI = false }
        }
    }

    func noteUserTypedDuringAI() {
        lock.withLock { state in
            // Ignore self-echo: while we're actively injecting (suppressMatching
            // is true), incoming synthesized keystrokes are our own.
            if state.activeAIRequest && !state.suppressMatching {
                state.userTypedDuringAI = true
            }
        }
    }

    /// Non-destructive read of `userTypedDuringAI`. Used by the AI placeholder
    /// animation loop to self-cancel on real user input without consuming the
    /// flag that the post-await completion branch also reads.
    func peekUserTypedDuringAI() -> Bool {
        lock.withLock { state in state.userTypedDuringAI }
    }

    /// Atomically read `userTypedDuringAI` AND clear both AI flags. Callers
    /// on the completion/failure path must use this instead of a separate
    /// read + `setActiveAIRequest(false)` — the latter clears the flag
    /// before it can be observed.
    func consumeUserTypedDuringAI() -> Bool {
        lock.withLock { state in
            let typed = state.userTypedDuringAI
            state.activeAIRequest = false
            state.userTypedDuringAI = false
            return typed
        }
    }

    var isAIActive: Bool {
        lock.withLock { state in state.activeAIRequest }
    }

    // MARK: - Matching-state helpers (called inside lock.withLock)

    private func isCaretControlKey(_ keyCode: UInt16) -> Bool {
        switch keyCode {
        case 0x7B, 0x7C, 0x7D, 0x7E: return true // ← → ↓ ↑
        case 0x73, 0x77, 0x74, 0x79: return true // Home, End, PageUp, PageDown
        default: return false
        }
    }

    /// Trace a pending-match drop, but only when one actually existed — keeps
    /// frequent callers (mouse-click, app-switch) from spamming the log. Pure
    /// read of `pendingMatch`; takes no lock, so it MUST be called from inside
    /// `lock.withLock`, and before `resetMatcherAndTimer` clears the pending.
    /// `inout` mirrors the sibling helpers and avoids copying `State`.
    private func notePendingDrop(_ state: inout State, reason: String) {
        if let pending = state.matcher.pendingMatch {
            Logger.shared.trace("pending dropped (\(reason)): \(pending.match.trigger)")
        }
    }

    private func invalidateMatchingState(_ state: inout State) {
        state.undoManager.handleNonBackspaceKey()
        state.buffer.clear()
        resetMatcherAndTimer(&state)
    }

    private func rebuildAfterBackspace(_ state: inout State) {
        guard state.buffer.popLast() else { return }
        resetMatcherAndTimer(&state)
        let remaining = state.buffer.toString()
        for ch in remaining {
            _ = state.matcher.feed(ch)
        }
        reschedulePendingTimer(&state)
    }

    // MARK: - Pending-match timer (conflict-resolve delay)

    private func resetMatcherAndTimer(_ state: inout State) {
        state.matcher.reset()
        state.pendingTimer?.cancel()
        state.pendingTimer = nil
        state.pendingTimerToken &+= 1
    }

    private func reschedulePendingTimer(_ state: inout State) {
        state.pendingTimer?.cancel()
        state.pendingTimerToken &+= 1
        guard state.matcher.pendingMatch != nil else {
            state.pendingTimer = nil
            return
        }
        let token = state.pendingTimerToken
        let delayMs = max(50, min(5000, state.conflictResolveDelayMs))
        let work = DispatchWorkItem { [weak self] in
            self?.firePendingTimer(token: token)
        }
        state.pendingTimer = work
        timerQueue.asyncAfter(deadline: .now() + .milliseconds(delayMs), execute: work)
    }

    private func firePendingTimer(token: UInt64) {
        let result: (Match, Int, @Sendable (Match, Int) -> Void)? = lock.withLock { state in
            guard token == state.pendingTimerToken,
                  let pending = state.matcher.flushPending(),
                  let cb = state.onPendingMatchFired
            else { return nil }
            state.pendingTimer = nil
            state.buffer.clear()
            return (pending.match, pending.triggerLength, cb)
        }
        if let (match, length, cb) = result {
            Logger.shared.trace("pending fired (timeout): \(match.trigger) len=\(length)")
            cb(match, length)
        }
    }
}
