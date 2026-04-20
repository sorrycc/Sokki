import Foundation
import Observation

@Observable
@MainActor
final class HUDStore {
    private(set) var events: [HUDEvent] = []
    static let capacity: Int = 20

    func record(_ event: HUDEvent) {
        events.insert(event, at: 0)
        if events.count > Self.capacity {
            events.removeLast(events.count - Self.capacity)
        }
    }

    func clear() { events.removeAll() }
}
