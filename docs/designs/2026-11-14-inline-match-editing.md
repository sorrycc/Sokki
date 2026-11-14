# Inline match editing

Before this change, the detail of the Matches page in Settings showed the chosen match read-only.
Changing a word took **Edit…**, a second window, and **Save**. Now the detail edits the match
in place, the way Notes does: the trigger, label, replacement and file are always fields, and an
edit saves itself.

## What stays in the editor window

New and Duplicate (`⌘N`, `⌘D`), the palette (`⌘↵`, `⌘E`) and the HUD still open the editor
window. It suits a match that doesn't exist yet. A match's context menu has **Edit in Window…**
too. Both views edit a `matchDraft` (in `editor.go`), which holds the fields, the validation
(`triggerTooShort`, `conflict`, `problem`, `canSave`), the caret where a variable goes, and the
cached preview and variable ranges.

## Saving

`store.Update` costs more than it looks. It encodes the match files, bumps the store's
generation (so the list filters again and the expander rebuilds its trie), and the file watcher
then reloads the whole configuration and pushes it to settings sync. So the detail keeps a draft
and saves it:

- 600 ms after the last edit (one `time.Timer`, reset by each edit, handed to the main thread);
- when the focus leaves the fields, or `⌘↵`;
- when another match is chosen, the page changes, Settings hides, or Sokki quits;
- at once for the **Enabled** switch and the **Match File** menu, which aren't typed.

A draft saves only when it can be loaded back as it is. A trigger shorter than 2 characters, or
the key of another match, which loading would drop, stays unsaved. The note under the fields
says why, and the bar under the detail says **Not saved**. Choosing another match then shows a
toast with **Keep Editing**, which goes back to the draft. `Esc` in a field puts the match back
as saved.

`SaveMatches` leaves alone a file whose content is unchanged. An edit then writes its own file,
or two when it moves the match, and wakes the watcher once.

While a draft is unsaved, its row in the list shows it as typed. Search and the counts use the
saved matches. Matches keep their IDs across loads by their file and key, and the draft's key
is the store's after a save, so the choice survives the reload that follows. If the match
changes from elsewhere while the draft is unedited, the draft takes the change. If the draft
is edited, it keeps its edits and will save them over the change.
