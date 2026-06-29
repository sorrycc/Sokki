import Foundation

final class TrieNode: Sendable {
    let children: [Character: TrieNode]
    let match: Match?
    let hasDescendantMatches: Bool

    init(children: [Character: TrieNode] = [:], match: Match? = nil, hasDescendantMatches: Bool = false) {
        self.children = children
        self.match = match
        self.hasDescendantMatches = hasDescendantMatches
    }
}

struct TrieCursor: Sendable {
    let node: TrieNode
    let depth: Int
}

struct TrieMatchResult: Sendable {
    let match: Match
    let triggerLength: Int
}

struct TrieMatcher: Sendable {
    let root: TrieNode
    private(set) var cursors: [TrieCursor]
    private(set) var pendingMatch: TrieMatchResult?

    init(matches: [Match]) {
        self.root = TrieMatcher.buildTrie(from: matches)
        self.cursors = []
        self.pendingMatch = nil
    }

    private static func buildTrie(from matches: [Match]) -> TrieNode {
        let enabledMatches = matches.filter(\.isEnabled)
        return buildNode(from: enabledMatches, at: 0)
    }

    private static func buildNode(from matches: [Match], at depth: Int) -> TrieNode {
        var childrenMap: [Character: [Match]] = [:]
        var nodeMatch: Match?

        for match in matches {
            let chars = Array(match.trigger)
            if depth == chars.count {
                nodeMatch = match
            } else if depth < chars.count {
                let char = chars[depth]
                childrenMap[char, default: []].append(match)
            }
        }

        var children: [Character: TrieNode] = [:]
        var hasDescendants = false
        for (char, childMatches) in childrenMap {
            let childNode = buildNode(from: childMatches, at: depth + 1)
            children[char] = childNode
            hasDescendants = true
        }

        return TrieNode(
            children: children,
            match: nodeMatch,
            hasDescendantMatches: hasDescendants || children.values.contains { $0.match != nil || $0.hasDescendantMatches }
        )
    }

    mutating func feed(_ char: Character) -> TrieMatchResult? {
        // Advance existing cursors
        var newCursors: [TrieCursor] = []
        var longestMatch: TrieMatchResult?
        var hasActiveCursorsWithPotential = false

        for cursor in cursors {
            if let next = cursor.node.children[char] {
                let newDepth = cursor.depth + 1
                let newCursor = TrieCursor(node: next, depth: newDepth)
                newCursors.append(newCursor)

                if let match = next.match {
                    let result = TrieMatchResult(match: match, triggerLength: newDepth)
                    if longestMatch == nil || newDepth > longestMatch!.triggerLength {
                        longestMatch = result
                    }
                }

                if next.hasDescendantMatches {
                    hasActiveCursorsWithPotential = true
                }
            }
        }

        // Start new cursor at root
        if let next = root.children[char] {
            let newCursor = TrieCursor(node: next, depth: 1)
            newCursors.append(newCursor)

            if let match = next.match {
                let result = TrieMatchResult(match: match, triggerLength: 1)
                if longestMatch == nil || 1 > longestMatch!.triggerLength {
                    longestMatch = result
                }
            }

            if next.hasDescendantMatches {
                hasActiveCursorsWithPotential = true
            }
        }

        cursors = newCursors

        // Drop pending if a cursor has advanced past it. The user has typed
        // further than the pending's trigger boundary, committing to a
        // longer-prefix path; the shorter pending proposal is no longer
        // their intent. Runs BEFORE the match-found branch so a chained
        // replacement (e.g. ;f → ;ff) still installs the new pending cleanly.
        if let pending = pendingMatch,
           cursors.contains(where: { $0.depth > pending.triggerLength }) {
            pendingMatch = nil
        }

        // Handle match found
        if let match = longestMatch {
            if hasActiveCursorsWithPotential {
                // Longer match might be possible — hold as pending
                pendingMatch = match
                return nil
            } else {
                // No longer matches possible — fire immediately
                pendingMatch = nil
                cursors = []
                return match
            }
        }

        // No match found this keystroke
        if !hasActiveCursorsWithPotential, let pending = pendingMatch {
            // No cursors can advance further — fire pending match
            pendingMatch = nil
            cursors = []
            return pending
        }

        return nil
    }

    /// Compact dump of post-feed matcher state for trace logging: the active
    /// cursor depths (how far each in-flight prefix has advanced) and the
    /// trigger of any match being held pending the conflict-resolve delay.
    var debugSummary: String {
        let depths = cursors.map(\.depth).sorted()
        return "cursors=\(depths) pending=\(pendingMatch?.match.trigger ?? "-")"
    }

    mutating func reset() {
        cursors = []
        pendingMatch = nil
    }

    mutating func flushPending() -> TrieMatchResult? {
        guard let pending = pendingMatch else { return nil }
        pendingMatch = nil
        cursors = []
        return pending
    }
}
