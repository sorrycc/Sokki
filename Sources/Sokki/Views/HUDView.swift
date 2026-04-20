import SwiftUI

struct HUDView: View {
    let store: HUDStore
    let onCreateMatch: (String) -> Void
    @State private var revealed: Set<UUID> = []

    var body: some View {
        Group {
            if store.events.isEmpty {
                ContentUnavailableView(
                    "No recent activity",
                    systemImage: "clock.arrow.circlepath",
                    description: Text("Type in another app — matches and blocks appear here.")
                )
            } else {
                List(store.events) { ev in
                    HUDRow(
                        event: ev,
                        isRevealed: revealed.contains(ev.id),
                        onToggleReveal: { toggle(ev.id) },
                        onCreateMatch: { onCreateMatch(ev.capturedBuffer) }
                    )
                }
                .listStyle(.inset(alternatesRowBackgrounds: true))
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .frame(minWidth: 380, minHeight: 420)
    }

    private func toggle(_ id: UUID) {
        if revealed.contains(id) { revealed.remove(id) } else { revealed.insert(id) }
    }
}

private struct HUDRow: View {
    let event: HUDEvent
    let isRevealed: Bool
    let onToggleReveal: () -> Void
    let onCreateMatch: () -> Void

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            VerdictChip(verdict: event.verdict)

            VStack(alignment: .leading, spacing: 2) {
                // For .matched rows, capturedBuffer == matchedTrigger; show the trigger
                // once as primary content rather than duplicating it on both lines.
                if event.verdict != .matched {
                    Text(renderedBuffer)
                        .font(.system(.body, design: .monospaced))
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
                if let t = event.matchedTrigger {
                    Text(event.verdict == .matched ? t : "→ \(t)")
                        .font(event.verdict == .matched ? .system(.body, design: .monospaced) : .caption)
                        .foregroundStyle(event.verdict == .matched ? .primary : .secondary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
            }
            Spacer(minLength: 8)
            Text(Self.timeFormatter.string(from: event.timestamp))
                .font(.caption.monospacedDigit())
                .foregroundStyle(.secondary)
        }
        .padding(.vertical, 2)
        .contentShape(Rectangle())
        .onTapGesture { onToggleReveal() }
        .contextMenu {
            Button("Create match from this") { onCreateMatch() }
            Button("Copy") { copy() }
        }
    }

    private var renderedBuffer: String {
        if isRevealed { return event.capturedBuffer }
        return String(event.capturedBuffer.map { $0.isWhitespace ? $0 : "•" })
    }

    private func copy() {
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(event.capturedBuffer, forType: .string)
    }

    private static let timeFormatter: DateFormatter = {
        let f = DateFormatter()
        f.dateFormat = "HH:mm:ss"
        return f
    }()
}

private struct VerdictChip: View {
    let verdict: HUDEvent.Verdict

    var body: some View {
        Text(label)
            .font(.caption).bold()
            .foregroundStyle(.primary)
            .padding(.horizontal, 6).padding(.vertical, 2)
            .background(tint.opacity(0.25), in: Capsule())
    }

    private var label: String {
        switch verdict {
        case .matched:  return "Matched"
        case .tooSoon:  return "Too soon"
        case .disabled: return "Disabled"
        }
    }

    private var tint: Color {
        switch verdict {
        case .matched:  return .green
        case .tooSoon:  return .orange
        case .disabled: return .secondary
        }
    }
}
