# Deep Native-macOS Platform Integration

## Idea 1: On-Device Apple Intelligence Smart Snippets

**Pitch:** Let a snippet's body be a natural-language prompt that expands through Apple's on-device Foundation Models LLM — `;reply` becomes "draft a polite decline of the currently-selected email," rewritten in the user's tone, fully offline, zero API keys. This is the single highest-leverage feature macOS now offers that no cross-platform expander can touch.

**How it works:**
- New match `type: llm` in YAML; body is the prompt, optional `tone`/`length`/`schema` fields.
- Call `FoundationModels.LanguageModelSession` (macOS 26 Tahoe SDK) with `@Generable` Swift structs so the model returns typed output — no regex parsing of LLM slop.
- Pull context into the prompt from `NSPasteboard.general`, the frontmost app via `NSWorkspace.shared.frontmostApplication`, and selected text via an AX `kAXSelectedTextAttribute` read on `kAXFocusedUIElementAttribute`.
- Stream tokens directly into the focused field through the existing `TextInjector` CGEvent pipeline; show an `NSPopover` anchored to the caret with a "Replace / Try Again / Cancel" affordance using `AXUIElementCopyParameterizedAttributeValue` + `kAXBoundsForRangeParameterizedAttribute` for caret rect.
- Gate behind a `SystemLanguageModel.availability` check so it degrades to plain expansion on unsupported hardware.

**Why it's accretive:** Espresso/TextExpander/Raycast either ship no LLM or route every keystroke to OpenAI — slow, paid, privacy-hostile, and blocked on corporate Macs. FoundationModels runs on-device, free, offline, and is usable inside Mail/Messages/Xcode without network egress. Apple ships the weights; we ship the glue.

**Risk / cost:** Requires macOS 26 + Apple Silicon for the good path. Latency ~200-500ms for short outputs — need streaming UI. Must design prompts so a 3B on-device model doesn't hallucinate; use `@Generable` guided decoding aggressively.

---

## Idea 2: Screen-OCR Variables via Vision + ScreenCaptureKit

**Pitch:** A new variable type `{{screen:order_number}}` lets a snippet capture a region of the screen, OCR it, and splice a named field into the expansion — "type my order number from the confirmation page I'm looking at." Works across any app, including web apps, PDFs, and screenshares where copy-paste is disabled.

**How it works:**
- On trigger, use `SCShareableContent` + `SCStreamConfiguration` (ScreenCaptureKit) to grab either the frontmost window or a user-drawn rect stored per-match.
- Run `VNRecognizeTextRequest` (Vision, `.accurate` revision 3) with `recognitionLanguages` auto-derived from user locale.
- Map OCR'd lines to named variables via regex in the match YAML (`pattern: "Order #(\\w+)"` → `$1`).
- Use `SCContentSharingPicker` (macOS 14+) so the user picks a region once per match — Apple's system picker, no custom overlay, no extra TCC prompt.
- Cache the last capture 2s so chained snippets reuse the same frame without re-prompting.

**Why it's accretive:** Electron apps can't access ScreenCaptureKit or Vision — they'd shell out to `screencapture` + ship a Tesseract WASM blob (slow, bad Chinese/Japanese accuracy). Vision's on-device OCR handles 20+ languages at native speed with the Neural Engine. And ScreenCaptureKit is the only path Apple will support going forward — legacy `CGWindowListCreateImage` is deprecated in Tahoe.

**Risk / cost:** Second TCC permission (Screen Recording) — friction. Need a clean onboarding flow that explains *why*. UI complexity for the region picker is moderate but `SCContentSharingPicker` does most of it.

---

## Idea 3: App Intents + Shortcuts + Spotlight Surfacing

**Pitch:** Expose every snippet as an `AppIntent` so Shortcuts, Spotlight, Siri, and the macOS 15+ Action Button can fire expansions without typing a trigger. "Hey Siri, insert my standup update" → Sokki types it into whatever field has focus. Your snippet library becomes the OS's snippet library.

**How it works:**
- `ExpandSnippetIntent: AppIntent` with `@Parameter` for snippet name (dynamic `AppEnum` refreshed from `MatchStore`).
- `AppShortcutsProvider` registers phrases like "Insert (.$snippet) with Sokki" — appears in Shortcuts.app, Spotlight, and Siri automatically.
- For the `perform()` body, reuse the existing `TextInjector` but resolve the target: if an accessibility-focused text field exists via `AXUIElementCreateSystemWide` + `kAXFocusedUIElementAttribute`, inject there; otherwise return an `IntentResult` with `.dialog` + `.value` so it pipes into another Shortcut.
- Add `ProvidesDialog` + `ShowsSnippetView` so Spotlight previews the expanded text inline.
- Register a `FocusFilter` intent so users can say "in Work Focus, only enable the #work snippet category" — Apple's system handles the on/off wiring.

**Why it's accretive:** App Intents is a private-to-Apple ABI — Electron literally cannot register there. This turns Sokki from "keyboard thing" into a first-class macOS automation primitive usable from `osascript`, Shortcuts, Stream Deck, Keyboard Maestro, and Raycast (which can execute Shortcuts but not define App Intents of its own quality). Focus-filter integration is something even Alfred doesn't do properly.

**Risk / cost:** Low. Mostly declarative Swift, maybe 300 LOC. Must keep the dynamic enum cheap — refresh on `.matchesDidChange`. Siri invocation needs a short distinct app name ("Sokki" is already good).

---

## Idea 4: True Rich-Text Insertion via NSTextInputClient Probing

**Pitch:** Replace the current keystroke-simulation/clipboard approach with real rich-text insertion where the target supports it — preserving formatting, attachments, links, and inline images in Mail/Notes/Pages/TextEdit/Xcode comments. Every other expander on macOS fakes it with a pasteboard swap that flashes the clipboard and loses attributed runs.

**How it works:**
- Probe the focused AX element: `AXUIElementCopyAttributeNames` — if it claims `kAXInsertionPointLineNumberAttribute` + `kAXSelectedTextRangeAttribute`, it's an AppKit `NSTextView`-backed field.
- For those, walk `NSApp.runningApplications` + use the AX hierarchy to find the live `NSTextView` isn't possible cross-process — but we *can* set `kAXSelectedTextAttribute` to an attributed-string-serialized RTFD blob, which Cocoa text fields honor.
- For non-AppKit targets (Electron, web), fall back to the current keystroke injector, but detect the case first via bundle ID on `NSWorkspace.shared.frontmostApplication` (per-app behavior table).
- For Mail specifically, register a Mail Extension (`MEComposeSessionHandler`) so snippets with HTML bodies insert as real rich email, not plaintext.
- Use `NSServicesProvider` registration so snippets also appear under Services menu system-wide — another surface Electron can't touch.

**Why it's accretive:** Cross-platform expanders are stuck with the `Cmd+V` clipboard hack because they have no AXUIElement access on macOS without native code, and they certainly don't ship a Mail Extension. This unlocks real formatted signatures, inline screenshots in snippets, and code blocks that keep syntax highlighting in Xcode comments — table-stakes features that no expander gets right on macOS.

**Risk / cost:** Medium-high. AX rich-text insertion is brittle across app versions; need a compatibility matrix + graceful fallback. Mail Extension requires a separate `.appex` target, entitlement, and notarization — one-time structural work. Worth it because it converts Sokki from "keystroke robot" into "native text participant."
