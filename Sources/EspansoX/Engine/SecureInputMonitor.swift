import Foundation
import Carbon

struct SecureInputMonitor: Sendable {
    var wasSecure: Bool = false

    var isSecureInputActive: Bool {
        IsSecureEventInputEnabled()
    }

    mutating func check() -> SecureInputTransition {
        let isSecure = isSecureInputActive
        defer { wasSecure = isSecure }

        if isSecure && !wasSecure {
            return .enteredSecure
        } else if !isSecure && wasSecure {
            return .exitedSecure
        } else if isSecure {
            return .stillSecure
        }
        return .notSecure
    }
}

enum SecureInputTransition: Sendable {
    case notSecure
    case enteredSecure
    case exitedSecure
    case stillSecure
}
