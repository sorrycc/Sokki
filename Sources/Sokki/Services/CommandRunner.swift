import Foundation
import os

struct CommandResult: Sendable {
    let exitCode: Int32
    let stdoutData: Data
    let stderrTail: String
}

enum CommandRunnerError: Error, Sendable {
    case notFound
    case launchFailed(String)
    case cancelled
}

protocol CommandRunner: Sendable {
    /// Spawns `executable` with `args`. Optionally writes `stdin` and closes.
    /// Returns accumulated stdout bytes plus the tail of stderr.
    /// Callers can cancel the Swift task to terminate the underlying process.
    func run(
        executable: URL,
        args: [String],
        cwd: URL?,
        stdin: Data?
    ) async throws -> CommandResult
}

/// Real `Process`-backed implementation.
final class ProcessCommandRunner: CommandRunner {
    func run(
        executable: URL,
        args: [String],
        cwd: URL?,
        stdin: Data?
    ) async throws -> CommandResult {
        let process = Process()
        process.executableURL = executable
        process.arguments = args
        if let cwd { process.currentDirectoryURL = cwd }

        let stdoutPipe = Pipe()
        let stderrPipe = Pipe()
        let stdinPipe = stdin != nil ? Pipe() : nil

        process.standardOutput = stdoutPipe
        process.standardError = stderrPipe
        if let stdinPipe { process.standardInput = stdinPipe }

        // Keep a minimal PATH so `node`/shims inside the claude shell stub resolve.
        var env = ProcessInfo.processInfo.environment
        let pathAdditions = ["/usr/local/bin", "/opt/homebrew/bin", "/usr/bin", "/bin"]
        let existingPath = env["PATH"] ?? ""
        let merged = (pathAdditions + existingPath.split(separator: ":").map(String.init))
            .reduce(into: [String]()) { acc, p in if !acc.contains(p) { acc.append(p) } }
            .joined(separator: ":")
        env["PATH"] = merged
        process.environment = env

        let runner = ProcessRunActor(process: process, stdout: stdoutPipe, stderr: stderrPipe)

        return try await withTaskCancellationHandler {
            try await runner.run(stdin: stdin, stdinPipe: stdinPipe)
        } onCancel: {
            runner.terminate()
        }
    }
}

/// Wraps a running `Process` so cancellation can reach it from another task.
private final class ProcessRunActor: @unchecked Sendable {
    private let process: Process
    private let stdout: Pipe
    private let stderr: Pipe
    private let state = OSAllocatedUnfairLock(initialState: false)

    init(process: Process, stdout: Pipe, stderr: Pipe) {
        self.process = process
        self.stdout = stdout
        self.stderr = stderr
    }

    func terminate() {
        state.withLock { terminated in
            if terminated { return }
            terminated = true
            if process.isRunning {
                process.terminate()
            }
        }
    }

    private func wasTerminated() -> Bool {
        state.withLock { $0 }
    }

    func run(stdin: Data?, stdinPipe: Pipe?) async throws -> CommandResult {
        do {
            try process.run()
        } catch {
            throw CommandRunnerError.launchFailed(error.localizedDescription)
        }

        // Feed stdin then close.
        if let stdin, let stdinPipe {
            let handle = stdinPipe.fileHandleForWriting
            do {
                try handle.write(contentsOf: stdin)
                try handle.close()
            } catch {
                // Process may have exited before we finished writing — not fatal.
            }
        }

        // Drain pipes on background queues; `readDataToEndOfFile` blocks until EOF.
        async let stdoutData: Data = Task.detached {
            self.stdout.fileHandleForReading.readDataToEndOfFile()
        }.value
        async let stderrData: Data = Task.detached {
            self.stderr.fileHandleForReading.readDataToEndOfFile()
        }.value

        let outData = await stdoutData
        let errData = await stderrData

        process.waitUntilExit()

        let exit = process.terminationStatus
        let stderrString = String(data: errData, encoding: .utf8) ?? ""
        let tail = stderrString.suffix(500).trimmingCharacters(in: .whitespacesAndNewlines)

        if wasTerminated() {
            throw CommandRunnerError.cancelled
        }

        return CommandResult(exitCode: exit, stdoutData: outData, stderrTail: String(tail))
    }
}
