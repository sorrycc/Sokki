import Foundation
import Yams

struct ConfigLoadResult: Sendable {
    let matches: [Match]
    let config: AppConfig
    let error: String?
    let warnings: [String]
}

final class ConfigLoader: Sendable {
    private let configDir: URL

    init() {
        self.configDir = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/espansox")
    }

    var configDirURL: URL { configDir }
    var matchDir: URL { configDir.appendingPathComponent("match") }

    func loadAll() -> ConfigLoadResult {
        let config = loadConfig()
        let (matches, error, warnings) = loadMatches()
        return ConfigLoadResult(matches: matches, config: config, error: error, warnings: warnings)
    }

    private func loadConfig() -> AppConfig {
        let configFile = configDir.appendingPathComponent("config/default.yml")
        guard let data = try? Data(contentsOf: configFile),
              let yaml = String(data: data, encoding: .utf8) else {
            return AppConfig()
        }

        let decoder = YAMLDecoder()
        do {
            return try decoder.decode(AppConfig.self, from: yaml)
        } catch {
            Logger.shared.error("Failed to parse config/default.yml: \(error.localizedDescription)")
            return AppConfig()
        }
    }

    private func loadMatches() -> ([Match], String?, [String]) {
        var allMatches: [Match] = []
        var firstError: String?
        var warnings: [String] = []
        var seenTriggers: [String: String] = [:] // trigger -> filename

        let matchDir = self.matchDir
        guard let files = try? FileManager.default.contentsOfDirectory(
            at: matchDir,
            includingPropertiesForKeys: nil
        ) else {
            return ([], nil, [])
        }

        let ymlFiles = files
            .filter { $0.pathExtension == "yml" || $0.pathExtension == "yaml" }
            .sorted { $0.lastPathComponent < $1.lastPathComponent }

        for file in ymlFiles {
            guard let data = try? Data(contentsOf: file),
                  let yaml = String(data: data, encoding: .utf8) else { continue }

            let decoder = YAMLDecoder()
            do {
                let matchFile = try decoder.decode(MatchFile.self, from: yaml)
                for match in matchFile.matches {
                    let filename = file.lastPathComponent
                    if let existing = seenTriggers[match.trigger] {
                        warnings.append(
                            "Duplicate trigger \"\(match.trigger)\" in \(filename) (overrides \(existing))"
                        )
                        allMatches.removeAll { $0.trigger == match.trigger }
                    }
                    seenTriggers[match.trigger] = filename
                    allMatches.append(match)
                }
            } catch {
                let msg = "Failed to parse \(file.lastPathComponent): \(error.localizedDescription)"
                Logger.shared.error(msg)
                if firstError == nil { firstError = msg }
            }
        }

        return (allMatches, firstError, warnings)
    }

    func saveMatches(_ matches: [Match]) {
        let matchFile = MatchFile(matches: matches)
        let encoder = YAMLEncoder()
        do {
            let yaml = try encoder.encode(matchFile)
            let fileURL = matchDir.appendingPathComponent("default.yml")
            try yaml.write(to: fileURL, atomically: true, encoding: .utf8)
        } catch {
            Logger.shared.error("Failed to save matches: \(error.localizedDescription)")
        }
    }
}
