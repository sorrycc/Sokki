import Foundation

struct Match: Identifiable, Codable, Sendable, Equatable, Hashable {
    let id: UUID
    var trigger: String
    var replace: String
    var label: String?
    var isEnabled: Bool
    var ai: AIConfig?
    var sourceFile: String?

    enum CodingKeys: String, CodingKey {
        case trigger, replace, label, ai
        case isEnabled = "enabled"
    }

    init(id: UUID = UUID(), trigger: String, replace: String = "", label: String? = nil, isEnabled: Bool = true, ai: AIConfig? = nil, sourceFile: String? = nil) {
        self.id = id
        self.trigger = trigger
        self.replace = replace
        self.label = label
        self.isEnabled = isEnabled
        self.ai = ai
        self.sourceFile = sourceFile
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.id = UUID()
        self.trigger = try container.decode(String.self, forKey: .trigger)
        self.ai = try container.decodeIfPresent(AIConfig.self, forKey: .ai)
        // When `ai` is present, `replace` is optional (ignored at expansion time).
        self.replace = (try? container.decode(String.self, forKey: .replace)) ?? ""
        self.label = try container.decodeIfPresent(String.self, forKey: .label)
        self.isEnabled = (try? container.decode(Bool.self, forKey: .isEnabled)) ?? true
        self.sourceFile = nil
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(trigger, forKey: .trigger)
        if ai == nil {
            try container.encode(replace, forKey: .replace)
        } else if !replace.isEmpty {
            try container.encode(replace, forKey: .replace)
        }
        if let ai {
            try container.encode(ai, forKey: .ai)
        }
        if let label, !label.isEmpty {
            try container.encode(label, forKey: .label)
        }
        if !isEnabled {
            try container.encode(isEnabled, forKey: .isEnabled)
        }
    }
}

struct MatchFile: Codable, Sendable {
    let matches: [Match]
}
