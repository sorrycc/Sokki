import Foundation
import AppKit

@MainActor
final class Bootstrapper {
    private let configDir: URL

    init() {
        self.configDir = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/sokki")
    }

    func run() {
        guard !FileManager.default.fileExists(atPath: configDir.path) else { return }

        do {
            try createDirectoryStructure()
            try writeDefaultConfig()
            try writeExampleMatches()
            showWelcomeAlert()
        } catch {
            Logger.shared.error("Bootstrap failed: \(error.localizedDescription)")
        }
    }

    private func createDirectoryStructure() throws {
        let fm = FileManager.default
        try fm.createDirectory(at: configDir.appendingPathComponent("config"),
                              withIntermediateDirectories: true)
        try fm.createDirectory(at: configDir.appendingPathComponent("match"),
                              withIntermediateDirectories: true)
        try fm.createDirectory(at: configDir.appendingPathComponent("logs"),
                              withIntermediateDirectories: true)
    }

    private func writeDefaultConfig() throws {
        let content = """
        # Sokki — Global Configuration
        # See docs for all available options.

        enable: true
        clipboard_threshold: 50          # chars before switching to clipboard mode
        inject_delay: 0                  # ms between key simulations
        paste_shortcut: "Cmd+V"
        preserve_clipboard: true
        restore_clipboard_delay: 100     # ms
        log_level: "debug"               # "debug" (default) or "error"
        toggle_shortcut: "none"          # "none" (default), "double_option", "double_shift", or "double_control"
        hud_shortcut: "none"             # "none" (default), "double_option", "double_shift", or "double_control"
        """
        let url = configDir.appendingPathComponent("config/default.yml")
        try content.write(to: url, atomically: true, encoding: .utf8)
    }

    private func writeExampleMatches() throws {
        let content = """
        # Sokki — Example matches
        # Edit this file or add more .yml files in this directory.
        # Changes are auto-detected and reloaded.

        matches:
          # Simple text replacement
          - trigger: ":hello"
            replace: "Hi there!"

          # Insert current date
          - trigger: ":today"
            replace: "{{date:%Y-%m-%d}}"

          # Insert clipboard content with wrapper
          - trigger: ":paste"
            replace: "Pasted: {{clipboard}}"

          # Multi-line replacement
          - trigger: ":sig"
            replace: |
              Best regards,
              Your Name
        """
        let url = configDir.appendingPathComponent("match/default.yml")
        try content.write(to: url, atomically: true, encoding: .utf8)
    }

    private func showWelcomeAlert() {
        let alert = NSAlert()
        alert.messageText = "Welcome to Sokki"
        alert.informativeText = "Config files created at ~/.config/sokki/.\nEdit match/default.yml to add your triggers."
        alert.alertStyle = .informational
        alert.addButton(withTitle: "Open Config Folder")
        alert.addButton(withTitle: "OK")

        let response = alert.runModal()
        if response == .alertFirstButtonReturn {
            NSWorkspace.shared.open(configDir)
        }
    }
}
