import Foundation
import AppKit

@Observable
@MainActor
final class MatchStore {
    var matches: [Match] = []
    var config: AppConfig = AppConfig()
    var lastError: String?

    private var knownFiles: Set<String> = []
    private let configLoader = ConfigLoader()

    func loadAll() {
        let result = configLoader.loadAll()
        self.matches = result.matches
        self.config = result.config
        self.lastError = result.error
        self.knownFiles = result.loadedFiles

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
        var newMatch = match
        if newMatch.sourceFile == nil { newMatch.sourceFile = "default.yml" }
        matches.append(newMatch)
        saveMatches()
    }

    func update(_ match: Match) {
        guard let index = matches.firstIndex(where: { $0.id == match.id }) else { return }
        var updated = match
        updated.sourceFile = matches[index].sourceFile
        matches[index] = updated
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

    func saveConfig() {
        configLoader.saveConfig(config)
    }

    private func saveMatches() {
        configLoader.saveMatches(matches, knownFiles: knownFiles)
        knownFiles.formUnion(matches.compactMap(\.sourceFile))
        NotificationCenter.default.post(name: .matchesDidChange, object: nil)
    }
}

extension Notification.Name {
    static let matchesDidChange = Notification.Name("Sokki.matchesDidChange")
    static let openSettings = Notification.Name("Sokki.openSettings")
    static let checkForUpdates = Notification.Name("Sokki.checkForUpdates")
}
