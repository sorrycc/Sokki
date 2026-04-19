import Foundation
import Yams

struct ConfigLoadResult: Sendable {
    let matches: [Match]
    let config: AppConfig
    let error: String?
    let warnings: [String]
    let loadedFiles: Set<String>
}

private let defaultMatchFilename = "default.yml"

final class ConfigLoader: Sendable {
    private let configDir: URL

    init() {
        self.configDir = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/sokki")
    }

    var configDirURL: URL { configDir }
    var matchDir: URL { configDir.appendingPathComponent("match") }

    func loadAll() -> ConfigLoadResult {
        let config = loadConfig()
        let (matches, error, warnings, loadedFiles) = loadMatches()
        return ConfigLoadResult(
            matches: matches,
            config: config,
            error: error,
            warnings: warnings,
            loadedFiles: loadedFiles
        )
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

    private func loadMatches() -> ([Match], String?, [String], Set<String>) {
        var allMatches: [Match] = []
        var firstError: String?
        var warnings: [String] = []
        var seenTriggers: [String: String] = [:] // trigger -> filename
        var loadedFiles: Set<String> = []

        let matchDir = self.matchDir
        guard let files = try? FileManager.default.contentsOfDirectory(
            at: matchDir,
            includingPropertiesForKeys: nil
        ) else {
            return ([], nil, [], [])
        }

        let ymlFiles = files
            .filter { $0.pathExtension == "yml" || $0.pathExtension == "yaml" }
            .sorted { $0.lastPathComponent < $1.lastPathComponent }

        for file in ymlFiles {
            let filename = file.lastPathComponent
            loadedFiles.insert(filename)

            guard let data = try? Data(contentsOf: file),
                  let yaml = String(data: data, encoding: .utf8) else { continue }

            let decoder = YAMLDecoder()
            do {
                let matchFile = try decoder.decode(MatchFile.self, from: yaml)
                for var match in matchFile.matches {
                    match.sourceFile = filename
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
                let msg = "Failed to parse \(filename): \(error.localizedDescription)"
                Logger.shared.error(msg)
                if firstError == nil { firstError = msg }
            }
        }

        return (allMatches, firstError, warnings, loadedFiles)
    }

    func saveMatches(_ matches: [Match], knownFiles: Set<String>) {
        var grouped: [String: [Match]] = [:]
        for match in matches {
            let filename = match.sourceFile ?? defaultMatchFilename
            grouped[filename, default: []].append(match)
        }

        let filesToWrite = knownFiles.union(grouped.keys).union([defaultMatchFilename])

        let encoder = YAMLEncoder()
        encoder.options.allowUnicode = true
        encoder.options.newLineScalarStyle = .literal

        for filename in filesToWrite {
            let fileMatches = grouped[filename] ?? []
            let matchFile = MatchFile(matches: fileMatches)
            do {
                let yaml = try encoder.encode(matchFile)
                let fileURL = matchDir.appendingPathComponent(filename)
                try yaml.write(to: fileURL, atomically: true, encoding: .utf8)
            } catch {
                Logger.shared.error("Failed to save \(filename): \(error.localizedDescription)")
            }
        }
    }

    func saveConfig(_ config: AppConfig) {
        let encoder = YAMLEncoder()
        encoder.options.allowUnicode = true
        do {
            let yaml = try encoder.encode(config)
            let fileURL = configDir.appendingPathComponent("config/default.yml")
            try yaml.write(to: fileURL, atomically: true, encoding: .utf8)
        } catch {
            Logger.shared.error("Failed to save config: \(error.localizedDescription)")
        }
    }
}
