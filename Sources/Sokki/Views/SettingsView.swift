import SwiftUI
import ServiceManagement

struct SettingsView: View {
    @State var store: MatchStore

    var body: some View {
        TabView {
            MatchListView(store: store)
                .tabItem { Label("Matches", systemImage: "list.bullet") }

            GeneralSettingsView(store: store)
                .tabItem { Label("General", systemImage: "gear") }

            AISettingsView(store: store)
                .tabItem { Label("AI", systemImage: "sparkles") }

            PrivacySettingsView()
                .tabItem { Label("Privacy", systemImage: "hand.raised") }
        }
        .frame(minWidth: 500, minHeight: 400)
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

    var body: some View {
        Form {
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
        }
        .formStyle(.grouped)
        .padding()
        .onChange(of: store.config) { _, _ in
            store.saveConfig()
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
