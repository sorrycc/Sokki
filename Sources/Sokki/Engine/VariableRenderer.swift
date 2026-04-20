import AppKit
import Foundation

@MainActor
final class VariableRenderer {
    private static let variablePattern = try! NSRegularExpression(
        pattern: #"\{\{(\w+)(?::([^}]*))?\}\}"#
    )

    func render(_ template: String) -> String {
        let nsTemplate = template as NSString
        let range = NSRange(location: 0, length: nsTemplate.length)
        let matches = Self.variablePattern.matches(in: template, range: range)

        guard !matches.isEmpty else { return template }

        var result = template
        // Process matches in reverse order to preserve indices
        for match in matches.reversed() {
            let fullRange = Range(match.range, in: result)!
            let nameRange = Range(match.range(at: 1), in: result)!
            let name = String(result[nameRange])

            var params: String?
            if match.range(at: 2).location != NSNotFound,
               let paramsRange = Range(match.range(at: 2), in: result) {
                params = String(result[paramsRange])
            }

            let value = resolveVariable(name: name, params: params)
            result.replaceSubrange(fullRange, with: value)
        }

        return result
    }

    /// Render with a stable clipboard snapshot and a visible cursor glyph.
    /// Used by the palette preview pane so `{{clipboard}}` doesn't change as
    /// the user navigates results, and `{{cursor}}` is visible in the preview.
    func renderPreview(_ template: String, clipboardSnapshot: String?) -> AttributedString {
        let range = NSRange(location: 0, length: (template as NSString).length)
        let matches = Self.variablePattern.matches(in: template, range: range)

        var plain = template
        for match in matches.reversed() {
            let fullRange = Range(match.range, in: plain)!
            let nameRange = Range(match.range(at: 1), in: plain)!
            let name = String(plain[nameRange])

            var params: String?
            if match.range(at: 2).location != NSNotFound,
               let paramsRange = Range(match.range(at: 2), in: plain) {
                params = String(plain[paramsRange])
            }

            let value: String
            switch name {
            case "clipboard":
                value = clipboardSnapshot ?? ""
            case "cursor":
                value = Self.cursorGlyph
            case "date":
                let format = params ?? "yyyy-MM-dd"
                let formatter = DateFormatter()
                formatter.dateFormat = format
                value = formatter.string(from: Date())
            default:
                value = params.map { "{{\(name):\($0)}}" } ?? "{{\(name)}}"
            }

            plain.replaceSubrange(fullRange, with: value)
        }

        // Tint every cursor glyph in the result. Safe because the glyph is the
        // only place we ever insert this Unicode block, and user content is
        // very unlikely to contain it.
        var result = AttributedString(plain)
        var cursor = result.startIndex
        while cursor < result.endIndex,
              let range = result[cursor..<result.endIndex].range(of: Self.cursorGlyph) {
            result[range].foregroundColor = .accentColor
            cursor = range.upperBound
        }
        return result
    }

    static let cursorGlyph = "▎"

    private func resolveVariable(name: String, params: String?) -> String {
        switch name {
        case "clipboard":
            return NSPasteboard.general.string(forType: .string) ?? ""
        case "date":
            let format = params ?? "yyyy-MM-dd"
            let formatter = DateFormatter()
            formatter.dateFormat = format
            return formatter.string(from: Date())
        default:
            // Unknown variable — leave as-is
            if let params {
                return "{{\(name):\(params)}}"
            }
            return "{{\(name)}}"
        }
    }
}
