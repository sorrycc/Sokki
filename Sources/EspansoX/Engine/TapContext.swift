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
        var toggleShortcut: String = "double_option"
        var lastModifierTapTime: Date?
        var lastModifierKey: CGEventFlags?
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
    }

    func feed(character: Character, keyCode: UInt16, modifierFlags: CGEventFlags) -> FeedResult {
        lock.withLock { state in
            // Check suppression
            if state.suppressMatching {
                return FeedResult(action: .none)
            }

            // Check paste suppression (Cmd+V)
            if let until = state.pasteSuppressionUntil, Date() < until {
                return FeedResult(action: .none)
            }
            state.pasteSuppressionUntil = nil

            // Check secure input
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

            // Check if disabled
            guard state.isEnabled else {
                return FeedResult(action: .none)
            }

            // Handle backspace
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

            // Non-backspace key clears undo tracking
            state.undoManager.handleNonBackspaceKey()

            // Append to buffer
            state.buffer.append(character)

            // Feed to trie matcher
            if let result = state.matcher.feed(character) {
                return FeedResult(action: .matched(
                    match: result.match,
                    triggerLength: result.triggerLength
                ))
            }

            return FeedResult(action: .none)
        }
    }

    func handlePasteDetected() {
        lock.withLock { state in
            state.pasteSuppressionUntil = Date().addingTimeInterval(0.2)
        }
    }

    func handleModifierTap(flags: CGEventFlags) -> FeedResult {
        lock.withLock { state in
            guard state.toggleShortcut != "none" else {
                return FeedResult(action: .none)
            }

            let targetFlag: CGEventFlags
            switch state.toggleShortcut {
            case "double_option":
                targetFlag = .maskAlternate
            case "double_shift":
                targetFlag = .maskShift
            case "double_control":
                targetFlag = .maskControl
            default:
                return FeedResult(action: .none)
            }

            // Check if the target modifier is the only one pressed
            let modifierMask: CGEventFlags = [.maskShift, .maskControl, .maskAlternate, .maskCommand]
            let activeModifiers = flags.intersection(modifierMask)
            guard activeModifiers == targetFlag else {
                state.lastModifierTapTime = nil
                return FeedResult(action: .none)
            }

            let now = Date()
            if let lastTime = state.lastModifierTapTime,
               state.lastModifierKey == targetFlag,
               now.timeIntervalSince(lastTime) < 0.3 {
                // Double tap detected
                state.lastModifierTapTime = nil
                state.lastModifierKey = nil
                state.isEnabled.toggle()
                state.buffer.clear()
                state.matcher.reset()
                return FeedResult(action: .toggled(isEnabled: state.isEnabled))
            }

            state.lastModifierTapTime = now
            state.lastModifierKey = targetFlag
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
            state.isEnabled = config.isEnabled
            state.toggleShortcut = config.toggleShortcut
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
