import Foundation

/// Sidecar store for "last-used" timestamps per match trigger.
/// Persisted to ~/.config/sokki/recency.json. Writes are debounced 1s.
/// Keyed by trigger string (not Match.id) because Match.id is regenerated
/// on every YAML reload.
final class RecencyStore: @unchecked Sendable {
    private let url: URL
    private let queue = DispatchQueue(label: "sokki.recency")
    private var map: [String: Date] = [:]
    private var pendingSave: DispatchWorkItem?

    init(configDir: URL) {
        self.url = configDir.appendingPathComponent("recency.json")
        loadFromDisk()
    }

    func lastUsed(for trigger: String) -> Date? {
        queue.sync { map[trigger] }
    }

    func snapshot() -> [String: Date] {
        queue.sync { map }
    }

    func bump(_ trigger: String, now: Date = Date()) {
        queue.async {
            self.map[trigger] = now
            self.scheduleSave()
        }
    }

    private func scheduleSave() {
        pendingSave?.cancel()
        let work = DispatchWorkItem { [weak self] in
            self?.saveToDisk()
        }
        pendingSave = work
        queue.asyncAfter(deadline: .now() + 1.0, execute: work)
    }

    private func loadFromDisk() {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        guard let data = try? Data(contentsOf: url),
              let raw = try? decoder.decode([String: Date].self, from: data) else {
            return
        }
        map = raw
    }

    private func saveToDisk() {
        let snapshot = map
        do {
            let encoder = JSONEncoder()
            encoder.dateEncodingStrategy = .iso8601
            encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
            let data = try encoder.encode(snapshot)
            try FileManager.default.createDirectory(
                at: url.deletingLastPathComponent(),
                withIntermediateDirectories: true
            )
            try data.write(to: url, options: .atomic)
        } catch {
            Logger.shared.error("Failed to save recency.json: \(error.localizedDescription)")
        }
    }
}
