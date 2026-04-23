import Foundation

struct Match: Identifiable, Codable, Sendable, Equatable, Hashable {
    let id: UUID
    var trigger: String
    var replace: String
    var prompt: String?
    var label: String?
    var isEnabled: Bool
    var sourceFile: String?

    enum CodingKeys: String, CodingKey {
        case trigger, replace, prompt, label
        case isEnabled = "enabled"
    }

    var isAI: Bool { prompt != nil }

    init(
        id: UUID = UUID(),
        trigger: String,
        replace: String = "",
        prompt: String? = nil,
        label: String? = nil,
        isEnabled: Bool = true,
        sourceFile: String? = nil
    ) {
        self.id = id
        self.trigger = trigger
        self.replace = replace
        self.prompt = prompt
        self.label = label
        self.isEnabled = isEnabled
        self.sourceFile = sourceFile
    }

    init(from decoder: Decoder) throws {
        // Reject the legacy `ai:` block before lenient decoding.
        let raw = try decoder.container(keyedBy: AnyStringKey.self)
        if raw.contains(AnyStringKey(stringValue: "ai")) {
            throw ConfigLoadIssue.legacyKey(
                key: "ai",
                hint: "replace with top-level `prompt:` containing {{selection}} / {{clipboard}} placeholders"
            )
        }

        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.id = UUID()
        self.trigger = try container.decode(String.self, forKey: .trigger)
        self.prompt = try container.decodeIfPresent(String.self, forKey: .prompt)
        self.replace = (try? container.decode(String.self, forKey: .replace)) ?? ""
        self.label = try container.decodeIfPresent(String.self, forKey: .label)
        self.isEnabled = (try? container.decode(Bool.self, forKey: .isEnabled)) ?? true
        self.sourceFile = nil

        if let prompt = self.prompt {
            if prompt.isEmpty {
                throw ConfigLoadIssue.invalidMatch(
                    trigger: self.trigger,
                    reason: "`prompt:` is empty"
                )
            }
            if !self.replace.isEmpty {
                throw ConfigLoadIssue.invalidMatch(
                    trigger: self.trigger,
                    reason: "`prompt:` and `replace:` cannot both be set"
                )
            }
            _ = try PromptTemplate.parse(prompt)
        }
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(trigger, forKey: .trigger)
        if let prompt {
            try container.encode(prompt, forKey: .prompt)
        } else {
            try container.encode(replace, forKey: .replace)
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
