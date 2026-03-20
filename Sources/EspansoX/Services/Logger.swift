import Foundation

final class Logger: Sendable {
    static let shared = Logger()

    enum Level: Int, Sendable, Comparable {
        case error = 0
        case warning = 1
        case debug = 2

        static func < (lhs: Level, rhs: Level) -> Bool {
            lhs.rawValue < rhs.rawValue
        }
    }

    private let logDir: URL
    private let logFile: URL
    private let maxSize: Int = 1_048_576 // 1MB
    private let lock = NSLock()

    nonisolated(unsafe) private static var _level: Level = .error

    static var level: Level {
        get { _level }
        set { _level = newValue }
    }

    private init() {
        let configDir = FileManager.default.homeDirectoryForCurrentUser
            .appendingPathComponent(".config/espansox/logs")
        self.logDir = configDir
        self.logFile = configDir.appendingPathComponent("espansox.log")
        try? FileManager.default.createDirectory(at: configDir, withIntermediateDirectories: true)
    }

    func error(_ message: String) {
        log(level: .error, message: message)
    }

    func warning(_ message: String) {
        log(level: .warning, message: message)
    }

    func debug(_ message: String) {
        guard Self.level >= .debug else { return }
        log(level: .debug, message: message)
    }

    private func log(level: Level, message: String) {
        let timestamp = ISO8601DateFormatter().string(from: Date())
        let label = switch level {
        case .error: "ERROR"
        case .warning: "WARN"
        case .debug: "DEBUG"
        }
        let line = "[\(timestamp)] [\(label)] \(message)\n"

        lock.lock()
        defer { lock.unlock() }

        rotateIfNeeded()

        if let data = line.data(using: .utf8) {
            if FileManager.default.fileExists(atPath: logFile.path) {
                if let handle = try? FileHandle(forWritingTo: logFile) {
                    handle.seekToEndOfFile()
                    handle.write(data)
                    handle.closeFile()
                }
            } else {
                try? data.write(to: logFile)
            }
        }
    }

    private func rotateIfNeeded() {
        guard let attrs = try? FileManager.default.attributesOfItem(atPath: logFile.path),
              let size = attrs[.size] as? Int,
              size >= maxSize else { return }

        let backup = logDir.appendingPathComponent("espansox.log.1")
        try? FileManager.default.removeItem(at: backup)
        try? FileManager.default.moveItem(at: logFile, to: backup)
    }
}
