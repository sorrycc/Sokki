import SwiftUI

struct MatchListView: View {
    @Bindable var store: MatchStore
    @State private var selectedMatch: Match?
    @State private var showingEditor = false
    @State private var editingMatch: Match?

    var body: some View {
        VStack(spacing: 0) {
            List(selection: $selectedMatch) {
                ForEach(store.matches) { match in
                    MatchRow(match: match)
                        .tag(match)
                        .contextMenu {
                            Button("Edit") {
                                editingMatch = match
                                showingEditor = true
                            }
                            Button("Delete", role: .destructive) {
                                store.remove(match)
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

            HStack {
                Button(action: {
                    editingMatch = nil
                    showingEditor = true
                }) {
                    Image(systemName: "plus")
                }
                .help("Add match")

                Button(action: {
                    if let match = selectedMatch {
                        store.remove(match)
                        selectedMatch = nil
                    }
                }) {
                    Image(systemName: "minus")
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
