# sa56 handoff: every dialog sleek (backlog-2 item 56)

Read BRIEF.md first. The PLAN CHANGED after the brief was written. atrium-87300 said: no design phase, no mockups,
no approval wait. Build it now: card details first, then rooms edit-agents, then every other dialog, one commit
per dialog family, on claude/sleek-dialogs. Keep every field and action, change only look and layout. One headless
after-screenshot per dialog to D:/tmp/sa56/after. No before set. When all are done: test plan section
(docs/test-plan.md), CHANGELOG.md entry, commit, then atrium_report to atrium-87300 with the sha and the folder.

The CLAUDE.md files are not in this worktree. Read them from D:/git/github/dovholuknf/atrium/CLAUDE.md,
.../internal/api/CLAUDE.md and .../internal/api/web/CLAUDE.md. The main rules: skins are palette only, colours are
channel triples (`rgba(var(--x-rgb),a)`, never a new rgba literal), run `bash scripts/check-board.sh` (slow, about
6+ minutes, run it in the background) and `bash scripts/check-skins.sh` after edits.

The Bash tool hook refuses `;`, `>` and `>>`, even inside a heredoc. Write files with the Write tool, append with
Edit.

## Done (in the WIP commit)

1. **Shared dialog chrome**, css/dialogs.css, the "the dialog family" block (was "detail dialog"). Every `dialog`:
   - background: a 3px teal-to-blue rule along the top as a background layer (not ::before, a modal scrolls itself),
     a teal radial glow falling from it (`.09`), then the card-0 to card-1 gradient. Border stroke at .7, hairline
     inset.
   - backdrop `rgba(sink,.66)` plus `blur(6px) saturate(120%)`. Entrance is an opacity fade only (`dlg-in` .18s).
     No slide, because dialogs are pinned so they do not move.
   - `dialog[open]:not(.waitcard)` is a flex column. Head and foot are `flex: none`, the body has `min-height: 0`, so
     only the body scrolls.
   - `.dlg-head` padding 24px 26px 18px, a softer divider, h2 at `--fs-2xl`, 600, -.01em. A new `.dlg-eyebrow` above
     the h2 (the wait card's eyebrow: fs-xxs, .16em, uppercase, chip-accent-text) says what kind of thing it is.
     A close button in the head (`.dlg-head > button:not(.go):not(.no)`) is a quiet ghost button.
   - `.dlg-body` padding 22px 26px 24px. `.dlg-foot` is a sink tint (.35) with a soft top divider, padding 14px 26px.
   - `.dlg-sec` is a panel: `var(--lift)` fill, stroke-dim at .45, r-lg, padding 16px 18px 4px. `.dlg-sec-h` is its
     heading, the same eyebrow style.
   - `.dlg-cols` is main plus a 280-340px side column (`.dlg-main`, `aside.dlg-side`, sticky), one column under 960px.
   - `.field` margin 22px. label.eyebrow .14em. `dialog .hintline` fs-xs, 1.55, max 76ch.
   - Buttons in a dialog are one step smaller (fs-base, 8px 14px, r-md) through `:where(...) button`, so any button
     with its own class still wins. `.go` gets an inset `var(--lift)` highlight.
   - Inputs: sink .55, stroke-dim .8, padding 9px 12px, hover goes to stroke, focus is a teal border plus a
     3px `rgba(teal,.14)` ring. `.dlg-body input:not([type])` joined the input rules (#d-icon had no type).
2. **Card details (#detail)**, index.html plus the `#detail` block at the end of dialogs.css. There is an eyebrow
   "card details", the h2 at fs-3xl, and the chips quiet on `--lift`. The left column holds why, the question, the
   recap, notes, actions, say, files, token use and history, in that order. Files and usage moved below say. The
   right column holds three panels: runner (a 2-up button grid, with `.no` buttons danger-tinted and hints across
   the full row), tags, and bell plus mark. The question is a warn callout (warn-stroke border, 3px warn left edge,
   warn-bg-soft fill). Every id and handler is unchanged. Checked in harbour and paper. It looks right in both.

## Tools

- `scripts/shoot-dialogs.js <outdir> [skins] [shots]` is new and in the commit. It is a mocked daemon plus
  `wholeBoard()`, and it saves `skin-name.png`, plus `skin-name-end.png` when the body scrolls. Add a dialog by
  adding an entry to `SHOTS`, a function run in the page that opens it. The harness and fixture entries need their
  lists loaded first. Change them to
  `async () => { await renderRunners(); await renderFixtures(); await editHarness("claude"); }` and the same for
  `editFixture("fx1")`. The runners list lives in `allHarnesses` (runners.js:536) and the fixtures list in
  `allFixtures`. Playwright is installed in this worktree's node_modules.
- `regroup.js` (untracked, in this directory) cuts a dialog's `.dlg-body` into top-level field chunks, each keyed
  by its first id with its comment kept, and reassembles them into `<section class="dlg-sec[ dlg-grid]">` panels
  from a layout JSON. It is UNTESTED. Diff the result carefully, or just use Edit by hand.

## Left, in order (one commit each)

1. **Rooms edit agents**: `#harness` (the runner editor, index.html ~2343, `editHarness` fixtures.js:853) and
   `#fixture` (~2211). Plan: widen #harness to about 980px and group it into `.dlg-sec` panels on a 2-column
   `.dlg-grid`, which needs CSS (a grid of fields, with `.field.span` spanning both columns):
   - what it is: id, label, command, notes, npm package
   - how it starts: args, resume, prompt, model, effort, model/effort env
   - its terminal: launch mode, exit keys, bracketed paste, mid-turn
   - where and with what: cwd, prepare, env, rule import source

   Move the save/delete toolbar into a `.dlg-foot`. `busyWhile` inserts `.busy-why` before the button's parent,
   so it lands between the body and the foot: style `dialog > .busy-why { margin: 8px 26px }`. In #fixture, turn
   the second `.dlg-head` with the inline style into a `.dlg-foot`. Add eyebrows ("runner", "fixture").
2. launch (#launch, ~2499) plus pickrepo, pickwhere and browse
3. the gear (#settings, wide, split into panes by `splitIntoPanes`: keep headings as DIRECT children) plus roomcfg
4. permissions and rules (ask, review, action, source, recogniser, provider)
5. intake / dispatch, roomjoin, hooks, runner-setup, steps
6. theme, sharedlg, toastlog, switcher (check notify.css #toastlog rules), and the rest

For each one: add the eyebrow, group long forms into `.dlg-sec` panels, pin the actions in `.dlg-foot`, add a SHOTS
entry, and screenshot harbour plus one other skin (paper checks the light side) to D:/tmp/sa56/after.

## Exact next step

Wait for the background `check-board.sh` from the last context, or rerun it in the background. Look for FAIL lines,
because the flex-column dialog and the moved #detail fields are the likely suspects. Then fix the harness and
fixture SHOTS entries as described above, and restyle #harness and #fixture.
