# Agent history: prompts sent to any agent, found in the palette

Each coding agent keeps the prompts typed into it and recalls them with ↑,
but only its own. A prompt sent in Claude Code yesterday is a retype in
Codex today. Sokki now reads the agents' own history files, keeps one
history of them all, de-duplicated and counted, and the palette finds a
prompt of it, to insert, edit or send in any app. The palette ranks
everything, matches too, by how often and how lately it was used.

## Sources

`internal/history/sources.go` has one reader per agent. Each reads what
was added since its cursor (a byte offset per file, or for a file written
whole a count of records with its size and time), so a scan after the
first costs a `stat` per file.

| Agent       | Reads                                           | Takes                                       |
|-------------|-------------------------------------------------|---------------------------------------------|
| Claude Code | `~/.claude/history.jsonl`                        | `display`; pasted text put back from `pastedContents` or `paste-cache/<hash>.txt` |
| Codex       | `~/.codex/history.jsonl`                         | `text`, `ts`                                |
| Gemini CLI  | `~/.gemini/tmp/*/logs.json`                      | records of `type: user`                     |
| Antigravity | `~/.gemini/antigravity-cli/history.jsonl`        | `display`, not slash commands               |
| Qoder       | `~/.qoder/projects/*/*.jsonl`                    | `humanInput.text` of human turns, not bash mode or side chains |
| Grok        | `~/.grok/sessions/*/*/chat_history.jsonl`        | `<user_query>` of numbered user turns, not synthetic ones |
| opencode    | `$XDG_STATE_HOME/opencode/prompt-history.jsonl`  | `input`                                     |
| Aider       | `.aider.input.history` at home and in each project seen | `+` lines under `# <time>`           |

These files are no agent's promise. A reader that fails logs it and reads
nothing; the others go on. Cursor and GUI apps (ChatGPT, Claude) keep no
such file: Accessibility capture of their input fields is left for later.

Scans run off the main thread (`app.scanHistory`, one at a time): at
launch, every 2 minutes, as the palette shows, from **Read Now**, the menu
and `sokki history import`. New prompts re-filter the palette, its choice
kept.

## Library

`history.Store` keeps `history.json` in the app's data folder, outside
`~/.config/sokki`: it is not configuration, not synced, and its writes do
not reload the configuration (the watcher skips it too, for development,
whose data folder is inside the configuration).

An `Entry` is a text, normalized for identity (`Normalize`: spaces and
line breaks collapsed), with its count, first and last time, the agents
and the latest projects it was sent in, its latest 16 uses, and how often
Sokki recalled it. A prompt sent in Claude Code and in Codex is one entry
of both.

`Clean` leaves out:

- prompts under `MinChars` (12 by default: "continue", "commit")
- slash and shell commands ("/model", "!open .", but not "/Users/…")
- text wholly inside tags, which tools send (`<command-name>…`)
- a "[Pasted text #1 …]" placeholder whose text was not found
- anything that looks like a key or token (`sk-`, `ghp_`, `AKIA`, private keys…)
- the person's own texts to ignore, case-insensitively
- prompts over 20,000 characters, which are documents

Hiding an entry (`⌘⌫`) keeps it, hidden, so a later sending does not
bring it back. Clearing forgets the entries and keeps the cursors; **Read
Everything Again** forgets both.

## Ranking

`internal/search` replaces the palette's subsequence scan, which on long
prompts matched nearly anything and searched only their first 200
characters:

- each word of the query must be found (substring, case-insensitive) in
  the trigger, the title or the first 4,000 characters of the text;
  found at a word's start scores more, and in a script without spaces
  (Chinese, Japanese) every character starts a word
- the trigger scores most (":sig" is found as "sig" too), then the title,
  then the text; the phrase and the title's start score a bonus
- one word may still match a trigger or a title fuzzily, as before
- `@codex` keeps what was sent in an agent (`@sokki` keeps matches),
  `#web` what was sent in a project

The rank is `4 × text score + Boost(frecency)`. Frecency is the count of
uses times the mean weight of the latest 16, a use of the last day
weighing 120 and one older than 90 days 10, as Firefox ranks; `Boost` is
`12 × log2(1 + frecency/100)`, so a hundred uses lift a prompt without
burying a better match. An empty query lists by frecency alone, then by
the latest use. Uses of matches (typed, inserted, copied) are counted in
`history.json` too; `recency.json` stays as it was, for "Used … ago".

A query that adds to the last one still searches only what that one found
(`Query.Narrows`), and the rune mask still skips most docs at the first
key. A prompt saved as a match shows once, as the match.

## Palette

- **Scopes**: All · Matches · History (`⌘1`–`⌘3`), a segmented control
  beside the search.
- **Rows** of the history show the agent where a match shows its trigger
  ("Claude Code +1"), and under the title the project, the count and when
  it was last sent; the preview has the whole text, the agents and the
  project, and how often it was sent and recalled.
- **Keys**: `↵` inserts without sending, so the prompt can be changed in
  the agent; `⌥↵` inserts and presses Return; `⇥` edits it in the palette
  first (`⌘↵` inserts, `⌥⌘↵` inserts and sends, the original kept);
  `⌘S` saves an entry as a prompt (the New Prompt form, filled); `⌘⌫`
  hides it, `⌘Z` showing it again while the footer says so.

Insertion from the palette (`Injector.Insert`) pastes any text with a line
break, whatever its length: typed, a line break is a Return, which sends a
prompt in an agent half-written. Return for `⌥↵` follows the paste after
150 ms.

## Elsewhere

- **Settings → Agent History**: the agents with a switch and their counts,
  when they were read with **Read Now**, the least length and the texts to
  leave out (applied with **Apply**, not as typed), **Read Everything
  Again…** and **Clear History…**.
- **Insights** counts insertions of history prompts by agent (`usage.Event.Agent`),
  in a **From Agent History** card.
- **Menu**: **Agent History → Read Agents Now / History Settings…**.
- **sokki history** searches, shows, copies, hides and unhides entries
  (by a short hash of their text, the same across launches), lists and
  switches the sources, imports and clears.

## Not done

- Accessibility capture of agents without a history file (ChatGPT, Claude
  desktop, web apps).
- A boost for the project or the agent in front: the app in front rarely
  tells its folder.
- Grouping near-duplicates as variants of one prompt.
- Syncing the history: it stays on this Mac by design.
