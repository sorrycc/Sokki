import AppKit
import Foundation

@MainActor
final class VariableRenderer {
    private static let variablePattern = try! NSRegularExpression(
        pattern: #"\{\{(\w+)(?::([^}]*))?\}\}"#
    )

    static let cursorGlyph = "▎"

    /// Live render for actual injection: `{{clipboard}}` reads the pasteboard,
    /// `{{date}}` uses now, unknown variables are left verbatim.
    func render(_ template: String) -> String {
        substitute(template) { name, params in
            switch name {
            case "clipboard":
                return NSPasteboard.general.string(forType: .string) ?? ""
            case "date":
                return Self.formatDate(params)
            default:
                return Self.verbatim(name: name, params: params)
            }
        }
    }

    /// Render for the palette preview pane: `{{clipboard}}` uses a stable
    /// snapshot (so it doesn't change as the user navigates results) and
    /// `{{cursor}}` renders as a visible, tinted glyph.
    func renderPreview(_ template: String, clipboardSnapshot: String?) -> AttributedString {
        let plain = substitute(template) { name, params in
            switch name {
            case "clipboard":
                return clipboardSnapshot ?? ""
            case "cursor":
                return Self.cursorGlyph
            case "date":
                return Self.formatDate(params)
            default:
                return Self.verbatim(name: name, params: params)
            }
        }

        // Tint every cursor glyph. Safe because the glyph is the only place we
        // ever insert this Unicode block, and user content is very unlikely to
        // contain it.
        var result = AttributedString(plain)
        var cursor = result.startIndex
        while cursor < result.endIndex,
              let range = result[cursor..<result.endIndex].range(of: Self.cursorGlyph) {
            result[range].foregroundColor = .accentColor
            cursor = range.upperBound
        }
        return result
    }

    /// Walk every `{{name[:params]}}` occurrence in reverse (to keep indices
    /// stable) and replace it with `resolve(name, params)`.
    private func substitute(_ template: String, resolve: (String, String?) -> String) -> String {
        let range = NSRange(location: 0, length: (template as NSString).length)
        let matches = Self.variablePattern.matches(in: template, range: range)
        guard !matches.isEmpty else { return template }

        var result = template
        for match in matches.reversed() {
            let fullRange = Range(match.range, in: result)!
            let name = String(result[Range(match.range(at: 1), in: result)!])

            var params: String?
            if match.range(at: 2).location != NSNotFound,
               let paramsRange = Range(match.range(at: 2), in: result) {
                params = String(result[paramsRange])
            }

            result.replaceSubrange(fullRange, with: resolve(name, params))
        }
        return result
    }

    private static func formatDate(_ params: String?) -> String {
        let formatter = DateFormatter()
        formatter.dateFormat = params ?? "yyyy-MM-dd"
        return formatter.string(from: Date())
    }

    /// Unknown variable — leave the original token untouched.
    private static func verbatim(name: String, params: String?) -> String {
        if let params { return "{{\(name):\(params)}}" }
        return "{{\(name)}}"
    }
}
