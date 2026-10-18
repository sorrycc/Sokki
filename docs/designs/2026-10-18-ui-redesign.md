# The look of Sokki's windows (after 0.1.1)

Sokki's windows were the MyGo widgets as they come, laid out as forms. This
redesign gives them the look of a Mac app of today, and takes the work off
the event tap's thread that slowed typing.

## Settings

A window with a hidden title bar and a sidebar over the sidebar material,
as System Settings: "All Matches", a section of the match files, each with
its count, and the pages General, Shortcuts, Privacy and About. ⌘1 to ⌘5
choose them. A file's page is the Matches page filtered to it, which
replaces the file drop-down.

Settings pages are grouped cards (`group` and `setting` in `style.go`): a
row has its label, what it does in a caption under it, and its control at
the end, a hairline between rows. The page header and the top of the
sidebar drag the window.

General adds the Enabled switch and `conflict_resolve_delay_ms` ("Overlap
delay"); Shortcuts adds `hud_shortcut`, which was only in the file, and
lists the palette's keys. About shows the icon, the version, updates and
links.

## Matches

Rows show the trigger as a keycap (`keycap`), the label over the first
line of the replacement, the file on the All page, and "Off" for a match
turned off, which dims. The footer shows the keys, or the load error of a
match file in the warning color. The list derives its rows once per change
of the store (`Store.Gen`), the file or the query, not every frame, and
searches labels too.

### List and detail (second pass)

The rows left most of a wide window empty, and a replacement could only
be read whole in the editor. The page is now a split (`ui.Split`, the
list 280 wide to start, the user's to drag): rows of the title (the label,
or the replacement's first line) over a line under it, the trigger's
keycap at the end, as the palette's; beside them the chosen match whole:
its title, trigger and file, an Enabled switch, the replacement and, when
it has variables, a preview of them filled in, its length and whether it
is typed or pasted (`clipboard_threshold`), and Edit, Copy and Delete.
The preview is rendered when the choice or its replacement changes, not
every frame: it reads the clipboard.

## Palette, editor, Recent Activity

- **Palette**: a search icon and a larger field, rows of label and keycap,
  a preview with the trigger, file and length, and a footer of the keys it
  takes. Escape clears the query before closing; ⌘N opens the editor with
  the query as trigger. The preview is rendered when the choice changes,
  not every frame.
- **Palette, second pass**: the preview has the match's title, trigger
  and file over the text, on the same plate as Settings' detail
  (`textPlate`); No Results fills the whole body.
- **Editor**: trigger and label side by side, an Insert Variable menu, a
  preview when the replacement has variables, a warning when another match
  has the trigger, the file and Enabled at the bottom, Save in a footer.
  The replacement grows with the window instead of leaving room empty
  under the form.
- **General**: the number fields show their unit (ms, chars) after them.
- **Recent Activity**: a header with a Clear button, each event an icon in
  its verdict's color with a sentence ("Expanded", "Too soon after a
  paste", "Sokki is paused").

## Menu bar

The menu says the state ("Sokki is on", "Sokki is paused", "Paused while
you type a password"), pauses and resumes with the toggle's shortcut
beside it, opens the palette, a new match and Recent Activity, and puts
reload, the log, the config folder and restart under Troubleshooting. The
two icons (on, dimmed) are drawn once.

## Performance

`logx` wrote each line by stat, open, write and close, under the engine's
lock on the event tap's thread: a trace line for every key typed. It now
queues lines to a goroutine of its own that keeps the file open, counts
its size to rotate it, and reopens it once a second at most if it was
moved. A full queue drops lines (counted in the next line) rather than
block the tap. `logx.Flush` writes what is queued, on quit.

### Second pass

A key typed cost 530 ns and 14 allocations on the tap's thread
(`BenchmarkFeed`, 500 triggers): the matcher made a new cursor slice and
a result for every key, and its trace line was formatted three times
over (`fmt.Sprintf` of the summary, of the line, of the time). The
matcher now swaps two cursor slices, and the per-key trace line is built
by hand into a stack buffer (`Context.traceFeed`, `logx.Trace`): 240 ns
and one allocation, the line itself. The log's format is unchanged.

The injector made and freed a CGEventSource for each character typed;
it now keeps one. The palette folded every match's trigger, label and
replacement to lower-case runes for every key typed in its search; it
folds them once per change of the store (`engine.Fold`,
`FuzzyScoreFolded`). The store's file list and counts, which Settings'
sidebar asks for every frame, are derived once per generation.

## Third pass: state, variables, search

- **Accessibility**: `app.trusted` follows the permission (`RequestTrust`
  at launch, `waitForTrust` after). Untrusted, the Matches page shows a
  banner and the tray menu an item, both opening the Accessibility pane of
  System Settings (`accessibilitySettings`); the tray icon dims.
- **Status card**: the foot of Settings' sidebar says what `app.status`
  says (on, paused, paused for secure input, needs access), with the
  Enabled switch, saved as General's is (`app.setEnabled`).
- **Variables** (`engine.Variables`) are drawn apart: in the accent's color
  on a tint in the replacement's plate (`replacementPlate`), in its color
  in the list's titles (`titleText`) and in the editor's text area
  (`TextRanges`). Insert Variable replaces the area's last selection
  (`editorWindow.insert`) and puts the caret after it.
- **Palette search** shows the runes the query matched in bold
  (`engine.FuzzyPositions`, `marked`), in the title or else the trigger,
  worked out once per filter for the rows that show.
- **Last used**: an expansion bumps its trigger's recency as the palette
  does (`app.recordEvent`), which the palette ranks by; the detail's facts
  card and the palette's footer say it (`ago`).
- **Editor and Recent Activity** hide their title bars and draw a header
  under the window's controls that drags the window. Recent Activity
  keeps the title of the match each expansion typed, as of then.
- **Benchmarks** (`cmd/sokki/bench_test.go`): a frame of Matches over 2000
  matches that changes nothing takes about 0.17 ms; a key typed in the
  palette over them, its filter and frame, about 0.5 ms.

## Fourth pass: editing safely

- **Delete with Undo**: deleting a match (⌫, the row's menu, the trash
  button) no longer opens an alert; the match goes at once and a toast
  offers Undo, which puts it back at its index (`Store.Insert`).
- **Duplicate** (⌘D, the row's menu, a button) opens the editor on a copy
  of the match, its trigger warned as taken until it is changed.
- **Copy** shows a toast. The detail's Copy, Duplicate and Delete are icon
  buttons beside Edit, which fit the detail at its narrowest.
- **The editor** asks before dropping edits (`dirty`, against the match as
  it opened) on Escape, ⌘W or its close button; Cancel still drops them.
  A trigger under two characters doesn't save, but one a match had
  already: a lone `:` would expand at every colon typed.
- **Shortcuts**: `c.OnShortcut` in a view nested in Settings only fired
  with the focus inside it, so ⌘N and ⌘F did nothing from the sidebar.
  The Matches page's keys are declared at the window's top
  (`matchList.shortcuts`).
- **The hints row** under Matches goes: with the detail's buttons it made
  two footers. A load error keeps the row, with Open Folder.
- **Sidebar files** have a menu: New Match in This File, Reveal in Finder.
- **Hidden Settings** don't redraw as each expansion bumps a recency; the
  window redraws as it shows instead.
