import Foundation

@MainActor
final class ClaudeBinaryResolver {
    private var cached: URL?
    private let fileManager = FileManager.default

    /// Resolve at startup (or re-resolve if cached path has gone missing).
    /// Returns nil when no `claude` binary can be found.
    func resolve(explicitPath: String?) -> URL? {
        if let cached, fileManager.isExecutableFile(atPath: cached.path) {
            return cached
        }

        let candidates = candidatePaths(explicitPath: explicitPath)
        for path in candidates {
            if fileManager.isExecutableFile(atPath: path) {
                let url = URL(fileURLWithPath: path)
                cached = url
                Logger.shared.error("[AI:resolve] found at \(path)")
                return url
            }
        }

        if let fromShell = resolveViaLoginShell() {
            cached = fromShell
            Logger.shared.error("[AI:resolve] found via login shell at \(fromShell.path)")
            return fromShell
        }

        Logger.shared.error("[AI:resolve] claude binary not found; checked=\(candidates.joined(separator: ","))")
        return nil
    }

    func invalidate() { cached = nil }

    private func candidatePaths(explicitPath: String?) -> [String] {
        var list: [String] = []
        if let explicitPath, !explicitPath.isEmpty {
            list.append(expandTilde(explicitPath))
        }
        let home = fileManager.homeDirectoryForCurrentUser.path
        list.append("\(home)/.claude/local/claude")
        list.append("\(home)/.local/bin/claude")
        list.append("/opt/homebrew/bin/claude")
        list.append("/usr/local/bin/claude")
        return list
    }

    private func expandTilde(_ p: String) -> String {
        if p.hasPrefix("~/") {
            return fileManager.homeDirectoryForCurrentUser
                .appendingPathComponent(String(p.dropFirst(2)))
                .path
        }
        return p
    }

    private func resolveViaLoginShell() -> URL? {
        let shell = ProcessInfo.processInfo.environment["SHELL"] ?? "/bin/zsh"
        let proc = Process()
        proc.executableURL = URL(fileURLWithPath: shell)
        proc.arguments = ["-lc", "command -v claude"]
        let out = Pipe()
        let err = Pipe()
        proc.standardOutput = out
        proc.standardError = err
        do {
            try proc.run()
        } catch {
            return nil
        }
        proc.waitUntilExit()
        guard proc.terminationStatus == 0 else { return nil }
        let data = out.fileHandleForReading.readDataToEndOfFile()
        let path = String(data: data, encoding: .utf8)?
            .trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        guard !path.isEmpty, fileManager.isExecutableFile(atPath: path) else {
            return nil
        }
        return URL(fileURLWithPath: path)
    }
}
