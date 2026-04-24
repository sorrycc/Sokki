import AppKit
import SwiftUI
import ServiceManagement

struct SettingsView: View {
    @State var store: MatchStore
    @State private var selectedTab: Int = 0

    var body: some View {
        TabView(selection: $selectedTab) {
            MatchListView(store: store)
                .tabItem { Label("Matches", systemImage: "list.bullet") }
                .tag(0)

            GeneralSettingsView(store: store)
                .tabItem { Label("General", systemImage: "gear") }
                .tag(1)

            AISettingsView(store: store)
                .tabItem { Label("AI", systemImage: "sparkles") }
                .tag(2)

            PrivacySettingsView()
                .tabItem { Label("Privacy", systemImage: "hand.raised") }
                .tag(3)
        }
        .frame(minWidth: 500, minHeight: 400)
        .background {
            Group {
                Button("") { selectedTab = 0 }.keyboardShortcut("1", modifiers: .command)
                Button("") { selectedTab = 1 }.keyboardShortcut("2", modifiers: .command)
                Button("") { selectedTab = 2 }.keyboardShortcut("3", modifiers: .command)
                Button("") { selectedTab = 3 }.keyboardShortcut("4", modifiers: .command)
            }
            .opacity(0)
            .frame(width: 0, height: 0)
            .accessibilityHidden(true)
        }
    }
}

struct GeneralSettingsView: View {
    @Bindable var store: MatchStore
    @State private var launchAtLogin = false
    @AppStorage("SUEnableAutomaticChecks") private var automaticallyCheckForUpdates = true

    private var updatesConfigured: Bool {
        Bundle.main.object(forInfoDictionaryKey: "SUFeedURL") != nil
    }

    private var currentVersion: String {
        Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "?"
    }

    var body: some View {
        Form {
            Section("Behavior") {
                Toggle("Launch at Login", isOn: $launchAtLogin)
                    .onChange(of: launchAtLogin) { _, newValue in
                        setLaunchAtLogin(newValue)
                    }

                TextField("Clipboard threshold (chars)", value: $store.config.clipboardThreshold, format: .number)
                    .help("Text longer than this will use clipboard injection")

                Toggle("Preserve clipboard", isOn: $store.config.preserveClipboard)
                    .help("Restore clipboard after clipboard-mode injection")

                TextField("Inject delay (ms)", value: $store.config.injectDelay, format: .number)
                    .help("Delay between simulated keypresses")

                TextField("Restore clipboard delay (ms)", value: $store.config.restoreClipboardDelay, format: .number)
            }

            Section("Logging") {
                Picker("Log level", selection: $store.config.logLevel) {
                    Text("Error").tag("error")
                    Text("Debug").tag("debug")
                }
            }

            Section("Toggle Shortcut") {
                Picker("Quick toggle", selection: $store.config.toggleShortcut) {
                    Text("Double Option").tag("double_option")
                    Text("Double Shift").tag("double_shift")
                    Text("Double Control").tag("double_control")
                    Text("None").tag("none")
                }
                .help("Double-tap to toggle Sokki on/off")
            }

            Section("Palette Shortcut") {
                Picker("Open palette", selection: $store.config.paletteShortcut) {
                    Text("⌥ Space").tag("opt+space")
                    Text("⌃ Space").tag("ctrl+space")
                    Text("⌃⌥ Space").tag("ctrl+opt+space")
                    Text("⌃⇧ Space").tag("ctrl+shift+space")
                    Text("None").tag("none")
                }
                .help("Spotlight-style fuzzy search across all your matches")
            }

            if updatesConfigured {
                Section("Updates") {
                    HStack {
                        Text("Current version")
                        Spacer()
                        Text(currentVersion).foregroundStyle(.secondary)
                    }
                    Toggle("Automatically check for updates", isOn: $automaticallyCheckForUpdates)
                    HStack {
                        Spacer()
                        Button("Check for Updates…") {
                            NotificationCenter.default.post(name: .checkForUpdates, object: nil)
                        }
                    }
                }
            }
        }
        .formStyle(.grouped)
        .padding()
        .onAppear {
            launchAtLogin = SMAppService.mainApp.status == .enabled
        }
        .onChange(of: store.config) { _, _ in
            store.saveConfig()
        }
    }

    private func setLaunchAtLogin(_ enabled: Bool) {
        do {
            if enabled {
                try SMAppService.mainApp.register()
            } else {
                try SMAppService.mainApp.unregister()
            }
        } catch {
            Logger.shared.error("Failed to set launch at login: \(error.localizedDescription)")
        }
    }
}

struct AISettingsView: View {
    @Bindable var store: MatchStore
    @State private var controller = AITestController()

    var body: some View {
        Form {
            Section {
                Text("AI features are powered by your locally-installed Claude Code CLI.")
                    .foregroundStyle(.secondary)
            }

            Section("Model") {
                TextField("Model", text: $store.config.ai.model)

                TextField("Claude binary", text: binaryBinding, prompt: Text("Auto-detect"))
                    .help("Path to the `claude` CLI. Leave blank to auto-detect: ~/.claude/local/claude, ~/.local/bin/claude, /opt/homebrew/bin/claude, /usr/local/bin/claude, then shell lookup.")

                TextField("Timeout (seconds)", value: $store.config.ai.timeoutSeconds, format: .number)
                    .help("Max seconds to wait for the AI response before failing.")

                TextField("Selection capture timeout (ms)", value: $store.config.ai.selectionCaptureTimeoutMs, format: .number)
            }

            Section("Placeholder") {
                Picker("Placeholder style", selection: $store.config.ai.placeholderStyle) {
                    Text("Static").tag(PlaceholderStyle.static)
                    Text("Braille").tag(PlaceholderStyle.braille)
                    Text("Block sweep").tag(PlaceholderStyle.blockSweep)
                }
            }

            Section("Test") {
                HStack {
                    Button(controller.state.isRunning ? "Cancel" : "Test connection") {
                        if controller.state.isRunning {
                            controller.cancel()
                        } else {
                            controller.start(config: store.config.ai)
                        }
                    }

                    if controller.state.isRunning {
                        ProgressView()
                            .controlSize(.small)
                            .padding(.leading, 4)
                    }

                    Spacer()
                }

                testResultView
            }
        }
        .formStyle(.grouped)
        .padding()
        .onChange(of: store.config) { _, _ in
            store.saveConfig()
        }
        .onReceive(NotificationCenter.default.publisher(for: NSWindow.willCloseNotification)) { note in
            if let window = note.object as? NSWindow, window.title == "Sokki Settings" {
                controller.cancel()
            }
        }
    }

    @ViewBuilder
    private var testResultView: some View {
        switch controller.state {
        case .idle:
            EmptyView()
        case .running:
            Text("Running…")
                .foregroundStyle(.secondary)
                .frame(maxWidth: .infinity, alignment: .leading)
        case .ok(let binary, let model, let reply):
            VStack(alignment: .leading, spacing: 4) {
                Label("Connection OK", systemImage: "checkmark.circle.fill")
                    .foregroundStyle(.green)
                Text("Binary: \(binary)")
                    .font(.caption).foregroundStyle(.secondary)
                    .lineLimit(1).truncationMode(.middle)
                Text("Model: \(model)")
                    .font(.caption).foregroundStyle(.secondary)
                    .lineLimit(1)
                Text("Reply: \(reply)")
                    .font(.caption).foregroundStyle(.secondary)
                    .lineLimit(3)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        case .fail(let detail):
            VStack(alignment: .leading, spacing: 4) {
                Label("Test failed", systemImage: "xmark.octagon.fill")
                    .foregroundStyle(.red)
                Text(detail)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(8)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private var binaryBinding: Binding<String> {
        Binding(
            get: { store.config.ai.binary ?? "" },
            set: { newValue in
                store.config.ai.binary = newValue.isEmpty ? nil : newValue
            }
        )
    }
}

@MainActor
@Observable
final class AITestController {
    enum State {
        case idle
        case running
        case ok(binary: String, model: String, reply: String)
        case fail(detail: String)

        var isRunning: Bool {
            if case .running = self { return true }
            return false
        }
    }

    var state: State = .idle
    @ObservationIgnored private var task: Task<Void, Never>?

    func start(config: AISettings) {
        task?.cancel()
        state = .running

        let binaryOverride = config.binary
        let model = config.model
        let timeout = TimeInterval(max(config.timeoutSeconds, 1))
        let userArgs = config.args

        task = Task { [weak self] in
            let resolver = ClaudeBinaryResolver()
            let rewriter = AIRewriter(
                resolver: resolver,
                scratchCwd: Self.scratchDirectory()
            )
            let resolvedPath = resolver.resolve(explicitPath: binaryOverride)?.path ?? "(not found)"

            let event = await rewriter.run(
                AIRewriter.Request(
                    prompt: "ping",
                    model: model,
                    timeout: timeout,
                    userArgs: userArgs
                ),
                aiBinaryPath: binaryOverride
            )

            guard let self else { return }
            if Task.isCancelled { return }

            switch event {
            case .completed(let text):
                let reply = Self.truncate(text.trimmingCharacters(in: .whitespacesAndNewlines), limit: 200)
                self.state = .ok(binary: resolvedPath, model: model, reply: reply)
            case .cancelled:
                return
            case .failed(let reason):
                self.state = .fail(detail: Self.detail(for: reason, binary: resolvedPath, timeout: timeout))
            }
            self.task = nil
        }
    }

    func cancel() {
        task?.cancel()
        task = nil
        state = .idle
    }

    private static func scratchDirectory() -> URL {
        let appSupport = FileManager.default.urls(
            for: .applicationSupportDirectory,
            in: .userDomainMask
        ).first ?? FileManager.default.temporaryDirectory
        let dir = appSupport.appendingPathComponent("Sokki/ai-scratch", isDirectory: true)
        try? FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        return dir
    }

    private static func detail(for reason: AIRewriter.Reason, binary: String, timeout: TimeInterval) -> String {
        let header: String
        switch reason {
        case .binaryMissing:
            return "`claude` binary not found.\nSet a path in 'Claude binary' above, or install the Claude Code CLI."
        case .authRequired:
            header = "Not signed in to Claude Code CLI.\nRun `claude` in a terminal and sign in, then retry."
        case .timeout:
            header = "Timed out after \(Int(timeout))s.\nIncrease the Timeout field above or check your network."
        case .nonZeroExit(let code, let tail):
            let trimmed = tail.trimmingCharacters(in: .whitespacesAndNewlines)
            let tailText = trimmed.isEmpty ? "(no stderr output)" : truncate(trimmed, limit: 500)
            header = "CLI exited with code \(code):\n\(tailText)"
        case .protocolError(let s):
            header = "Protocol error: \(s)"
        }
        return "\(header)\nBinary: \(binary)"
    }

    private static func truncate(_ s: String, limit: Int) -> String {
        if s.count <= limit { return s }
        return String(s.prefix(limit)) + "…"
    }
}
