import AppKit
import ApplicationServices
import Foundation

/// Polls `AXIsProcessTrusted` when Accessibility is not yet granted at launch,
/// and fires `onGranted` exactly once when the user flips the toggle in System
/// Settings. The event tap is only created during `KeystrokeMonitor.start()`
/// (see `KeystrokeMonitor.runEventTap`), so post-grant recovery requires an
/// app relaunch — hence the "Quit & Reopen" alert wired up by AppDelegate.
@MainActor
final class PermissionMonitor {
    var onGranted: (() -> Void)?

    private var timer: Timer?

    func startIfNeeded() {
        guard !AXIsProcessTrusted() else { return }
        Logger.shared.warning("Accessibility not granted — polling for grant")
        // Timer.scheduledTimer takes a @Sendable closure under Swift 6.
        // Capture self INSIDE MainActor.assumeIsolated — mirrors the
        // `AppDelegate.debouncedReload` pattern.
        timer = Timer.scheduledTimer(withTimeInterval: 2.0, repeats: true) { [weak self] _ in
            MainActor.assumeIsolated {
                guard let self else { return }
                guard AXIsProcessTrusted() else { return }
                self.stop()
                Logger.shared.warning("Accessibility granted — prompting to relaunch")
                self.onGranted?()
            }
        }
    }

    func stop() {
        timer?.invalidate()
        timer = nil
    }
}
