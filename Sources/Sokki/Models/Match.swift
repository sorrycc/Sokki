import Foundation

struct Match: Identifiable, Codable, Sendable, Equatable, Hashable {
    let id: UUID
    var trigger: String
    var replace: String
    var isEnabled: Bool

    enum CodingKeys: String, CodingKey {
        case id, trigger, replace
        case isEnabled = "enabled"
    }

    init(id: UUID = UUID(), trigger: String, replace: String, isEnabled: Bool = true) {
        self.id = id
        self.trigger = trigger
        self.replace = replace
        self.isEnabled = isEnabled
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.id = (try? container.decode(UUID.self, forKey: .id)) ?? UUID()
        self.trigger = try container.decode(String.self, forKey: .trigger)
        self.replace = try container.decode(String.self, forKey: .replace)
        self.isEnabled = (try? container.decode(Bool.self, forKey: .isEnabled)) ?? true
    }
}

struct MatchFile: Codable, Sendable {
    let matches: [Match]
}
