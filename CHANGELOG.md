# Changelog

All notable changes to Sokki are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Releases follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
The update window shows the section of the version it offers: every release needs a `## <version>` section here.

## Unreleased

- A match's trigger is now optional: a match without one is a prompt, inserted from the palette.
- `⌘N` in the palette opens a **New Prompt** form filled with the selected text, or else the clipboard.
- The New Prompt form saves to `prompts.yml` with `⌘↵`, and `⌘E` moves it to the full editor.
- A new **Save selection as prompt** shortcut in **Settings → Shortcuts** saves selected text in one keystroke.
- **New Prompt from Selection** in the menu bar menu does the same, with **Undo** and **Edit…** after.
- `⌘C` in the palette copies the chosen match with its variables filled in, instead of typing it.
- After `⌘C`, a small banner confirms what was copied.
- Saving a New Prompt marks it **New** in the list, and the palette footer says where it was saved.
- The New Prompt form has **Save** and **Open in Editor** buttons, and counts characters and variables.
- The palette fades in as it opens and slides between search and the New Prompt form.
- With no results, the palette offers **Save as a New Prompt**; with no matches, a **New Prompt** button.
- The capture banner shows the prompt's name first, and a line that runs out until it hides.
- Palette search is faster as you type, especially with many matches.
- Prompts show a **Palette** badge where a trigger would be, and Insights counts their uses.
- **Matches** has an **All · Triggers · Prompts** bar to show only prompts, and **New** then makes one.
- The match editor calls a match without a trigger a prompt, and says how it shows in the palette.
- The menu bar menu shows the palette and capture shortcuts beside their items, as macOS draws them.
- **New Prompt from Selection** is dimmed until Sokki has Accessibility access, which reading it needs.
- The palette, editor, Matches and Insights share one look for rows, headings, icons and motion.
- The Insights empty page shows the trigger to try as a key, and **By App** rows no longer light up on hover.
- **Settings → Insights** charts your expansions of the past 7 days, with today against the average.
- Insights counts the characters your matches saved you and the typing time that comes to.
- Insights ranks your most used triggers and the apps you expand in, and splits typed from palette use.
- Insights lists the matches you haven't used in 7 days; click one to edit it.
- **Settings → Account** signs in to your mon.sorrycc.dev account to sync matches and settings between Macs.
- Synced Macs merge each match on its own, so edits to different matches on two Macs both stay.
- The menu bar menu has **Sign In to Sync…**, and **Sync Now** once signed in.
- **Settings** is redesigned like System Settings, with a sidebar of match files and grouped setting cards.
- New settings for the Recent Activity shortcut and the delay before a shorter trigger expands.
- The palette shows each match's label, trigger and preview, with its keys along the bottom.
- In the palette, `Escape` clears the search before closing.
- The palette shows in bold where the search matched, and when the chosen match was last used.
- The match editor puts trigger and label side by side and previews the replacement with variables filled.
- The match editor warns when another match has the same trigger.
- **Insert Variable** in the match editor puts the variable at the caret instead of the end.
- The match editor asks before throwing away unsaved edits.
- The match editor and Recent Activity have their own title bars, the editor naming the file it saves to.
- Recent Activity says in words what happened to each trigger, names the match, and has a Clear button.
- The menu bar menu shows whether Sokki is on, pauses it, and opens the palette and Recent Activity.
- Reloading, the log and restarting move to **Troubleshooting** in the menu bar menu.
- Typing costs the system less: each key takes about half the time it did in Sokki.
- The matches search also looks in labels.
- **Matches** in Settings shows the chosen match beside the list, with buttons to edit, copy and delete.
- A match's details say how long it is, whether it is typed or pasted, and when it was last used.
- Typing a trigger counts as a use, so the palette ranks recently typed matches first.
- The delays and the paste threshold in **General** show their units.
- Settings and the menu bar menu offer to grant Accessibility access until it is granted.
- Settings' sidebar ends with Sokki's state and a switch to pause it.
- Variables such as `{{clipboard}}` and `{{date:…}}` show in the accent color.
- The welcome on first launch says how to try Sokki and opens Settings.
- Deleting a match no longer asks first, and a toast offers Undo.
- Duplicate a match with `⌘D`, the row's menu, or the button beside the match.
- Copying a replacement says so.
- A new trigger needs at least two characters, so a lone `:` can't expand.
- `⌘N`, `⌘F` and `⌘D` on the Matches page work wherever the focus is, the sidebar too.
- A match file in the sidebar has a menu to add a match to it or show it in Finder.
- A match file's error shows on the Matches page with a button to open the config folder.
- **Settings → Insights** sets each chart on its own card, with the week's dates under the title.
- The daily chart labels today, highlights the day under the pointer, and shows the average once two days have data.
- The Today tile shows in green when today is above the daily average.
- In the first days, Insights says the averages will fill in instead of comparing against one day.
- Insights with nothing yet suggests one of your triggers to try, with a **Show Matches** button.
- Insights opens and redraws faster with thousands of matches, listing up to 48 unused ones.
- The capture and Copied banner now hides a moment after the pointer leaves it.

## 0.1.1

- AI matches are removed, with the AI tab of Settings and the editor's **AI prompt** toggle.
- A match file with a `prompt:` match fails to load until it is deleted or turned into a `replace:`.

## 0.1.0

- Sokki is rewritten in Go, keeping every feature and your matches and settings in `~/.config/sokki`.
- Updates come from GitHub releases, as smaller delta updates, with a new update window.
- Sokki 0.0.16 doesn't update to this version by itself: download it once from the releases page.
- The palette leaves the app you were typing in at the front and types the chosen match into it.
- Recent Activity, Settings and the match editor are redrawn.
- The match editor is a window of its own, opened from Settings, the palette (`⌘↵`) and the HUD.
- `⌘↵` saves the match editor.
- `{{date:…}}` also takes strftime formats such as `%Y-%m-%d`.
- Editing a match in Settings no longer loses the edit when the match files reload.
- Crash reports also cover crashes of the app itself, sent at the next launch.

## 0.0.16 - 2026-06-29

- Fix the config reloading several times a second and filling the log.

## 0.0.15 - 2026-06-29

- Fix Sokki no longer expanding until relaunch after macOS disabled its event tap.
- The log records why each trigger did or didn't expand.
- Keys typed in password fields no longer reach the log.

## 0.0.14 - 2026-04-29

- `⌘,` in the palette brings Settings to the front again.
- A trigger that a longer one extends now fires after a brief pause (`conflict_resolve_delay_ms`, 400 ms).
- Clicks, app switches and opening the palette cancel a pending trigger.

## 0.0.13 - 2026-04-24

- Refresh the Sokki logo.

## 0.0.12 - 2026-04-24

- In the match list search, arrow keys move the selection, `Return` opens the match, and `Esc` clears.

## 0.0.11 - 2026-04-24

- Fix a broken release.

## 0.0.10 - 2026-04-24

- **AI** settings explains that AI features use your local Claude Code CLI.
- **AI** settings has a **Test connection** button, and new configs default to the `sonnet` model.
- The default log level is `debug`, and routine messages no longer show as errors.
- The match list and editor flag duplicate and shadowed triggers.
- `⌘1`–`⌘4` switch Settings tabs, `⌘F` focuses the search, and `⌘N` adds a match.
- `⌘,` opens Settings from the palette.

## 0.0.9 - 2026-04-23

- AI matches use a top-level `prompt:` with `{{selection}}` and `{{clipboard}}` instead of `ai:`.
- The match editor's **AI prompt** toggle makes AI matches without editing YAML.
- AI prompts with `{{s|c}}` fall back to the clipboard when nothing is selected.
- A new **AI** tab in Settings sets the model, the Claude binary, timeouts and the loading indicator.
- After granting Accessibility, Sokki offers to quit and reopen, and the menu bar has **Quit & Reopen**.
- The match editor's **Source File** has a `+` button to make a new match file.
- The app icon no longer has a white border in the Dock and Finder.
- The Settings match list shows an `AI · <input>` badge on AI matches.
- The starter `match/default.yml` no longer has AI examples.
- Fix a typo with `⌫` and keep typing, and the trigger still expands.

## 0.0.8 - 2026-04-22

- A new **Privacy** tab turns off the anonymous crash reports and daily ping, both on by default.

## 0.0.7 - 2026-04-22

- **Check for Updates…** in the menu bar brings the update window to the front.

## 0.0.6 - 2026-04-22

- Matches with an `ai:` block rewrite the trigger or selection with the local `claude` CLI.

## 0.0.5 - 2026-04-20

- The match list shows each match's file, filters by file, and searches triggers and replacements.
- `⌫` deletes the selected matches in the match list.
- A Spotlight-style palette fuzzy-searches matches and runs them.
- The toggle and palette shortcuts default to none.

## 0.0.4 - 2026-04-19

- Fix Settings not activating on macOS Tahoe.

## 0.0.3 - 2026-04-19

- The menu bar icon is a keycap with a tab glyph.
- Closing Settings keeps Sokki running.

## 0.0.2 - 2026-04-19

- Sokki updates itself, and this version needs a manual download.
- **Check for Updates…** in the menu bar, and a Settings switch for automatic checks.
