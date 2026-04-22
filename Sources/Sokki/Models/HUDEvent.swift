import Foundation

struct HUDEvent: Identifiable, Sendable, Equatable {
    enum Verdict: String, Sendable, Equatable {
        case matched
        case tooSoon
        case disabled
        case aiThinking
        case aiCompleted
        case aiFailed
    }

    let id: UUID
    let timestamp: Date
    let verdict: Verdict
    let capturedBuffer: String
    let matchedTrigger: String?
    let note: String?

    init(id: UUID = UUID(), timestamp: Date = Date(), verdict: Verdict, capturedBuffer: String, matchedTrigger: String?, note: String? = nil) {
        self.id = id
        self.timestamp = timestamp
        self.verdict = verdict
        self.capturedBuffer = capturedBuffer
        self.matchedTrigger = matchedTrigger
        self.note = note
    }
}
