import SwiftUI

struct MatchEditorView: View {
    @State private var trigger: String
    @State private var replace: String
    @State private var isEnabled: Bool
    @State private var sourceFile: String
    private let existingId: UUID?
    private let availableFiles: [String]

    let onSave: (Match) -> Void
    let onCancel: () -> Void

    init(
        match: Match? = nil,
        availableFiles: [String],
        prefillSourceFile: String,
        onSave: @escaping (Match) -> Void,
        onCancel: @escaping () -> Void
    ) {
        self._trigger = State(initialValue: match?.trigger ?? ":")
        self._replace = State(initialValue: match?.replace ?? "")
        self._isEnabled = State(initialValue: match?.isEnabled ?? true)
        self._sourceFile = State(initialValue: match?.sourceFile ?? prefillSourceFile)
        self.existingId = match?.id
        self.availableFiles = availableFiles
        self.onSave = onSave
        self.onCancel = onCancel
    }

    var body: some View {
        VStack(spacing: 16) {
            Text(existingId != nil ? "Edit Match" : "New Match")
                .font(.headline)

            Form {
                TextField("Trigger", text: $trigger)
                    .font(.system(.body, design: .monospaced))
                    .help("The keyword that triggers expansion (e.g., :hello)")

                Picker("Source File", selection: $sourceFile) {
                    ForEach(availableFiles, id: \.self) { file in
                        Text(file).tag(file)
                    }
                }
                .help("The YAML file where this match is stored.")

                Section("Replacement") {
                    TextEditor(text: $replace)
                        .font(.system(.body, design: .monospaced))
                        .frame(minHeight: 80)
                        .help("The text to insert. Use {{clipboard}} and {{date:format}} for variables.")
                }

                Toggle("Enabled", isOn: $isEnabled)
            }
            .formStyle(.grouped)

            HStack {
                Button("Cancel", role: .cancel) {
                    onCancel()
                }
                .keyboardShortcut(.cancelAction)

                Spacer()

                Button("Save") {
                    let match = Match(
                        id: existingId ?? UUID(),
                        trigger: trigger,
                        replace: replace,
                        isEnabled: isEnabled,
                        sourceFile: sourceFile
                    )
                    onSave(match)
                }
                .keyboardShortcut(.defaultAction)
                .disabled(trigger.isEmpty || replace.isEmpty)
            }
            .padding(.horizontal)
        }
        .padding()
        .frame(minWidth: 400, minHeight: 300)
    }
}
