import Foundation

enum VariableType: Sendable {
    case date(format: String)
    case clipboard
}

struct ParsedVariable: Sendable {
    let name: String
    let params: String?
    let range: Range<String.Index>
}
