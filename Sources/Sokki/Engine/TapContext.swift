import AppKit
import Foundation
import os

final class TapContext: @unchecked Sendable {
    private let lock = OSAllocatedUnfairLock(initialState: State())

    struct State {
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
                state.buffer.clear()
                state.matcher.reset()
                return FeedResult(action: .secureInputChanged(isSecure: true))
            case .exitedSecure:
                state.buffer.clear()
                state.matcher.reset()
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
                    return FeedResult(action: .blockedDisabled(bufferSnapshot: snapshot))
                }
                return FeedResult(action: .none)
            }

            // Handle backspace.
            let backspaceKeyCode: UInt16 = 0x33
            if keyCode == backspaceKeyCode {
                let undoResult = state.undoManager.handleBackspace()
                switch undoResult {
                case .undo(let trigger):
                    state.buffer.clear()
                    state.matcher.reset()
                    return FeedResult(action: .undo(restoreTrigger: trigger))
                case .none:
                    break
                }
                return FeedResult(action: .none)
            }

            // Non-backspace key clears undo tracking.
            state.undoManager.handleNonBackspaceKey()

            // Append to buffer.
            state.buffer.append(character)

            // Feed to trie matcher.
            if let result = state.matcher.feed(character) {
                return FeedResult(action: .matched(
                    match: result.match,
                    triggerLength: result.triggerLength
                ))
            }

            // Pure typo / still-typing: intentionally not a HUD event (AC10).
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
                    state.buffer.clear()
                    state.matcher.reset()
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
        }
    }

    func clearBuffer() {
        lock.withLock { state in
            state.buffer.clear()
            state.matcher.reset()
        }
    }

    func updateMatches(_ matches: [Match]) {
        lock.withLock { state in
            state.matcher = TrieMatcher(matches: matches)
            state.buffer.clear()
        }
    }

    func updateConfig(_ config: AppConfig) {
        lock.withLock { state in
            let wasDisabled = !state.isEnabled
            state.isEnabled = config.isEnabled
            state.toggleShortcut = config.toggleShortcut
            state.hudShortcut = config.hudShortcut
            state.paletteChord = PaletteChord.parse(config.paletteShortcut)
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
            return keyCode == chord.keyCode && activeModifiers == chord.modifiers
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
}
