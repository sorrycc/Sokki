import Foundation

enum TriggerConflictKind: Int, Sendable, Equatable, Hashable, Comparable {
    case duplicate = 0
    case shadowedBy = 1
    case shadows = 2

    static func < (lhs: Self, rhs: Self) -> Bool { lhs.rawValue < rhs.rawValue }
}

struct TriggerConflict: Sendable, Equatable, Hashable {
    let kind: TriggerConflictKind
    let otherTrigger: String
}

struct ConflictReport: Sendable, Equatable {
    let conflicts: [TriggerConflict]

    var isEmpty: Bool { conflicts.isEmpty }
    var hasDuplicate: Bool { conflicts.contains { $0.kind == .duplicate } }

    static let empty = ConflictReport(conflicts: [])
}

enum TriggerConflictDetector {
    static func computeList(matches: [Match]) -> [UUID: ConflictReport] {
        let enabled = matches.filter(\.isEnabled)
        var out: [UUID: [TriggerConflict]] = [:]

        var byTrigger: [String: [Match]] = [:]
        for m in enabled { byTrigger[m.trigger, default: []].append(m) }
        for (trigger, group) in byTrigger where group.count > 1 {
            for m in group {
                out[m.id, default: []]
                    .append(TriggerConflict(kind: .duplicate, otherTrigger: trigger))
            }
        }

        for m in enabled {
            guard !m.trigger.isEmpty else { continue }
            for other in enabled where other.id != m.id {
                if other.trigger.count > m.trigger.count,
                   other.trigger.hasPrefix(m.trigger) {
                    out[m.id, default: []]
                        .append(TriggerConflict(kind: .shadowedBy, otherTrigger: other.trigger))
                }
            }
        }

        return out.mapValues { conflicts in
            ConflictReport(conflicts: conflicts.sorted(by: comparator))
        }
    }

    static func computeEditor(
        draftTrigger: String,
        matches: [Match],
        excludingId: UUID?
    ) -> ConflictReport {
        guard !draftTrigger.isEmpty, draftTrigger != ":" else { return .empty }

        let enabled = matches.filter { $0.isEnabled && $0.id != excludingId }
        var seen = Set<TriggerConflict>()
        var collected: [TriggerConflict] = []

        for other in enabled {
            let conflict: TriggerConflict?
            if other.trigger == draftTrigger {
                conflict = TriggerConflict(kind: .duplicate, otherTrigger: draftTrigger)
            } else if other.trigger.count > draftTrigger.count,
                      other.trigger.hasPrefix(draftTrigger) {
                conflict = TriggerConflict(kind: .shadowedBy, otherTrigger: other.trigger)
            } else if draftTrigger.count > other.trigger.count,
                      draftTrigger.hasPrefix(other.trigger) {
                conflict = TriggerConflict(kind: .shadows, otherTrigger: other.trigger)
            } else {
                conflict = nil
            }

            if let c = conflict, seen.insert(c).inserted {
                collected.append(c)
            }
        }

        return ConflictReport(conflicts: collected.sorted(by: comparator))
    }

    private static func comparator(_ a: TriggerConflict, _ b: TriggerConflict) -> Bool {
        if a.kind != b.kind { return a.kind < b.kind }
        return a.otherTrigger < b.otherTrigger
    }
}
