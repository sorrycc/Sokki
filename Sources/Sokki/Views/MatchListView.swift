import SwiftUI

struct MatchListView: View {
    @Bindable var store: MatchStore
    @State private var selectedMatch: Match?
    @State private var sheetRequest: EditorRequest?
    @State private var pendingDeletion: Match?
    @State private var fileFilter: String?
    @State private var searchQuery: String = ""
    @State private var lastClickTime: Date?
    @State private var lastClickedId: UUID?
    @FocusState private var focus: Focus?

    private static let doubleClickInterval: TimeInterval = 0.5

    private enum Focus: Hashable { case list, search }

    private enum EditorRequest: Identifiable, Equatable {
        case edit(Match)
        case new(prefillSourceFile: String, initialTrigger: String? = nil)

        var id: String {
            switch self {
            case .edit(let m):                  return "edit-\(m.id.uuidString)"
            case .new(let f, let t):            return "new-\(f)-\(t ?? "")"
            }
        }
    }

    var body: some View {
        content
            .onChange(of: fileFilter) { _, _ in pruneSelection() }
            .onChange(of: searchQuery) { _, _ in pruneSelection() }
            .onChange(of: store.matches) { _, _ in pruneSelection() }
            .onChange(of: sheetRequest) { _, request in
                if request == nil {
                    lastClickTime = nil
                    lastClickedId = nil
                }
            }
            .onReceive(NotificationCenter.default.publisher(for: .openMatchEditor)) { note in
                let trigger = (note.userInfo?["trigger"] as? String) ?? ""
                guard !trigger.isEmpty else { return }
                sheetRequest = .new(prefillSourceFile: resolvedPrefill(), initialTrigger: trigger)
            }
            .sheet(item: $sheetRequest, content: sheetContent)
            .alert(
                "Delete Match?",
                isPresented: deletionAlertBinding,
                presenting: pendingDeletion,
                actions: deletionAlertActions,
                message: deletionAlertMessage
            )
    }

    @ViewBuilder
    private var content: some View {
        VStack(spacing: 0) {
            header

            if filteredMatches.isEmpty {
                ContentUnavailableView(
                    "No matches",
                    systemImage: "line.3.horizontal.decrease.circle",
                    description: Text(emptyStateDescription)
                )
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                listBody
            }

            footerToolbar
        }
    }

    @ViewBuilder
    private func sheetContent(for req: EditorRequest) -> some View {
        switch req {
        case .edit(let m):
            MatchEditorView(
                match: m,
                availableFiles: store.availableSourceFiles,
                prefillSourceFile: m.sourceFile ?? "default.yml",
                onSave: { updated in
                    store.update(updated)
                    sheetRequest = nil
                },
                onCancel: { sheetRequest = nil }
            )
        case .new(let prefill, let initial):
            MatchEditorView(
                match: nil,
                availableFiles: store.availableSourceFiles,
                prefillSourceFile: prefill,
                initialTrigger: initial,
                onSave: { m in
                    store.add(m)
                    sheetRequest = nil
                },
                onCancel: { sheetRequest = nil }
            )
        }
    }

    private var deletionAlertBinding: Binding<Bool> {
        Binding(
            get: { pendingDeletion != nil },
            set: { if !$0 { pendingDeletion = nil } }
        )
    }

    @ViewBuilder
    private func deletionAlertActions(for match: Match) -> some View {
        Button("Cancel", role: .cancel) { }
        Button("Delete", role: .destructive) {
            guard store.matches.contains(where: { $0.id == match.id }) else { return }
            store.remove(match)
            if selectedMatch?.id == match.id {
                selectedMatch = nil
            }
        }
    }

    private func deletionAlertMessage(for match: Match) -> Text {
        Text("The match \"\(match.trigger)\" will be permanently deleted. This action cannot be undone.")
    }

    private var header: some View {
        HStack(spacing: 8) {
            Picker(selection: $fileFilter) {
                Text("All Files").tag(nil as String?)
                ForEach(store.availableSourceFiles, id: \.self) { f in
                    Text(f).tag(Optional(f))
                }
            } label: {
                Label(fileFilter ?? "All Files", systemImage: "doc")
            }
            .pickerStyle(.menu)
            .fixedSize()

            HStack(spacing: 4) {
                Image(systemName: "magnifyingglass")
                    .foregroundStyle(.secondary)
                TextField("Search", text: $searchQuery)
                    .textFieldStyle(.roundedBorder)
                    .focused($focus, equals: .search)
            }
        }
        .padding(.horizontal, 8)
        .padding(.vertical, 6)
    }

    private var listBody: some View {
        List(selection: $selectedMatch) {
            ForEach(filteredMatches) { match in
                MatchRow(match: match)
                    .tag(match)
                    .contentShape(Rectangle())
                    .onTapGesture {
                        handleTap(match)
                    }
                    .contextMenu {
                        Button("Edit") {
                            openEditor(for: match)
                        }
                        Button("Delete", role: .destructive) {
                            pendingDeletion = match
                        }
                        Divider()
                        Button(match.isEnabled ? "Disable" : "Enable") {
                            var updated = match
                            updated.isEnabled.toggle()
                            store.update(updated)
                        }
                    }
            }
            .onDelete { offsets in
                let targets = offsets.map { filteredMatches[$0] }
                for match in targets { store.remove(match) }
            }
        }
        .listStyle(.inset(alternatesRowBackgrounds: true))
        .focused($focus, equals: .list)
        .onKeyPress(.return) {
            guard focus == .list, let selected = selectedMatch else { return .ignored }
            openEditor(for: selected)
            return .handled
        }
        .onKeyPress(.delete) {
            guard focus == .list else { return .ignored }
            return requestDeleteSelected()
        }
        .onKeyPress(.deleteForward) {
            guard focus == .list else { return .ignored }
            return requestDeleteSelected()
        }
    }

    private var footerToolbar: some View {
        HStack {
            Button(action: {
                sheetRequest = .new(prefillSourceFile: resolvedPrefill())
            }) {
                Image(systemName: "plus")
                    .frame(width: 22, height: 22)
            }
            .help("Add match")

            Button(action: {
                if let match = selectedMatch {
                    pendingDeletion = match
                }
            }) {
                Image(systemName: "minus")
                    .frame(width: 22, height: 22)
            }
            .disabled(selectedMatch == nil)
            .help("Remove match")

            Spacer()

            if let error = store.lastError {
                Label(error, systemImage: "exclamationmark.triangle.fill")
                    .foregroundStyle(.yellow)
                    .font(.caption)
                    .lineLimit(1)
            }
        }
        .padding(8)
    }

    private var filteredMatches: [Match] {
        let q = searchQuery.trimmingCharacters(in: .whitespaces)
        return store.matches.filter { m in
            let fileOK = fileFilter.map { m.sourceFile == $0 } ?? true
            let searchOK = q.isEmpty
                || m.trigger.localizedCaseInsensitiveContains(q)
                || m.replace.localizedCaseInsensitiveContains(q)
            return fileOK && searchOK
        }
    }

    private var emptyStateDescription: String {
        if !searchQuery.trimmingCharacters(in: .whitespaces).isEmpty {
            return "Try a different search or clear the filter."
        }
        if fileFilter != nil {
            return "No matches in this file yet."
        }
        return "Add a match with the + button below."
    }

    private func resolvedPrefill() -> String {
        fileFilter ?? "default.yml"
    }

    private func pruneSelection() {
        guard let sel = selectedMatch else { return }
        if !filteredMatches.contains(sel) { selectedMatch = nil }
    }

    private func requestDeleteSelected() -> KeyPress.Result {
        guard let match = selectedMatch else { return .ignored }
        pendingDeletion = match
        return .handled
    }

    private func handleTap(_ match: Match) {
        let now = Date()
        if lastClickedId == match.id,
           let last = lastClickTime,
           now.timeIntervalSince(last) < Self.doubleClickInterval {
            openEditor(for: match)
            lastClickTime = nil
            lastClickedId = nil
        } else {
            selectedMatch = match
            lastClickTime = now
            lastClickedId = match.id
        }
    }

    private func openEditor(for match: Match) {
        let target = store.matches.first(where: { $0.id == match.id }) ?? match
        sheetRequest = .edit(target)
    }
}

struct MatchRow: View {
    let match: Match

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 2) {
                Text(match.trigger)
                    .font(.system(.body, design: .monospaced))
                    .foregroundStyle(match.isEnabled ? .primary : .secondary)

                Text(match.replace)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            .layoutPriority(1)

            Spacer(minLength: 8)

            if let file = match.sourceFile, !file.isEmpty {
                Text(file)
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
                    .truncationMode(.middle)
            }

            if !match.isEnabled {
                Text("Disabled")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 2)
                    .background(.quaternary)
                    .clipShape(Capsule())
            }
        }
        .padding(.vertical, 2)
    }
}
