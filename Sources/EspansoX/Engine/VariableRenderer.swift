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
