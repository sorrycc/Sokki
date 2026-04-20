import SwiftUI

struct MatchListView: View {
    @Bindable var store: MatchStore
    @State private var selectedMatch: Match?
    @State private var showingEditor = false
    @State private var editingMatch: Match?
    @State private var lastClickTime: Date?
    @State private var lastClickedId: UUID?
    @State private var pendingDeletion: Match?

    private static let doubleClickInterval: TimeInterval = 0.5

    var body: some View {
        VStack(spacing: 0) {
            List(selection: $selectedMatch) {
                ForEach(store.matches) { match in
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
                    store.remove(at: offsets)
                }
            }
            .listStyle(.inset(alternatesRowBackgrounds: true))
            .onKeyPress(.return) {
                guard let selected = selectedMatch else { return .ignored }
                openEditor(for: selected)
                return .handled
            }

            HStack {
                Button(action: {
                    editingMatch = nil
                    showingEditor = true
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
        .onChange(of: showingEditor) { _, isShowing in
            if !isShowing {
                lastClickTime = nil
                lastClickedId = nil
            }
        }
        .sheet(isPresented: $showingEditor) {
            MatchEditorView(
                match: editingMatch,
                onSave: { match in
                    if editingMatch != nil {
                        store.update(match)
                    } else {
                        store.add(match)
                    }
                    showingEditor = false
                },
                onCancel: {
                    showingEditor = false
                }
            )
        }
        .alert(
            "Delete Match?",
            isPresented: Binding(
                get: { pendingDeletion != nil },
                set: { if !$0 { pendingDeletion = nil } }
            ),
            presenting: pendingDeletion
        ) { match in
            Button("Cancel", role: .cancel) { }
            Button("Delete", role: .destructive) {
                guard store.matches.contains(where: { $0.id == match.id }) else { return }
                store.remove(match)
                if selectedMatch?.id == match.id {
                    selectedMatch = nil
                }
            }
        } message: { match in
            Text("The match \"\(match.trigger)\" will be permanently deleted. This action cannot be undone.")
        }
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
        editingMatch = store.matches.first(where: { $0.id == match.id }) ?? match
        showingEditor = true
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

            Spacer()

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
