import Foundation

@MainActor
final class AIRewriter {
    struct Request: Sendable {
        let prompt: String
        let input: String?
        let model: String
        let timeout: TimeInterval
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
            Logger.shared.error("[AI] binary not found (override=\(aiBinaryPath ?? "nil"))")
            return .failed(.binaryMissing)
        }

        let inputLen = req.input?.count ?? 0
        Logger.shared.error(
            "[AI] run binary=\(binary.path) model=\(req.model) timeout=\(Int(req.timeout))s inputLen=\(inputLen) gen=\(myGen)"
        )

        let task = Task<Event, Never> { [runner, scratchCwd, systemPromptAppend] in
            // NOTE: `--bare` is NOT passed. The CLI's `--bare` mode skips
            // credential loading for Claude subscriptions and returns
            // "Not logged in" even when `claude auth status` shows logged in.
            // Scratch cwd + `--tools ""` still prevent project CLAUDE.md
            // and tool access respectively. Cost is ~$0.01/call instead of
            // the ~$0.002 `--bare` would have given us.
            let args: [String] = [
                "--print",
                "--tools", "",
                "--model", req.model,
                "--output-format", "json",
                "--max-turns", "1",
                "--no-session-persistence",
                "--append-system-prompt", systemPromptAppend,
            ]

            var payload = req.prompt
            if let input = req.input, !input.isEmpty {
                payload += "\n\n" + input
            }
            let stdin = payload.data(using: .utf8) ?? Data()

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
            Logger.shared.error("[AI] cancelCurrent")
        }
        currentTask?.cancel()
        currentTask = nil
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
        // Throwing task group so Task.sleep cancellation propagates cleanly
        // and the runner-first path can cancel the sleep child immediately.
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

                // First child to produce a value wins; cancel the rest.
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
        Logger.shared.error(
            "[AI] process exit=\(result.exitCode) stdoutBytes=\(result.stdoutData.count) stderrLen=\(result.stderrTail.count)"
        )

        // Auth failure detection — CLI prints a sign-in hint on stderr and
        // typically exits non-zero before producing JSON.
        let stderrLower = result.stderrTail.lowercased()
        let authHints = ["log in", "logged in", "authenticate", "not signed in", "sign in"]
        if result.exitCode != 0, authHints.contains(where: { stderrLower.contains($0) }) {
            Logger.shared.error("[AI] parse: authRequired stderrTail=\(result.stderrTail.prefix(200))")
            return .failed(.authRequired)
        }

        if result.stdoutData.isEmpty {
            if result.exitCode == 0 {
                Logger.shared.error("[AI] parse: empty stdout with exit=0")
                return .failed(.protocolError("empty stdout"))
            }
            Logger.shared.error("[AI] parse: nonZeroExit code=\(result.exitCode) stderrTail=\(result.stderrTail.prefix(200))")
            return .failed(.nonZeroExit(code: result.exitCode, stderrTail: result.stderrTail))
        }

        do {
            guard let obj = try JSONSerialization.jsonObject(with: result.stdoutData) as? [String: Any] else {
                Logger.shared.error("[AI] parse: non-object JSON")
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
                    Logger.shared.error("[AI] parse: authRequired (json) msg=\(msg.prefix(200))")
                    return .failed(.authRequired)
                }
                Logger.shared.error("[AI] parse: is_error=true msg=\(msg.prefix(200))")
                return .failed(.nonZeroExit(code: result.exitCode, stderrTail: msg))
            }
            if text.isEmpty {
                Logger.shared.error("[AI] parse: missing `result` field keys=\(obj.keys.joined(separator: ","))")
                return .failed(.protocolError("missing `result` field"))
            }
            Logger.shared.error(
                "[AI] parse: ok resultLen=\(text.count) cliDurationMs=\(durationMs) costUSD=\(cost)"
            )
            return .completed(text)
        } catch {
            Logger.shared.error("[AI] parse: JSON decode \(error.localizedDescription)")
            return .failed(.protocolError("JSON decode: \(error.localizedDescription)"))
        }
    }
}
