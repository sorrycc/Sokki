import AppKit

struct PasteboardItemSnapshot: Sendable {
    let types: [NSPasteboard.PasteboardType]
    let data: [NSPasteboard.PasteboardType: Data]
}

struct PasteboardSnapshot: Sendable {
    let items: [PasteboardItemSnapshot]
    let changeCount: Int
}

@MainActor
final class ClipboardManager {
    private var snapshot: PasteboardSnapshot?

    func save() {
        let pasteboard = NSPasteboard.general
        let changeCount = pasteboard.changeCount
        var items: [PasteboardItemSnapshot] = []

        if let pbItems = pasteboard.pasteboardItems {
            for item in pbItems {
                let types = item.types
                var dataMap: [NSPasteboard.PasteboardType: Data] = [:]
                for type in types {
                    if let data = item.data(forType: type) {
                        dataMap[type] = data
                    }
                }
                items.append(PasteboardItemSnapshot(types: types, data: dataMap))
            }
        }

        self.snapshot = PasteboardSnapshot(items: items, changeCount: changeCount)
    }

    func restore() {
        guard let snapshot = self.snapshot else { return }
        self.snapshot = nil

        let pasteboard = NSPasteboard.general

        // If clipboard was modified externally since snapshot, don't overwrite
        if pasteboard.changeCount != snapshot.changeCount + 1 {
            // changeCount incremented by 1 from our own setString; anything else means external change
            return
        }

        pasteboard.clearContents()

        for itemSnapshot in snapshot.items {
            let pbItem = NSPasteboardItem()
            for type in itemSnapshot.types {
                if let data = itemSnapshot.data[type] {
                    pbItem.setData(data, forType: type)
                }
            }
            pasteboard.writeObjects([pbItem])
        }
    }

    func setString(_ string: String) {
        let pasteboard = NSPasteboard.general
        pasteboard.clearContents()
        pasteboard.setString(string, forType: .string)
    }
}
