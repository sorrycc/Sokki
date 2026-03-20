import Foundation
import AppKit

@Observable
@MainActor
final class MatchStore {
    var matches: [Match] = []
    var config: AppConfig = AppConfig()
    var lastError: String?

    private let configLoader = ConfigLoader()

    func loadAll() {
        let result = configLoader.loadAll()
        self.matches = result.matches
        self.config = result.config
        self.lastError = result.error

        if let error = result.error {
            Logger.shared.error("Config load error: \(error)")
        }
        if !result.warnings.isEmpty {
            for warning in result.warnings {
                Logger.shared.warning(warning)
            }
        }

        NotificationCenter.default.post(name: .matchesDidChange, object: nil)
    }

    func add(_ match: Match) {
        matches.append(match)
        saveMatches()
    }

    func update(_ match: Match) {
        guard let index = matches.firstIndex(where: { $0.id == match.id }) else { return }
        matches[index] = match
        saveMatches()
    }

    func remove(at offsets: IndexSet) {
        matches.remove(atOffsets: offsets)
        saveMatches()
    }

    func remove(_ match: Match) {
        matches.removeAll { $0.id == match.id }
        saveMatches()
    }

    private func saveMatches() {
        configLoader.saveMatches(matches)
        NotificationCenter.default.post(name: .matchesDidChange, object: nil)
    }
}

extension Notification.Name {
    static let matchesDidChange = Notification.Name("EspansoX.matchesDidChange")
    static let openSettings = Notification.Name("EspansoX.openSettings")
}
