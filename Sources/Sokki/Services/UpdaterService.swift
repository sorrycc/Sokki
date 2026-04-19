import Sparkle
import os

private let osLog = os.Logger(subsystem: "com.sokki.Sokki", category: "updater")

final class SparkleDelegate: NSObject, SPUUpdaterDelegate {
    func updater(_ updater: SPUUpdater, didAbortWithError error: Error) {
        let msg = "Update aborted: \(error.localizedDescription)"
        osLog.error("\(msg, privacy: .public)")
        Logger.shared.error(msg)
    }

    func updater(
        _ updater: SPUUpdater,
        didFinishUpdateCycleFor updateCheck: SPUUpdateCheck,
        error: Error?
    ) {
        guard let error else { return }
        let msg = "Update cycle error (\(updateCheck.rawValue)): \(error.localizedDescription)"
        osLog.error("\(msg, privacy: .public)")
        Logger.shared.error(msg)
    }
}

@MainActor
final class UpdaterService {
    private let delegate = SparkleDelegate()
    private let controller: SPUStandardUpdaterController

    init() {
        controller = SPUStandardUpdaterController(
            startingUpdater: true,
            updaterDelegate: delegate,
            userDriverDelegate: nil
        )
    }

    func checkForUpdates() {
        controller.checkForUpdates(nil)
    }
}
