import Foundation

enum PromptSource: String, Sendable, Equatable {
    case selection
    case clipboard
}

struct PromptPlaceholder: Sendable, Equatable {
    let chain: [PromptSource]
    /// Character offset into `PromptTemplate.raw` where `{{` begins.
    let location: Int
    /// Character length of the `{{...}}` token, inclusive of braces.
    let length: Int
    /// Original text between `{{` and `}}` (for HUD messages).
    let innerRaw: String
}

enum PromptParseError: Error, LocalizedError, Equatable {
    case unclosedBrace(at: Int)
    case emptyPlaceholder(at: Int)
    case emptySourceInChain(at: Int)
    case unknownSource(String, at: Int)

    var errorDescription: String? {
        switch self {
        case .unclosedBrace(let i):       return "Unclosed `{{` at position \(i)"
        case .emptyPlaceholder(let i):    return "Empty `{{}}` at position \(i)"
        case .emptySourceInChain(let i):  return "Empty source in chain at position \(i)"
        case .unknownSource(let s, let i):
            return "Unknown source `\(s)` at position \(i) (expected selection/clipboard/s/c)"
        }
    }
}

struct PromptTemplate: Sendable, Equatable {
    let raw: String
    let placeholders: [PromptPlaceholder]

    var needsSelection: Bool { placeholders.contains { $0.chain.contains(.selection) } }
    var needsClipboard: Bool { placeholders.contains { $0.chain.contains(.clipboard) } }

    /// True iff some chain is exactly `[.selection]` — i.e. no fallback source
    /// can substitute. Distinguishes `{{s}}` (mandatory) from `{{s|c}}`
    /// (preferred but fallback-capable).
    var requiresSelection: Bool { placeholders.contains { $0.chain == [.selection] } }

    static func parse(_ s: String) throws -> PromptTemplate {
        let chars = Array(s)
        var placeholders: [PromptPlaceholder] = []
        var i = 0
        while i < chars.count {
            if i + 1 < chars.count, chars[i] == "{", chars[i + 1] == "{" {
                let open = i
                var j = i + 2
                var close: Int? = nil
                while j + 1 < chars.count {
                    if chars[j] == "}" && chars[j + 1] == "}" {
                        close = j
                        break
                    }
                    j += 1
                }
                guard let close else {
                    throw PromptParseError.unclosedBrace(at: open)
                }
                let inner = String(chars[(open + 2)..<close])
                let chain = try parseChain(inner, at: open)
                placeholders.append(PromptPlaceholder(
                    chain: chain,
                    location: open,
                    length: close + 2 - open,
                    innerRaw: inner
                ))
                i = close + 2
            } else {
                i += 1
            }
        }
        return PromptTemplate(raw: s, placeholders: placeholders)
    }

    private static func parseChain(_ inner: String, at location: Int) throws -> [PromptSource] {
        if inner.trimmingCharacters(in: .whitespaces).isEmpty {
            throw PromptParseError.emptyPlaceholder(at: location)
        }
        let parts = inner.split(separator: "|", omittingEmptySubsequences: false)
        var chain: [PromptSource] = []
        for part in parts {
            let name = part.trimmingCharacters(in: .whitespaces)
            if name.isEmpty {
                throw PromptParseError.emptySourceInChain(at: location)
            }
            switch name {
            case "selection", "s": chain.append(.selection)
            case "clipboard", "c": chain.append(.clipboard)
            default: throw PromptParseError.unknownSource(name, at: location)
            }
        }
        return chain
    }

    /// Single-pass, non-recursive substitution. Returns nil when any
    /// placeholder's chain resolves entirely to empty.
    func render(selection: String?, clipboard: String?) -> String? {
        if placeholders.isEmpty { return raw }
        let chars = Array(raw)
        var out = ""
        var cursor = 0
        for ph in placeholders {
            if cursor < ph.location {
                out.append(String(chars[cursor..<ph.location]))
            }
            var resolved: String? = nil
            for src in ph.chain {
                let v: String? = src == .selection ? selection : clipboard
                if let v, !v.isEmpty { resolved = v; break }
            }
            guard let resolved else { return nil }
            out.append(resolved)
            cursor = ph.location + ph.length
        }
        if cursor < chars.count {
            out.append(String(chars[cursor..<chars.count]))
        }
        return out
    }

    /// Echo of the first placeholder whose chain is all-empty, formatted
    /// as `{{<inner>}}` for HUD notes. nil when everything resolved.
    func firstUnresolvedChain(selection: String?, clipboard: String?) -> String? {
        for ph in placeholders {
            var anyFilled = false
            for src in ph.chain {
                let v: String? = src == .selection ? selection : clipboard
                if let v, !v.isEmpty { anyFilled = true; break }
            }
            if !anyFilled { return "{{\(ph.innerRaw)}}" }
        }
        return nil
    }
}
