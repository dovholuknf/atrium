# Files across cards: a path link that belongs to another card

Backlog item `f-files-across-cards`, high. Status: designed, not built. clint approved the direction on 2026-10-17.

clint, 2026-10-17 23:20: the QA director's terminal names `D:\tmp\qa-ci-last10\table.md`, a report a scratch helper
wrote in its own directory, and the board will not open it. The file is inside a card, just not the card whose
terminal names it.

## What exists

- A terminal path is a link only when `POST /v1/tasks/{id}/files/probe` says it is a file (`fileprobe.go`). The
  probe resolves each candidate through `safepath.Contained` against the clicked card's `worktree`, and a candidate
  outside is absent from the answer.
- A click offers three things (`terminal-links.js` `fileLinkMenu`): atrium's editor (`files/text`, read and `PUT`),
  a tab (`read.html` on `files/view`), and `files/open` when that room has an `editor_command`. Every one is
  addressed by the CLICKED card's id and the probe's `rel`.
- `files` (download), `files/view`, `files/text`, `files/open`, `files/list` and `files/zip` each resolve against
  the card in their URL, and answer 403 for anything outside it, whether or not it exists. Upload and drop compute
  their own destination.
- A card's directory is `task.worktree`: a git worktree, the main checkout, or a scratch directory a launcher made.
  An archived or done card keeps its row and its directory until it is reclaimed. A swept dead card's row is
  deleted.
- `browseroots.go` records why a bound matters: a share terminates on the room's machine, so a request over one
  presents as loopback and nothing can be decided by source address.

## The observation the design rests on

Any card's files can already be read by naming THAT card's id: `/v1/tasks/<owner>/files/view?path=...`. The union
of every card's directory is reachable today, one card at a time. What is missing is only the step from "a path
printed in card A" to "card B holds it". So the design widens one question, "which card holds this path", and
leaves every read where it is.

## Decisions

### 1. Only the probe crosses cards. Every read goes through the owning card's id

The probe gains a second pass. A candidate that is not inside the clicked card is looked up among the room's other
card roots (decision 2). A hit names its owner:

```json
{"path": "D:\\tmp\\qa-ci-last10\\table.md", "rel": "table.md", "dir": false, "size": 2210,
 "card": "01a0...", "card_title": "qa ci last 10", "own": false}
```

`rel` is relative to the OWNER's directory. `own` is true, and `card` is the clicked card, for a hit inside the
clicked card, so today's answer is unchanged apart from two added fields.

The board then uses the owner's id for every action the menu offers: `files/text`, `files/view` and `files/open`
on `/v1/tasks/<card>/...`. No other file endpoint changes. Download, view, text, open, list, zip, upload and drop
keep resolving only against the card in their URL, so a request that names card A never returns a byte from card B.
That is what keeps "no arbitrary machine path" true: the only new answer is a card id, and only for a path already
inside that card.

Rejected: falling through to other cards inside `files/view` and download. It puts the widening on every read
endpoint, makes the 403 rule depend on a room-wide index in six places, and gives a write endpoint (`PUT
files/text`) a path into another card it never asked for.

### 2. Which cards count as roots

A card's directory is a FOREIGN root (a root for another card's probe) when all of these hold:

1. **The row exists.** Live, done, needs-anything, paused and archived cards count. A deleted row is gone, and a
   card whose directory no longer exists (reclaimed) is skipped. The work ledger and history are NOT consulted: a
   directory atrium only remembers is not a card.
2. **It is not wide.** It is never a filesystem or drive root, never fewer than two components below one (`D:\tmp`
   and `/tmp` are refused, `D:\tmp\qa-ci-last10` is not), never the user's home directory or an ancestor of it.
3. **It does not hold atrium's own state.** Never `daemon.StateDir()` or anything inside it or containing it. The
   store, the room key, the hub CA and the address file live there.

Each test runs on the RESOLVED directory, so a junction called `D:\work\x` that points at `C:\` is a drive root.

**The main checkout counts.** It is one repository, not the machine, and it holds no atrium state (the state is in
`StateDir`, and the checkout's `.atrium/` holds pastes and previews). A path inside a worktree that sits inside the
checkout goes to the deepest root.

**A wide card keeps its own reach.** A card whose own directory is a home directory or a drive root still probes
and reads its own directory exactly as today. This design does not tighten that: the card's id was already the key
to it. It only stops such a card from lending its directory to every other card's links. Narrowing a wide card's
own reach is a separate question, filed as its own item if wanted.

### 3. Picking one card when several hold the path

The clicked card first, always. Then, among foreign roots, the DEEPEST one that contains the path. On a tie (two
cards on one directory): a live card over a finished one, then the most recently changed. So a helper's scratch
directory wins over the main checkout around it, and the result is the same every time for the same board.

### 4. Relative paths stay with the clicked card

A relative candidate (`table.md`, `../x`) means "relative to this terminal's directory", which is the clicked card's.
Only an absolute path, or one `probeAlias` turns absolute (`~/`, `/d/...`), is looked up across cards.

### 5. The index of roots, not a scan

The room keeps an in-memory index: one entry per qualifying card, holding the card id, title, status, changed time
and the resolved directory. Resolving is the expensive part (`EvalSymlinks` on Windows), so each entry is resolved
once per `(card id, worktree)` and kept. Every probe that arrives more than 5 seconds after the last rebuild reads the
card list from the store (one query, no filesystem) and resolves only the rows it has not seen. A row whose directory
has gone is dropped at its next resolve. One rebuild runs at a time, and a probe never waits on one longer than
250 ms: past that it answers from the previous index.

A lookup resolves the candidate once, then compares it against each root with the separator-boundary, Windows
case-folding comparison `safepath` already has. 64 candidates against two thousand roots is about 128,000 string
compares, well under a millisecond.

`internal/safepath` gains one exported function, `Owner(roots []string, path string) (int, string, error)`: resolve
`path` once, and return the index of the deepest root containing it, with the resolved path, or `ErrOutside`. The
three rules stay in one place.

### 6. Upload and drop stay per card

Upload and drop compute their destination from the card in the URL and take no path. Nothing here changes them. A
file is always dropped into the card you dropped it on.

### 7. The hub, the ALL view and other rooms

The probe is a `/v1/tasks/{id}/` call, so the hub's card routing sends it to the clicked card's room, and the answer
names a card on THAT room. The board addresses it as `room~card` the way it addresses every card in the ALL view.
Cards on another room are never roots. They are another machine's filesystem, and one room per machine means a path
printed on this room cannot be inside them.

### 8. What a human sees

- The hover on a link to another card's file names it: `table.md in qa ci last 10`.
- The editor's title bar and `read.html`'s header name the owning card when it is not the clicked one, with a link
  to that card. The editor saves to the owner, which is the file the human clicked.
- `files/open` is offered by the owner's room setting, which is the same room.

### 9. What the guarantees say after

Root `AGENTS.md`, resilience guarantee 8, becomes:

> 8. **Files never leave the room's cards.** Upload computes its own destination and accepts no path at all. Every
>    read and write names one card and resolves through `internal/safepath`, which follows symlinks on both sides,
>    against that card's directory only. The probe alone looks across cards, to say which card on this room holds a
>    path, and never counts a directory that is a drive root, a home directory or above one, or atrium's own state.
>    Everything outside every card answers `403`, or is absent from a probe, whether or not it exists, so no
>    endpoint is an oracle for what is on the machine.

`internal/api/AGENTS.md`, the section "Files never leave a card" becomes "Files never leave the room's cards", with
one bullet added:

> - **The probe is the only endpoint that looks past the clicked card,** and it answers with the owning card's id.
>   Every other file endpoint resolves against the card in its URL, so the board reads another card's file through
>   that card. A directory that is a drive root, a home directory or above one, or holds atrium's state, is never
>   lent to another card's links. See `docs/runtime/files-across-cards-design.md`.

`internal/safepath/AGENTS.md` gains a line for `Owner`. `fileprobe.go`'s header loses "IT NEVER LEAVES THE CARD" for
the rule above.

### 10. Tests

Go (`internal/api`, `internal/safepath`), all on temp directories:

- A path inside a second card's scratch directory: a hit naming that card, `own` false, `rel` relative to it.
- A path outside every card: absent, and `files/view` on either card answers 403. Same answer for one that does not
  exist.
- A symlink (a junction on Windows) inside card B pointing outside every card: absent. One pointing from card B into
  card C: owned by C.
- A card whose directory is a drive root, a home directory, a parent of home, one component below a root, or
  `StateDir`: never a foreign root, so a file under it from another card's terminal is absent. The same card still
  probes its own files.
- A path under the main checkout and under a worktree inside it: the worktree's card wins. Two cards on one
  directory: the live one wins.
- A relative candidate is never looked up across cards.
- An archived card counts. A deleted row and a reclaimed directory do not, after the 5 second rebuild.
- `files/view`, download, `files/text` and `files/open` naming card A with a path inside card B: still 403.
- `safepath.Owner`: the separator boundary (`x-evil` is not inside `x`), Windows case folding, deepest root wins.
- An index of two thousand cards answers a 64-path probe inside its bound.

Headless board (`terminal-links.js`, mocked probe): a foreign hit underlines, the hover names the card, the editor
and the tab request the owner's id, and the editor title names the owner.

## Out of scope

- Narrowing a wide card's reach over its own directory (decision 2).
- Paths on another room.
- `atrium_files` and every agent-facing tool. An agent reads its own filesystem, and this is about the board.
