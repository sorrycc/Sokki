import Foundation

struct RollingBuffer: Sendable {
    private var buffer: [Character]
    private var head: Int = 0
    private var count: Int = 0
    private let capacity: Int

    init(capacity: Int = 128) {
        self.capacity = capacity
        self.buffer = Array(repeating: Character(" "), count: capacity)
    }

    mutating func append(_ char: Character) {
        buffer[(head + count) % capacity] = char
        if count < capacity {
            count += 1
        } else {
            head = (head + 1) % capacity
        }
    }

    mutating func clear() {
        count = 0
        head = 0
    }

    @discardableResult
    mutating func popLast() -> Bool {
        guard count > 0 else { return false }
        count -= 1
        return true
    }

    func tail(_ length: Int) -> String {
        guard length > 0, length <= count else { return "" }
        var result: [Character] = []
        result.reserveCapacity(length)
        let start = (head + count - length) % capacity
        for i in 0..<length {
            result.append(buffer[(start + i) % capacity])
        }
        return String(result)
    }

    func tailMatches(_ trigger: String) -> Bool {
        let triggerCount = trigger.count
        guard triggerCount <= count else { return false }
        let t = tail(triggerCount)
        return t == trigger
    }

    var currentCount: Int { count }

    func toString() -> String { tail(count) }
}
