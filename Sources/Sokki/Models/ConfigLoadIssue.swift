import Foundation

enum ConfigLoadIssue: Error, LocalizedError {
    case legacyKey(key: String, hint: String)
    case invalidMatch(trigger: String, reason: String)
    case invalidAIArg(arg: String, reason: String)

    var errorDescription: String? {
        switch self {
        case .legacyKey(let key, let hint):
            return "Unsupported key `\(key)` — \(hint)"
        case .invalidMatch(let trigger, let reason):
            return "Invalid match `\(trigger)`: \(reason)"
        case .invalidAIArg(let arg, let reason):
            return "Invalid `ai.args` entry `\(arg)`: \(reason)"
        }
    }
}

struct AnyStringKey: CodingKey {
    var stringValue: String
    var intValue: Int? { nil }
    init(stringValue: String) { self.stringValue = stringValue }
    init?(intValue: Int) { nil }
}
