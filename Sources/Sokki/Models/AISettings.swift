import Foundation

enum PlaceholderStyle: String, Codable, Sendable, Equatable {
    case `static`   = "static"
    case braille    = "braille"
    case blockSweep = "block_sweep"
}

struct AISettings: Codable, Sendable, Equatable {
    var binary: String? = nil
    var args: [String]? = nil
    var model: String = "sonnet"
    var timeoutSeconds: Int = 30
    var selectionCaptureTimeoutMs: Int = 400
    var placeholderStyle: PlaceholderStyle = .static

    enum CodingKeys: String, CodingKey {
        case binary, args, model
        case timeoutSeconds = "timeout_seconds"
        case selectionCaptureTimeoutMs = "selection_capture_timeout_ms"
        case placeholderStyle = "placeholder_style"
    }

    init() {}

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.binary = try container.decodeIfPresent(String.self, forKey: .binary)
        self.args = try container.decodeIfPresent([String].self, forKey: .args)
        if let args = self.args {
            try AISettings.validateArgs(args)
        }
        self.model = (try? container.decode(String.self, forKey: .model)) ?? "sonnet"
        self.timeoutSeconds = (try? container.decode(Int.self, forKey: .timeoutSeconds)) ?? 30
        self.selectionCaptureTimeoutMs = (try? container.decode(Int.self, forKey: .selectionCaptureTimeoutMs)) ?? 400
        self.placeholderStyle = (try? container.decode(PlaceholderStyle.self, forKey: .placeholderStyle)) ?? .static
    }

    /// Reject `=`-form flags (`--foo=bar`). Structural, version-independent.
    /// Other malformed shapes (dangling flags, unknown flag names) are left to
    /// the Claude CLI to reject at runtime.
    static func validateArgs(_ args: [String]) throws {
        for a in args where a.hasPrefix("--") && a.contains("=") {
            throw ConfigLoadIssue.invalidAIArg(
                arg: a,
                reason: "`=` form not supported; use separated form, e.g. `--output-format`, `json`"
            )
        }
    }
}
