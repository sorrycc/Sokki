import Foundation

struct AppConfig: Codable, Sendable, Equatable {
    var isEnabled: Bool = true
    var clipboardThreshold: Int = 50
    var injectDelay: Int = 0
    var pasteShortcut: String = "Cmd+V"
    var preserveClipboard: Bool = true
    var restoreClipboardDelay: Int = 100
    var logLevel: String = "error"
    var toggleShortcut: String = "none"
    var hudShortcut: String = "none"
    var paletteShortcut: String = "none"

    var ai: AISettings = .init()

    enum CodingKeys: String, CodingKey {
        case isEnabled = "enable"
        case clipboardThreshold = "clipboard_threshold"
        case injectDelay = "inject_delay"
        case pasteShortcut = "paste_shortcut"
        case preserveClipboard = "preserve_clipboard"
        case restoreClipboardDelay = "restore_clipboard_delay"
        case logLevel = "log_level"
        case toggleShortcut = "toggle_shortcut"
        case hudShortcut = "hud_shortcut"
        case paletteShortcut = "palette_shortcut"
        case ai
    }

    static let legacyAIKeys: [String] = [
        "ai_binary_path",
        "ai_model",
        "ai_timeout_seconds",
        "ai_selection_capture_timeout_ms",
    ]

    init() {}

    init(from decoder: Decoder) throws {
        // Reject legacy flat `ai_*` keys before lenient decoding.
        let raw = try decoder.container(keyedBy: AnyStringKey.self)
        for legacy in AppConfig.legacyAIKeys {
            if raw.contains(AnyStringKey(stringValue: legacy)) {
                throw ConfigLoadIssue.legacyKey(
                    key: legacy,
                    hint: "move under the `ai:` table (e.g. `ai.binary`, `ai.model`)"
                )
            }
        }

        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.isEnabled = (try? container.decode(Bool.self, forKey: .isEnabled)) ?? true
        self.clipboardThreshold = (try? container.decode(Int.self, forKey: .clipboardThreshold)) ?? 50
        self.injectDelay = (try? container.decode(Int.self, forKey: .injectDelay)) ?? 0
        self.pasteShortcut = (try? container.decode(String.self, forKey: .pasteShortcut)) ?? "Cmd+V"
        self.preserveClipboard = (try? container.decode(Bool.self, forKey: .preserveClipboard)) ?? true
        self.restoreClipboardDelay = (try? container.decode(Int.self, forKey: .restoreClipboardDelay)) ?? 100
        self.logLevel = (try? container.decode(String.self, forKey: .logLevel)) ?? "error"
        self.toggleShortcut = (try? container.decode(String.self, forKey: .toggleShortcut)) ?? "none"
        self.hudShortcut = (try? container.decode(String.self, forKey: .hudShortcut)) ?? "none"
        self.paletteShortcut = (try? container.decode(String.self, forKey: .paletteShortcut)) ?? "none"
        // `ai` decoding propagates `ConfigLoadIssue.invalidAIArg` (e.g. from
        // AISettings.validateArgs) rather than silently defaulting — we want
        // bad args to surface, not vanish.
        self.ai = (try container.decodeIfPresent(AISettings.self, forKey: .ai)) ?? AISettings()
    }
}
