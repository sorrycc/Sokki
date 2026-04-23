import SwiftUI

struct PaletteView: View {
    @Bindable var matchStore: MatchStore
    let recency: RecencyStore
    let renderer: VariableRenderer
    let clipboardSnapshot: String?
    let onCommit: (Match) -> Void
    let onEdit: (Match) -> Void
    let onClose: () -> Void

    @State private var query: String = ""
    @State private var selection: UUID?
    @FocusState private var searchFocused: Bool

    var body: some View {
        VStack(spacing: 0) {
            TextField("Search…", text: $query)
                .textFieldStyle(.plain)
                .font(.system(size: 16))
                .padding(.horizontal, 14)
                .padding(.vertical, 12)
                .focused($searchFocused)
                .onKeyPress(.upArrow) { moveSelection(-1); return .handled }
                .onKeyPress(.downArrow) { moveSelection(+1); return .handled }
                .onKeyPress(keys: [.return]) { press in
                    guard let m = selectedMatch else { return .ignored }
                    if press.modifiers.contains(.command) {
                        onEdit(m)
                    } else {
                        onCommit(m)
                    }
                    return .handled
                }
            Divider()
            HStack(spacing: 0) {
                resultsList
                    .frame(width: 280)
                Divider()
                previewPane
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .frame(width: 720, height: 420)
        .background(.thinMaterial)
        .clipShape(RoundedRectangle(cornerRadius: 12))
        .overlay(
            RoundedRectangle(cornerRadius: 12).strokeBorder(.separator, lineWidth: 0.5)
        )
        .onKeyPress(.escape) { onClose(); return .handled }
        .onAppear {
            Logger.shared.debug("[Palette] view onAppear — searchFocused(before)=\(searchFocused)")
            selection = filtered.first?.match.id
            // Defer one runloop turn so the underlying NSHostingView has finished
            // mounting and AppKit's first-responder chain is established before
            // SwiftUI tries to claim focus.
            DispatchQueue.main.async {
                searchFocused = true
                Logger.shared.debug("[Palette] view +async — searchFocused(after assign)=\(searchFocused)")
            }
        }
        .onChange(of: searchFocused) { old, new in
            Logger.shared.debug("[Palette] searchFocused changed \(old) -> \(new)")
        }
        .onChange(of: query) { _, new in
            Logger.shared.debug("[Palette] query changed -> '\(new)'")
            selection = filtered.first?.match.id
        }
    }

    // MARK: - Subviews

    private var resultsList: some View {
        ScrollViewReader { proxy in
            List(filtered, id: \.match.id, selection: $selection) { entry in
                paletteRow(entry.match)
                    .tag(entry.match.id)
                    .contentShape(Rectangle())
                    .onTapGesture { onCommit(entry.match) }
                    .id(entry.match.id)
            }
            .listStyle(.sidebar)
            .scrollContentBackground(.hidden)
            .onChange(of: selection) { _, newID in
                if let newID { proxy.scrollTo(newID, anchor: .center) }
            }
        }
    }

    private func paletteRow(_ match: Match) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            HStack(spacing: 6) {
                Text(match.label?.isEmpty == false ? match.label! : match.trigger)
                    .font(.system(.body, design: match.label?.isEmpty == false ? .default : .monospaced))
                    .lineLimit(1)
                if let label = match.label, !label.isEmpty {
                    Text(match.trigger)
                        .font(.system(.caption, design: .monospaced))
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }
                if match.isAI {
                    Text("AI")
                        .font(.caption.bold())
                        .foregroundStyle(.white)
                        .padding(.horizontal, 6).padding(.vertical, 1)
                        .background(Color.blue.opacity(0.7), in: Capsule())
                }
            }
            Text(previewLine(match))
                .font(.caption)
                .foregroundStyle(.secondary)
                .lineLimit(1)
        }
        .padding(.vertical, 2)
    }

    private func previewLine(_ match: Match) -> String {
        if let prompt = match.prompt {
            return firstLine(renderPlaceholdersAsGlyphs(prompt))
        }
        return firstLine(match.replace)
    }

    private func aiPreview(_ prompt: String) -> some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                labelledBlock("PROMPT", prompt)
                Text("Runs Claude (\(matchStore.config.ai.model)) via the local `claude` CLI. `{{selection}}` / `{{clipboard}}` placeholders are filled at run time. Result replaces the placeholder “\(PaletteView.placeholderText)”.")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .textSelection(.enabled)
            .padding(14)
        }
    }

    private static let placeholderText = "⟨generating…⟩"

    private func labelledBlock(_ label: String, _ body: String) -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(label)
                .font(.caption.bold())
                .foregroundStyle(.secondary)
            Text(body)
                .font(.system(.body, design: .monospaced))
                .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    /// Render `{{selection}}` / `{{s|c}}` as `⟨selection⟩` / `⟨s|c⟩` for
    /// compact list subtitles. Uses `PromptTemplate.parse` when possible;
    /// falls back to the raw prompt on parse failure (defensive — the match
    /// decoder should have rejected any unparseable prompt).
    private func renderPlaceholdersAsGlyphs(_ prompt: String) -> String {
        guard let tpl = try? PromptTemplate.parse(prompt) else { return prompt }
        let chars = Array(prompt)
        var out = ""
        var cursor = 0
        for ph in tpl.placeholders {
            if cursor < ph.location {
                out.append(String(chars[cursor..<ph.location]))
            }
            out.append("⟨\(ph.innerRaw.trimmingCharacters(in: .whitespaces))⟩")
            cursor = ph.location + ph.length
        }
        if cursor < chars.count {
            out.append(String(chars[cursor..<chars.count]))
        }
        return out
    }

    private var previewPane: some View {
        Group {
            if let m = selectedMatch {
                if let prompt = m.prompt {
                    aiPreview(prompt)
                } else {
                    ScrollView {
                        Text(renderer.renderPreview(m.replace, clipboardSnapshot: clipboardSnapshot))
                            .font(.system(.body, design: .monospaced))
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .textSelection(.enabled)
                            .padding(14)
                    }
                }
            } else {
                VStack {
                    Spacer()
                    Text(matchStore.matches.isEmpty ? "No matches" : "No results")
                        .foregroundStyle(.secondary)
                    Spacer()
                }
                .frame(maxWidth: .infinity)
            }
        }
    }

    // MARK: - Filtering & ranking

    private struct Entry {
        let match: Match
        let score: Int
    }

    private var filtered: [Entry] {
        let enabled = matchStore.matches.filter { $0.isEnabled }
        let snapshot = recency.snapshot()
        let q = query.trimmingCharacters(in: .whitespaces)

        let scored: [Entry] = enabled.compactMap { m in
            guard let s = matchScore(query: q, match: m) else { return nil }
            return Entry(match: m, score: s)
        }
        return scored.sorted { lhs, rhs in
            if lhs.score != rhs.score { return lhs.score > rhs.score }
            let l = snapshot[lhs.match.trigger] ?? .distantPast
            let r = snapshot[rhs.match.trigger] ?? .distantPast
            if l != r { return l > r }
            return lhs.match.trigger < rhs.match.trigger
        }
    }

    private var selectedMatch: Match? {
        guard let id = selection else { return filtered.first?.match }
        return filtered.first(where: { $0.match.id == id })?.match
            ?? filtered.first?.match
    }

    private func moveSelection(_ delta: Int) {
        let items = filtered
        guard !items.isEmpty else { return }
        let currentIndex = items.firstIndex(where: { $0.match.id == selection }) ?? 0
        let next = max(0, min(items.count - 1, currentIndex + delta))
        selection = items[next].match.id
    }

    private func firstLine(_ s: String) -> String {
        s.split(whereSeparator: { $0.isNewline }).first.map(String.init) ?? ""
    }

    // MARK: - Scoring

    private func matchScore(query: String, match m: Match) -> Int? {
        if query.isEmpty { return 0 }
        let triggerScore = FuzzyScorer.score(query: query, target: m.trigger)
        let labelScore = m.label.flatMap {
            FuzzyScorer.score(query: query, target: $0).map { $0 + 2 }
        }
        let replaceHead = String(m.replace.prefix(200))
        let replaceScore = FuzzyScorer.score(query: query, target: replaceHead)
            .map { max(0, $0 - 2) }
        return [triggerScore, labelScore, replaceScore].compactMap { $0 }.max()
    }
}

// MARK: - Fuzzy scorer

enum FuzzyScorer {
    static func score(query: String, target: String) -> Int? {
        if query.isEmpty { return 0 }
        let q = Array(query.lowercased())
        let t = Array(target.lowercased())
        var qi = 0
        var ti = 0
        var score = 0
        var run = 0
        var firstHit: Int?
        while qi < q.count, ti < t.count {
            if q[qi] == t[ti] {
                if firstHit == nil { firstHit = ti }
                run += 1
                score += 1 + run
                qi += 1
            } else {
                run = 0
            }
            ti += 1
        }
        guard qi == q.count else { return nil }
        if firstHit == 0 { score += 5 }
        return score
    }
}
