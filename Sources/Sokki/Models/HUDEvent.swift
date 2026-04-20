import Foundation

struct HUDEvent: Identifiable, Sendable, Equatable {
    enum Verdict: String, Sendable, Equatable {
        case matched
        case tooSoon
        case disabled
    }

    let id: UUID
    let timestamp: Date
    let verdict: Verdict
    let capturedBuffer: String
    let matchedTrigger: String?
}
