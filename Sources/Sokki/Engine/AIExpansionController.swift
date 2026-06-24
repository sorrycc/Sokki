import AppKit
import Carbon.HIToolbox
import Foundation

/// Owns the AI-rewrite expansion flow: selection/clipboard capture, animated
/// placeholder, `claude` subprocess orchestration, and result/failure cleanup.
/// Split out of `KeystrokeMonitor` so the monitor stays focused on the event
/// tap; this controller talks to the shared `TapContext` + `TextInjector`.
///
/// All entry points run on the MainActor. Suppression/AI-state bookkeeping is
/// delegated to `TapContext`, which the tap thread also reads.
@MainActor
final class AIExpansionController {
    private let tapContext: TapContext
    private let textInjector: TextInjector
    private let aiClipboardManager = ClipboardManager()
    private let aiResolver = ClaudeBinaryResolver()
    private lazy var aiRewriter: AIRewriter = AIRewriter(
        resolver: aiResolver,
        scratchCwd: aiScratchDirectory()
    )

    var config = AppConfig()
    var onHUDEvent: ((HUDEvent) -> Void)?

    init(tapContext: TapContext, textInjector: TextInjector) {
        self.tapContext = tapContext
        self.textInjector = textInjector
    }

    /// Cancel an in-flight request (Esc during the AI wait).
    func cancelCurrent() {
        aiRewriter.cancelCurrent()
    }

    private func aiScratchDirectory() -> URL {
        let appSupport = FileManager.default.urls(
            for: .applicationSupportDirectory,
            in: .userDomainMask
        ).first ?? FileManager.default.temporaryDirectory
        let dir = appSupport.appendingPathComponent("Sokki/ai-scratch", isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        return dir
    }

    // MARK: - Placeholder frames

    private static let placeholderWord = "generating…"
    private static let brailleFrames: [Character] = ["⠋","⠙","⠹","⠸","⠼","⠴","⠦","⠧","⠇","⠏"]
    private static let blockFrames:   [Character] = ["▏","▎","▍","▌","▋","▊","▉","█"]

    private static func placeholderFrame(style: PlaceholderStyle, index: Int) -> String {
        switch style {
        case .static:
            return placeholderWord
        case .braille:
            return String(brailleFrames[index % brailleFrames.count])
        case .blockSweep:
            return String(blockFrames[index % blockFrames.count])
        }
    }

    private var animationTask: Task<Void, Never>?
    private var aiPlaceholderLength: Int = 0
    private var aiMouseMonitor: Any?

    enum AIInvocation: Sendable {
        case typedTrigger(length: Int)
        case palette

        var label: String {
            switch self {
            case .typedTrigger: return "typed"
            case .palette:      return "palette"
            }
        }
    }

    // MARK: - Entry points

    /// Entry point for typed-trigger AI matches. Called from the monitor's
    /// `onMatch` on MainActor. `suppressMatching` is already true (set on the
    /// tap thread before dispatch).
    func handleTypedTrigger(match: Match, triggerLength: Int) async {
        await runAIFlow(match: match, invocation: .typedTrigger(length: triggerLength))
    }

    /// Entry point for palette-committed AI matches. Polls until the target
    /// app regains frontmost status (up to ~80 ms), then delegates.
    func handleFromPalette(match: Match, target: NSRunningApplication) async {
        let deadline = Date().addingTimeInterval(0.08)
        while Date() < deadline,
              NSWorkspace.shared.frontmostApplication?.processIdentifier != target.processIdentifier {
            try? await Task.sleep(nanoseconds: 10_000_000)
        }
        await runAIFlow(match: match, invocation: .palette)
    }

    // MARK: - Flow

    private func runAIFlow(match: Match, invocation: AIInvocation) async {
        guard let promptRaw = match.prompt else {
            tapContext.setSuppressMatching(false)
            return
        }

        // Parse the template. Validated at load, so parse cannot fail for a
        // match that reached us; treat a throw as a protocol error.
        let template: PromptTemplate
        do {
            template = try PromptTemplate.parse(promptRaw)
        } catch {
            Logger.shared.error("[AI] template parse failed (should not happen): \(error.localizedDescription)")
            emitHUD(.aiFailed, trigger: match.trigger, note: "bad prompt")
            tapContext.setSuppressMatching(false)
            return
        }

        // §5.2 Concurrency guard: refuse overlapping AI runs. Checked BEFORE
        // any state mutation / clipboard save / injection, so an in-flight
        // run is not disturbed.
        if tapContext.isAIActive {
            Logger.shared.warning("[AI] refused — another AI run is in flight")
            emitHUD(.aiFailed, trigger: match.trigger, note: "AI busy")
            return
        }

        let started = Date()
        Logger.shared.debug(
            "[AI] match trigger=\(match.trigger) invocation=\(invocation.label) promptLen=\(promptRaw.count) placeholders=\(template.placeholders.count) model=\(config.ai.model)"
        )

        // Idempotent: palette path needs this set; typed path already has it.
        tapContext.setSuppressMatching(true)

        // Erase the typed trigger, if there was one.
        switch invocation {
        case .typedTrigger(let n):
            textInjector.eraseInjected(count: n)
        case .palette:
            break
        }

        // Capture inputs, in this order: clipboard FIRST (before Cmd-C
        // overwrites the pasteboard), then selection via Cmd-C.
        var clipboardText: String? = nil
        if template.needsClipboard {
            let s = NSPasteboard.general.string(forType: .string) ?? ""
            clipboardText = s
            Logger.shared.debug("[AI] clipboard snapshot len=\(s.count)")
        }

        var selectionText: String? = nil
        if template.needsSelection {
            let captureStart = Date()
            let captured = await captureSelectionViaCmdC(
                timeoutMs: config.ai.selectionCaptureTimeoutMs,
                invocation: invocation
            )
            let captureMs = Int(Date().timeIntervalSince(captureStart) * 1000)
            if let captured {
                selectionText = captured
                Logger.shared.debug("[AI] selection captured len=\(captured.count) in \(captureMs)ms")
            } else if template.requiresSelection {
                Logger.shared.warning("[AI] no selection (timeout=\(config.ai.selectionCaptureTimeoutMs)ms, elapsed=\(captureMs)ms)")
                if case .typedTrigger = invocation {
                    textInjector.injectDelta(match.trigger)
                }
                emitHUD(.aiFailed, trigger: match.trigger, note: "no selection")
                tapContext.setSuppressMatching(false)
                return
            } else {
                // Selection missing but some chain has a non-selection
                // fallback (e.g. `{{s|c}}`). Leave selectionText = nil and let
                // render() pick the next source; if every chain is unresolved
                // the render-abort branch below will surface it.
                Logger.shared.warning("[AI] selection missing — falling back to chain (elapsed=\(captureMs)ms)")
            }
        }

        // Render. If any placeholder chain is all-empty, abort with a HUD
        // note naming the offending placeholder.
        guard let rendered = template.render(selection: selectionText, clipboard: clipboardText) else {
            let chain = template.firstUnresolvedChain(selection: selectionText, clipboard: clipboardText) ?? "{{…}}"
            Logger.shared.warning("[AI] render aborted: no input for \(chain)")
            if case .typedTrigger = invocation {
                textInjector.injectDelta(match.trigger)
            }
            emitHUD(.aiFailed, trigger: match.trigger, note: "no input for \(chain)")
            tapContext.setSuppressMatching(false)
            return
        }

        // Inject placeholder (frame 0) while still suppressed. For palette+selection
        // this single unicode-string event replaces the still-live selection.
        let style = config.ai.placeholderStyle
        let initialFrame = Self.placeholderFrame(style: style, index: 0)
        textInjector.injectDelta(initialFrame)
        aiPlaceholderLength = initialFrame.count

        // Drain the HID pipeline so our own synthesized events arrive at the
        // tap while suppressMatching is still true.
        try? await Task.sleep(nanoseconds: 50_000_000) // 50ms

        tapContext.setActiveAIRequest(true)
        tapContext.setSuppressMatching(false)
        beginAIMouseWatch()

        emitHUD(.aiThinking, trigger: match.trigger, note: nil)
        Logger.shared.debug("[AI] placeholder injected (style=\(style.rawValue)), dispatching request")
        startAnimationTaskIfNeeded(style: style)

        let request = AIRewriter.Request(
            prompt: rendered,
            model: config.ai.model,
            timeout: TimeInterval(config.ai.timeoutSeconds),
            userArgs: config.ai.args
        )
        let event = await aiRewriter.run(request, aiBinaryPath: config.ai.binary)

        // Fence suppression BEFORE cancelling the animation so a cooperative
        // resume in the listen window cannot flip suppressMatching back to false.
        tapContext.setSuppressMatching(true)
        animationTask?.cancel()
        animationTask = nil
        endAIMouseWatch()
        let typed = tapContext.consumeUserTypedDuringAI()
        let placeholderLength = aiPlaceholderLength
        let elapsedMs = Int(Date().timeIntervalSince(started) * 1000)

        switch event {
        case .completed(let text):
            Logger.shared.debug("[AI] completed len=\(text.count) typedDuringWait=\(typed) elapsed=\(elapsedMs)ms")
            if !typed {
                textInjector.eraseInjected(count: placeholderLength)
                textInjector.injectDelta(text)
                emitHUD(.aiCompleted, trigger: match.trigger, note: nil)
            } else {
                textInjector.injectDelta(text)
                emitHUD(
                    .aiCompleted,
                    trigger: match.trigger,
                    note: "result appended; loading indicator left inline — delete manually"
                )
            }
            if case .typedTrigger = invocation {
                tapContext.recordExpansion(trigger: match.trigger, replacement: text)
            }
            tapContext.clearBuffer()

        case .failed(let reason):
            Logger.shared.warning("[AI] failed reason=\(reasonString(reason)) typedDuringWait=\(typed) elapsed=\(elapsedMs)ms")
            restoreAfterAIFailure(
                trigger: match.trigger,
                selection: selectionText,
                placeholderLength: placeholderLength,
                typed: typed,
                invocation: invocation
            )
            emitHUD(.aiFailed, trigger: match.trigger, note: reasonString(reason))

        case .cancelled:
            Logger.shared.warning("[AI] cancelled typedDuringWait=\(typed) elapsed=\(elapsedMs)ms")
            restoreAfterAIFailure(
                trigger: match.trigger,
                selection: selectionText,
                placeholderLength: placeholderLength,
                typed: typed,
                invocation: invocation
            )
            emitHUD(.aiFailed, trigger: match.trigger, note: "cancelled")
        }

        tapContext.setSuppressMatching(false)
    }

    private func restoreAfterAIFailure(
        trigger: String,
        selection: String?,
        placeholderLength: Int,
        typed: Bool,
        invocation: AIInvocation
    ) {
        if typed { return }
        textInjector.eraseInjected(count: placeholderLength)
        if let selection, !selection.isEmpty {
            textInjector.injectDelta(selection)
        } else if case .typedTrigger = invocation {
            // Typed path: re-inject the trigger literal so the user's context
            // is preserved. Palette path: nothing to re-inject.
            textInjector.injectDelta(trigger)
        }
    }

    /// Drive the animated placeholder (no-op for `.static`). Each frame cycle:
    /// suppress → erase prior frame → inject next frame → drain 70ms → unsuppress →
    /// listen 50ms. Self-cancels on user typing via a non-destructive peek so the
    /// completion branch's atomic `consumeUserTypedDuringAI` is preserved.
    private func startAnimationTaskIfNeeded(style: PlaceholderStyle) {
        guard style != .static else { return }
        animationTask?.cancel()
        animationTask = Task { @MainActor [weak self] in
            var index = 1 // frame 0 already injected by runAIFlow
            // Show frame 0 for a full listen window before the first swap.
            try? await Task.sleep(nanoseconds: 50_000_000)

            while !Task.isCancelled {
                guard let self else { return }
                if self.tapContext.peekUserTypedDuringAI() { return }

                self.tapContext.setSuppressMatching(true)
                self.textInjector.eraseInjected(count: self.aiPlaceholderLength)
                let frame = Self.placeholderFrame(style: style, index: index)
                self.textInjector.injectDelta(frame)
                self.aiPlaceholderLength = frame.count

                try? await Task.sleep(nanoseconds: 70_000_000)
                if Task.isCancelled { return }
                self.tapContext.setSuppressMatching(false)

                try? await Task.sleep(nanoseconds: 50_000_000)
                index &+= 1
            }
        }
    }

    // §5.1 Mouse-click detection during AI wait. A click in the target app
    // can move the caret, which would make `eraseInjected(placeholderLength)`
    // delete unrelated user text on completion. Treat clicks the same as
    // keystrokes for the detect-and-degrade path. Global monitor — Sokki's
    // own windows don't fire it, so HUD / palette clicks don't false-positive.
    private func beginAIMouseWatch() {
        guard aiMouseMonitor == nil else { return }
        aiMouseMonitor = NSEvent.addGlobalMonitorForEvents(
            matching: [.leftMouseDown, .rightMouseDown, .otherMouseDown]
        ) { [weak self] _ in
            self?.tapContext.noteUserTypedDuringAI()
        }
    }

    private func endAIMouseWatch() {
        if let m = aiMouseMonitor {
            NSEvent.removeMonitor(m)
            aiMouseMonitor = nil
        }
    }

    private func reasonString(_ reason: AIRewriter.Reason) -> String {
        switch reason {
        case .timeout: return "timeout"
        case .binaryMissing: return "`claude` binary not found"
        case .authRequired: return "run `claude auth login` to sign in"
        case .nonZeroExit(let code, let tail):
            let trimmed = tail.prefix(120)
            return "exit \(code): \(trimmed)"
        case .protocolError(let s):
            return "protocol: \(s)"
        }
    }

    private func emitHUD(_ verdict: HUDEvent.Verdict, trigger: String, note: String?) {
        let event = HUDEvent(
            verdict: verdict,
            capturedBuffer: trigger,
            matchedTrigger: trigger,
            note: note
        )
        onHUDEvent?(event)
    }

    /// Save clipboard → Cmd+C → poll → read selection → restore.
    /// Returns nil when nothing was selected (changeCount didn't tick).
    ///
    /// For typed-trigger invocations, the trigger has already been erased so
    /// the selection is still live after Cmd+C; we post a Backspace to delete
    /// it before the caller injects the placeholder. For palette
    /// invocations, the placeholder's unicode-string event replaces the
    /// still-live selection on its own — no Backspace needed (posting one
    /// would delete a character of the preceding text).
    private func captureSelectionViaCmdC(timeoutMs: Int, invocation: AIInvocation) async -> String? {
        let pb = NSPasteboard.general
        let before = pb.changeCount
        aiClipboardManager.save()

        postCmdC()

        let deadline = Date().addingTimeInterval(Double(timeoutMs) / 1000.0)
        while Date() < deadline, pb.changeCount == before {
            try? await Task.sleep(nanoseconds: 10_000_000) // 10ms
        }

        guard pb.changeCount > before,
              let text = pb.string(forType: .string),
              !text.isEmpty
        else {
            aiClipboardManager.restore()
            return nil
        }

        if case .typedTrigger = invocation {
            postBackspaceOnce()
        }
        aiClipboardManager.restore()
        return text
    }

    private func postCmdC() {
        let source = CGEventSource(stateID: .hidSystemState)
        let cKeyCode: CGKeyCode = 0x08
        if let down = CGEvent(keyboardEventSource: source, virtualKey: cKeyCode, keyDown: true),
           let up = CGEvent(keyboardEventSource: source, virtualKey: cKeyCode, keyDown: false) {
            down.flags = .maskCommand
            up.flags = .maskCommand
            down.post(tap: .cghidEventTap)
            up.post(tap: .cghidEventTap)
        }
    }

    private func postBackspaceOnce() {
        let source = CGEventSource(stateID: .hidSystemState)
        let bs: CGKeyCode = 0x33
        if let down = CGEvent(keyboardEventSource: source, virtualKey: bs, keyDown: true) {
            down.post(tap: .cghidEventTap)
        }
        if let up = CGEvent(keyboardEventSource: source, virtualKey: bs, keyDown: false) {
            up.post(tap: .cghidEventTap)
        }
    }
}
