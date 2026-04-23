import SwiftUI

struct MatchEditorView: View {
    @State private var trigger: String
    @State private var replace: String
    @State private var label: String
    @State private var isEnabled: Bool
    @State private var sourceFile: String
    @State private var isAddingNewFile = false
    @State private var newFileDraft = ""
    @State private var extraFiles: [String] = []
    @FocusState private var newFileFieldFocused: Bool
    private let existingId: UUID?
    private let availableFiles: [String]

    private var displayedFiles: [String] {
        var seen = Set<String>()
        return (availableFiles + extraFiles).filter { seen.insert($0).inserted }
    }

    let onSave: (Match) -> Void
    let onCancel: () -> Void

    init(
        match: Match? = nil,
        availableFiles: [String],
        prefillSourceFile: String,
        initialTrigger: String? = nil,
        onSave: @escaping (Match) -> Void,
        onCancel: @escaping () -> Void
    ) {
        self._trigger = State(initialValue: match?.trigger ?? initialTrigger ?? ":")
        self._replace = State(initialValue: match?.replace ?? "")
        self._label = State(initialValue: match?.label ?? "")
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
                TextField("Label (optional)", text: $label)
                    .help("Friendly name shown in the palette. Leave blank to fall back to the trigger.")

                TextField("Trigger", text: $trigger)
                    .font(.system(.body, design: .monospaced))
                    .help("The keyword that triggers expansion (e.g., :hello)")

                if isAddingNewFile {
                    newFileRow
                } else {
                    HStack(spacing: 4) {
                        Picker("Source File", selection: $sourceFile) {
                            ForEach(displayedFiles, id: \.self) { file in
                                Text(file).tag(file)
                            }
                        }
                        .help("The YAML file where this match is stored.")

                        Button {
                            newFileDraft = ""
                            isAddingNewFile = true
                        } label: {
                            Image(systemName: "plus.circle")
                        }
                        .buttonStyle(.borderless)
                        .help("Create a new source file")
                    }
                }

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
                    let trimmedLabel = label.trimmingCharacters(in: .whitespaces)
                    let match = Match(
                        id: existingId ?? UUID(),
                        trigger: trigger,
                        replace: replace,
                        label: trimmedLabel.isEmpty ? nil : trimmedLabel,
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

    @ViewBuilder
    private var newFileRow: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 4) {
                TextField("filename.yml", text: $newFileDraft)
                    .textFieldStyle(.roundedBorder)
                    .focused($newFileFieldFocused)
                    .onSubmit { commitNewFile() }
                    .onExitCommand { cancelNewFile() }

                Button("Create") { commitNewFile() }
                    .disabled(normalizedFilename(newFileDraft) == nil)
                    .keyboardShortcut(.defaultAction)

                Button("Cancel", role: .cancel) { cancelNewFile() }
            }
            if let existing = matchingExistingFile(newFileDraft) {
                Text("Already exists — will use \(existing)")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
        .task { newFileFieldFocused = true }
    }

    private func normalizedFilename(_ raw: String) -> String? {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty,
              !trimmed.hasPrefix("."),
              !trimmed.contains("/"),
              !trimmed.contains("\\"),
              trimmed.count <= 255
        else { return nil }
        let lower = trimmed.lowercased()
        if lower.hasSuffix(".yml") || lower.hasSuffix(".yaml") { return trimmed }
        return trimmed + ".yml"
    }

    private func matchingExistingFile(_ raw: String) -> String? {
        guard let name = normalizedFilename(raw) else { return nil }
        func stem(_ s: String) -> String {
            let lower = s.lowercased()
            if lower.hasSuffix(".yml")  { return String(lower.dropLast(4)) }
            if lower.hasSuffix(".yaml") { return String(lower.dropLast(5)) }
            return lower
        }
        let target = stem(name)
        return displayedFiles.first { stem($0) == target }
    }

    private func commitNewFile() {
        guard let name = normalizedFilename(newFileDraft) else { return }
        let chosen = matchingExistingFile(newFileDraft) ?? name
        if !displayedFiles.contains(chosen) { extraFiles.append(chosen) }
        sourceFile = chosen
        isAddingNewFile = false
        newFileDraft = ""
    }

    private func cancelNewFile() {
        isAddingNewFile = false
        newFileDraft = ""
    }
}
