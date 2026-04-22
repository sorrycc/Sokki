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

    /// Erase `count` characters via simulated backspaces.
    func eraseInjected(count: Int) {
        eraseCharacters(count: count)
    }

    /// Inject a string via unicode keyboard events without touching the trigger.
    /// Used by the AI flow for placeholder + delta injection. Batches into
    /// ≤200 UTF-16 code unit chunks so `keyboardSetUnicodeString` doesn't
    /// overflow, and sleeps briefly between chunks so throttling Electron
    /// apps don't drop events.
    func injectDelta(_ text: String) {
        guard !text.isEmpty else { return }
        let source = CGEventSource(stateID: .hidSystemState)
        let utf16 = Array(text.utf16)
        let chunkSize = 200
        var index = 0
        while index < utf16.count {
            let end = min(index + chunkSize, utf16.count)
            let slice = Array(utf16[index..<end])
            if let event = CGEvent(keyboardEventSource: source, virtualKey: 0, keyDown: true) {
                event.keyboardSetUnicodeString(stringLength: slice.count, unicodeString: slice)
                event.post(tap: .cghidEventTap)
            }
            // IMPORTANT: keyUp must NOT carry the unicode string. Some apps
            // (Electron / rich-input fields) process keyUp text too and insert
            // a second copy. Match the existing `injectViaKeys` pattern: bare
            // release with no string attached.
            if let upEvent = CGEvent(keyboardEventSource: source, virtualKey: 0, keyDown: false) {
                upEvent.post(tap: .cghidEventTap)
            }
            index = end
            if index < utf16.count {
                let gap = config.injectDelay > 0 ? UInt32(config.injectDelay) * 1000 : 1500
                usleep(gap)
            }
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
