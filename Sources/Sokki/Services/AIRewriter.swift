import Foundation

@MainActor
final class AIRewriter {
    struct Request: Sendable {
        let prompt: String
        let model: String
        let timeout: TimeInterval
        let userArgs: [String]?
    }

    enum Event: Sendable {
        case completed(String)
        case failed(Reason)
        case cancelled
    }

    enum Reason: Sendable, Equatable {
        case timeout
        case binaryMissing
        case authRequired
        case nonZeroExit(code: Int32, stderrTail: String)
        case protocolError(String)
    }

    private let runner: CommandRunner
    private let resolver: ClaudeBinaryResolver
    private let scratchCwd: URL
    private let systemPromptAppend: String
    private var currentTask: Task<Event, Never>?
    private var generation: UInt64 = 0

    init(
        runner: CommandRunner = ProcessCommandRunner(),
        resolver: ClaudeBinaryResolver,
        scratchCwd: URL,
        systemPromptAppend: String = AIRewriter.defaultSystemPromptAppend
    ) {
        self.runner = runner
        self.resolver = resolver
        self.scratchCwd = scratchCwd
        self.systemPromptAppend = systemPromptAppend
    }

    nonisolated static let defaultSystemPromptAppend =
        "Output only the transformed text. Do not add preamble, quotes, explanations, or commentary."

    func run(_ req: Request, aiBinaryPath: String?) async -> Event {
        currentTask?.cancel()
        generation &+= 1
        let myGen = generation

        guard let binary = resolver.resolve(explicitPath: aiBinaryPath) else {
            Logger.shared.warning("[AI] binary not found (override=\(aiBinaryPath ?? "nil"))")
            return .failed(.binaryMissing)
        }

        let canon = Self.canonicalizeArgs(
            userArgs: req.userArgs,
            model: req.model,
            systemPromptAppend: systemPromptAppend
        )
        for line in canon.logs { Logger.shared.debug(line) }

        Logger.shared.debug(
            "[AI] run binary=\(binary.path) model=\(req.model) timeout=\(Int(req.timeout))s promptLen=\(req.prompt.count) gen=\(myGen)"
        )

        let args = canon.args
        let stdin = req.prompt.data(using: .utf8) ?? Data()

        let task = Task<Event, Never> { [runner, scratchCwd] in
            return await Self.execute(
                runner: runner,
                executable: binary,
                args: args,
                cwd: scratchCwd,
                stdin: stdin,
                timeout: req.timeout
            )
        }
        currentTask = task
        let result = await task.value
        if myGen == generation {
            currentTask = nil
        }
        return result
    }

    func cancelCurrent() {
        if currentTask != nil {
            Logger.shared.debug("[AI] cancelCurrent")
        }
        currentTask?.cancel()
        currentTask = nil
    }

    // MARK: - Arg canonicalisation (pure, testable)

    struct CanonicalArgs: Equatable, Sendable {
        let args: [String]
        let logs: [String]
    }

    /// Applies the force-floor (`--print`, `--output-format json`,
    /// `--append-system-prompt`) and the `--model` convenience injection.
    /// See `docs/designs/2026-04-23-ai-templated-prompts.md` §3.
    nonisolated static func canonicalizeArgs(
        userArgs: [String]?,
        model: String,
        systemPromptAppend: String
    ) -> CanonicalArgs {
        let defaults: [String] = [
            "--print",
            "--tools", "",
            "--output-format", "json",
            "--max-turns", "1",
            "--no-session-persistence",
            "--append-system-prompt", systemPromptAppend,
        ]

        var args = userArgs ?? defaults
        var logs: [String] = []

        // --output-format json (value-bearing flag; force)
        forceValue(
            args: &args,
            flag: "--output-format",
            required: "json",
            logs: &logs
        )

        // --append-system-prompt <systemPromptAppend> (value-bearing; force)
        forceValue(
            args: &args,
            flag: "--append-system-prompt",
            required: systemPromptAppend,
            logs: &logs,
            redactValues: true
        )

        // --print (flag-only; insert if absent)
        forceFlag(args: &args, flag: "--print", logs: &logs)

        // --model <model> (convenience; only inject if absent and model non-empty)
        if !model.isEmpty, Self.indexOfFlag("--model", in: args) == nil {
            args.append(contentsOf: ["--model", model])
        }

        return CanonicalArgs(args: args, logs: logs)
    }

    private nonisolated static func forceValue(
        args: inout [String],
        flag: String,
        required: String,
        logs: inout [String],
        redactValues: Bool = false
    ) {
        if let idx = Self.indexOfFlag(flag, in: args) {
            let valueIdx = idx + 1
            if valueIdx < args.count {
                let existing = args[valueIdx]
                if existing == required { return } // no-op
                args[valueIdx] = required
                let shownOld = redactValues ? truncate(existing) : existing
                let shownNew = redactValues ? truncate(required) : required
                logs.append("[AI] overrode \(flag)=\(shownNew) (user had: \(shownOld))")
            } else {
                // Flag without value — replace in place with flag+value.
                args.append(required)
                let shownNew = redactValues ? truncate(required) : required
                logs.append("[AI] inserted value for dangling \(flag)=\(shownNew)")
            }
        } else {
            args.append(contentsOf: [flag, required])
            let shownNew = redactValues ? truncate(required) : required
            logs.append("[AI] inserted \(flag)=\(shownNew) (user did not set)")
        }
    }

    private nonisolated static func forceFlag(
        args: inout [String],
        flag: String,
        logs: inout [String]
    ) {
        if Self.indexOfFlag(flag, in: args) != nil { return }
        args.append(flag)
        logs.append("[AI] inserted \(flag) (user did not set)")
    }

    private nonisolated static func indexOfFlag(_ flag: String, in args: [String]) -> Int? {
        args.firstIndex(of: flag)
    }

    private nonisolated static func truncate(_ s: String, limit: Int = 40) -> String {
        if s.count <= limit { return "\"\(s)\"" }
        return "\"\(s.prefix(limit))…\""
    }

    // MARK: - Private

    nonisolated private static func execute(
        runner: CommandRunner,
        executable: URL,
        args: [String],
        cwd: URL,
        stdin: Data,
        timeout: TimeInterval
    ) async -> Event {
        let timeoutNs = UInt64(timeout * 1_000_000_000)

        do {
            return try await withThrowingTaskGroup(of: Event.self) { group in
                group.addTask {
                    do {
                        let result = try await runner.run(
                            executable: executable,
                            args: args,
                            cwd: cwd,
                            stdin: stdin
                        )
                        return Self.parse(result: result)
                    } catch is CancellationError {
                        return .cancelled
                    } catch let e as CommandRunnerError {
                        switch e {
                        case .cancelled: return .cancelled
                        case .notFound: return .failed(.binaryMissing)
                        case .launchFailed(let s): return .failed(.protocolError("launch failed: \(s)"))
                        }
                    } catch {
                        return .failed(.protocolError(error.localizedDescription))
                    }
                }
                group.addTask {
                    try await Task.sleep(nanoseconds: timeoutNs)
                    return .failed(.timeout)
                }

                let first = try await group.next() ?? .failed(.protocolError("no result"))
                group.cancelAll()
                return first
            }
        } catch is CancellationError {
            return .cancelled
        } catch {
            return .failed(.protocolError(error.localizedDescription))
        }
    }

    nonisolated private static func parse(result: CommandResult) -> Event {
        Logger.shared.debug(
            "[AI] process exit=\(result.exitCode) stdoutBytes=\(result.stdoutData.count) stderrLen=\(result.stderrTail.count)"
        )

        let stderrLower = result.stderrTail.lowercased()
        let authHints = ["log in", "logged in", "authenticate", "not signed in", "sign in"]
        if result.exitCode != 0, authHints.contains(where: { stderrLower.contains($0) }) {
            Logger.shared.warning("[AI] parse: authRequired stderrTail=\(result.stderrTail.prefix(200))")
            return .failed(.authRequired)
        }

        if result.stdoutData.isEmpty {
            if result.exitCode == 0 {
                Logger.shared.warning("[AI] parse: empty stdout with exit=0")
                return .failed(.protocolError("empty stdout"))
            }
            Logger.shared.warning("[AI] parse: nonZeroExit code=\(result.exitCode) stderrTail=\(result.stderrTail.prefix(200))")
            return .failed(.nonZeroExit(code: result.exitCode, stderrTail: result.stderrTail))
        }

        do {
            guard let obj = try JSONSerialization.jsonObject(with: result.stdoutData) as? [String: Any] else {
                Logger.shared.warning("[AI] parse: non-object JSON")
                return .failed(.protocolError("non-object JSON"))
            }
            let isError = (obj["is_error"] as? Bool) ?? false
            let text = (obj["result"] as? String) ?? ""
            let durationMs = obj["duration_ms"] as? Int ?? -1
            let cost = obj["total_cost_usd"] as? Double ?? 0.0

            if isError {
                let msg = text.isEmpty ? result.stderrTail : text
                let msgLower = msg.lowercased()
                if msgLower.contains("not logged in")
                    || msgLower.contains("please run /login")
                    || msgLower.contains("log in")
                    || msgLower.contains("authenticate")
                {
                    Logger.shared.warning("[AI] parse: authRequired (json) msg=\(msg.prefix(200))")
                    return .failed(.authRequired)
                }
                Logger.shared.warning("[AI] parse: is_error=true msg=\(msg.prefix(200))")
                return .failed(.nonZeroExit(code: result.exitCode, stderrTail: msg))
            }
            if text.isEmpty {
                Logger.shared.warning("[AI] parse: missing `result` field keys=\(obj.keys.joined(separator: ","))")
                return .failed(.protocolError("missing `result` field"))
            }
            Logger.shared.debug(
                "[AI] parse: ok resultLen=\(text.count) cliDurationMs=\(durationMs) costUSD=\(cost)"
            )
            return .completed(text)
        } catch {
            Logger.shared.warning("[AI] parse: JSON decode \(error.localizedDescription)")
            return .failed(.protocolError("JSON decode: \(error.localizedDescription)"))
        }
    }
}
