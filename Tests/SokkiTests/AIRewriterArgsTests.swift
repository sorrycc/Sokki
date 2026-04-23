import Testing
@testable import Sokki

struct AIRewriterArgsTests {
    private let sysPrompt = "Output only transformed text."

    // MARK: - nil userArgs (defaults path)

    @Test func nilUserArgs_usesDefaultsAndAppendsModel() {
        let r = AIRewriter.canonicalizeArgs(
            userArgs: nil,
            model: "haiku",
            systemPromptAppend: sysPrompt
        )
        // Defaults already include --output-format json, --print,
        // --append-system-prompt — so no override/insert logs should fire.
        #expect(r.logs.isEmpty, "expected empty logs, got: \(r.logs)")
        #expect(r.args.contains("--print"))
        #expect(r.args.contains("--output-format"))
        #expect(r.args.contains("--append-system-prompt"))
        let idx = r.args.firstIndex(of: "--model")
        #expect(idx != nil)
        #expect(r.args[idx! + 1] == "haiku")
    }

    // MARK: - empty userArgs (floor-only)

    @Test func emptyUserArgs_insertsAllFloor() {
        let r = AIRewriter.canonicalizeArgs(
            userArgs: [],
            model: "haiku",
            systemPromptAppend: sysPrompt
        )
        #expect(r.args.contains("--print"))
        #expect(r.args.contains("--output-format"))
        #expect(r.args.contains("--append-system-prompt"))
        let idx = r.args.firstIndex(of: "--model")
        #expect(idx != nil)
        #expect(r.args[idx! + 1] == "haiku")
        // Three "inserted" logs (print, output-format, append-system-prompt)
        #expect(r.logs.filter { $0.contains("inserted") }.count == 3)
    }

    // MARK: - override

    @Test func overrideOutputFormat() {
        let r = AIRewriter.canonicalizeArgs(
            userArgs: ["--output-format", "text"],
            model: "haiku",
            systemPromptAppend: sysPrompt
        )
        let idx = r.args.firstIndex(of: "--output-format")!
        #expect(r.args[idx + 1] == "json")
        #expect(r.logs.contains { $0.contains("overrode --output-format=json") })
        #expect(r.logs.contains { $0.contains("user had: text") })
    }

    @Test func overrideAppendSystemPrompt_redactsValues() {
        let r = AIRewriter.canonicalizeArgs(
            userArgs: ["--append-system-prompt", "Be very long and chatty — say everything you think and then some, really elaborately"],
            model: "haiku",
            systemPromptAppend: sysPrompt
        )
        let idx = r.args.firstIndex(of: "--append-system-prompt")!
        #expect(r.args[idx + 1] == sysPrompt)
        let override = r.logs.first { $0.contains("overrode --append-system-prompt") }
        #expect(override != nil)
        // Truncated to 40 chars with ellipsis (user's value was longer).
        #expect(override!.contains("…"))
    }

    // MARK: - no-op when match

    @Test func noLog_whenExactMatch() {
        let r = AIRewriter.canonicalizeArgs(
            userArgs: [
                "--print",
                "--output-format", "json",
                "--append-system-prompt", sysPrompt,
            ],
            model: "haiku",
            systemPromptAppend: sysPrompt
        )
        #expect(r.logs.isEmpty, "expected no logs on exact match, got: \(r.logs)")
    }

    // MARK: - --model convenience

    @Test func modelNotInjectedWhenUserProvided() {
        let r = AIRewriter.canonicalizeArgs(
            userArgs: ["--model", "sonnet"],
            model: "haiku",
            systemPromptAppend: sysPrompt
        )
        let occurrences = r.args.enumerated().compactMap { $0.element == "--model" ? $0.offset : nil }
        #expect(occurrences.count == 1)
        #expect(r.args[occurrences[0] + 1] == "sonnet")
    }

    @Test func modelInjectedWhenMissing() {
        let r = AIRewriter.canonicalizeArgs(
            userArgs: ["--print"],
            model: "haiku",
            systemPromptAppend: sysPrompt
        )
        let idx = r.args.firstIndex(of: "--model")!
        #expect(r.args[idx + 1] == "haiku")
    }

    @Test func modelNotInjectedWhenEmpty() {
        let r = AIRewriter.canonicalizeArgs(
            userArgs: ["--print"],
            model: "",
            systemPromptAppend: sysPrompt
        )
        #expect(!r.args.contains("--model"))
    }

    // MARK: - --print insert

    @Test func printInserted() {
        let r = AIRewriter.canonicalizeArgs(
            userArgs: ["--output-format", "json", "--append-system-prompt", sysPrompt],
            model: "haiku",
            systemPromptAppend: sysPrompt
        )
        #expect(r.args.contains("--print"))
        #expect(r.logs.contains { $0.contains("inserted --print") })
    }
}
