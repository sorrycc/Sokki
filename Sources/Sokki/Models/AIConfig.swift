import Foundation

enum AIInput: String, Codable, Sendable, Equatable, Hashable {
    case selection
    case clipboard
    case none
}

struct AIConfig: Codable, Sendable, Equatable, Hashable {
    var prompt: String
    var input: AIInput

    enum CodingKeys: String, CodingKey {
        case prompt, input
    }

    init(prompt: String, input: AIInput = .selection) {
        self.prompt = prompt
        self.input = input
    }

    init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        self.prompt = try container.decode(String.self, forKey: .prompt)
        self.input = (try? container.decode(AIInput.self, forKey: .input)) ?? .selection
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(prompt, forKey: .prompt)
        try container.encode(input, forKey: .input)
    }
}
