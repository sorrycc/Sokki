import AppKit
import Carbon.HIToolbox
import Foundation

@MainActor
final class TextInjector {
    private let clipboardManager = ClipboardManager()
    var config: AppConfig = AppConfig()

    func inject(replacement: String, triggerLength: Int) {
        // Erase the trigger by simulating backspace
        eraseCharacters(count: triggerLength)

        if replacement.count <= config.clipboardThreshold {
            injectViaKeys(replacement)
        } else {
            injectViaClipboard(replacement)
        }
    }

    private func eraseCharacters(count: Int) {
        let backspaceKeyCode: CGKeyCode = 0x33
        for _ in 0..<count {
            postKeyEvent(keyCode: backspaceKeyCode, keyDown: true)
            postKeyEvent(keyCode: backspaceKeyCode, keyDown: false)
            if config.injectDelay > 0 {
                usleep(UInt32(config.injectDelay) * 1000)
            }
        }
        // Small delay after backspace to let the app process deletions
        usleep(10_000) // 10ms
    }

    private func injectViaKeys(_ text: String) {
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

            if config.injectDelay > 0 {
                usleep(UInt32(config.injectDelay) * 1000)
            }
        }
    }

    private func injectViaClipboard(_ text: String) {
        if config.preserveClipboard {
            clipboardManager.save()
        }

        clipboardManager.setString(text)

        // Simulate Cmd+V
        let source = CGEventSource(stateID: .hidSystemState)
        let vKeyCode: CGKeyCode = 0x09 // 'v' key
        if let keyDown = CGEvent(keyboardEventSource: source, virtualKey: vKeyCode, keyDown: true),
           let keyUp = CGEvent(keyboardEventSource: source, virtualKey: vKeyCode, keyDown: false) {
            keyDown.flags = .maskCommand
            keyUp.flags = .maskCommand
            keyDown.post(tap: .cghidEventTap)
            keyUp.post(tap: .cghidEventTap)
        }

        if config.preserveClipboard {
            let delay = config.restoreClipboardDelay
            DispatchQueue.main.asyncAfter(deadline: .now() + .milliseconds(delay)) { [clipboardManager] in
                clipboardManager.restore()
            }
        }
    }

    private func postKeyEvent(keyCode: CGKeyCode, keyDown: Bool) {
        let source = CGEventSource(stateID: .hidSystemState)
        guard let event = CGEvent(keyboardEventSource: source, virtualKey: keyCode, keyDown: keyDown) else {
            return
        }
        event.post(tap: .cghidEventTap)
    }
}
