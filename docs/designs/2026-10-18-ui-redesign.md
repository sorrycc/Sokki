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

## Palette, editor, Recent Activity

- **Palette**: a search icon and a larger field, rows of label and keycap,
  a preview with the trigger, file and length, and a footer of the keys it
  takes. Escape clears the query before closing; ⌘N opens the editor with
  the query as trigger. The preview is rendered when the choice changes,
  not every frame.
- **Editor**: trigger and label side by side, an Insert Variable menu, a
  preview when the replacement has variables, a warning when another match
  has the trigger, the file and Enabled at the bottom, Save in a footer.
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
