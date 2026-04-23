import Foundation
import AppKit

@Observable
@MainActor
final class MatchStore {
    var matches: [Match] = [] {
        didSet {
            conflictsByMatchId = TriggerConflictDetector.computeList(matches: matches)
        }
    }
    var config: AppConfig = AppConfig()
    var lastError: String?
    private(set) var conflictsByMatchId: [UUID: ConflictReport] = [:]

    private var knownFiles: Set<String> = []
    private let configLoader = ConfigLoader()

    var availableSourceFiles: [String] {
        var set = knownFiles
        for m in matches { if let f = m.sourceFile { set.insert(f) } }
        set.insert("default.yml")
        return set.sorted()
    }

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
    static let openMatchEditor = Notification.Name("Sokki.openMatchEditor")
    static let checkForUpdates = Notification.Name("Sokki.checkForUpdates")
}
