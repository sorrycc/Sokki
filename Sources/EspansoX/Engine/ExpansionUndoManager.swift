import Foundation

struct ExpansionRecord: Sendable {
    let trigger: String
    let replacement: String
    let timestamp: Date
}

struct ExpansionUndoManager: Sendable {
    private var lastExpansion: ExpansionRecord?
    private var backspaceCount: Int = 0
    private let undoTimeout: TimeInterval = 3.0

    mutating func recordExpansion(trigger: String, replacement: String) {
        lastExpansion = ExpansionRecord(
            trigger: trigger,
            replacement: replacement,
            timestamp: Date()
        )
        backspaceCount = 0
    }

    mutating func handleBackspace() -> UndoAction {
        guard let record = lastExpansion else { return .none }

        // Check if undo window has expired
        if Date().timeIntervalSince(record.timestamp) > undoTimeout {
            lastExpansion = nil
            backspaceCount = 0
            return .none
        }

        backspaceCount += 1

        if backspaceCount >= record.replacement.count {
            let trigger = record.trigger
            lastExpansion = nil
            backspaceCount = 0
            return .undo(restoreTrigger: trigger)
        }

        return .none
    }

    mutating func handleNonBackspaceKey() {
        lastExpansion = nil
        backspaceCount = 0
    }

    mutating func clear() {
        lastExpansion = nil
        backspaceCount = 0
    }
}

enum UndoAction: Sendable {
    case none
    case undo(restoreTrigger: String)
}
