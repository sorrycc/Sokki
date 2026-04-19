import Foundation

struct Match: Identifiable, Codable, Sendable, Equatable, Hashable {
    let id: UUID
    var trigger: String
    var replace: String
    var isEnabled: Bool
    var sourceFile: String?

    enum CodingKeys: String, CodingKey {
        case trigger, replace
        case isEnabled = "enabled"
    }

    init(id: UUID = UUID(), trigger: String, replace: String, isEnabled: Bool = true, sourceFile: String? = nil) {
        self.id = id
        self.trigger = trigger
        self.replace = replace
        self.isEnabled = isEnabled
        self.sourceFile = sourceFile
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.id = UUID()
        self.trigger = try container.decode(String.self, forKey: .trigger)
        self.replace = try container.decode(String.self, forKey: .replace)
        self.isEnabled = (try? container.decode(Bool.self, forKey: .isEnabled)) ?? true
        self.sourceFile = nil
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(trigger, forKey: .trigger)
        try container.encode(replace, forKey: .replace)
        if !isEnabled {
            try container.encode(isEnabled, forKey: .isEnabled)
        }
    }
}

struct MatchFile: Codable, Sendable {
    let matches: [Match]
}
