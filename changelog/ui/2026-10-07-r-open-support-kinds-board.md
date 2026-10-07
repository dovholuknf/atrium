A pasted issue, branch, Zendesk ticket or Discourse topic opens through the open verb from the launch dialog, the
Ctrl+Alt+R box and the pulls tab, the same as a pull request: one call makes its worktree, or a scratch folder, and its
card. Enter starts it at once, its missing directory no bar. Shift+Enter on a ticket or a topic stops at the dialog with
a repo field: the recogniser's default repo selected, the repos the board's cards are in, and "no repo" for a scratch
folder. A link the open verb refuses as naming no piece of work launches the old way. A re-paste of an open link says
"already open, attached its card". The pulls tab says a non-PR paste opened a card with no row, and attaches it. The
recogniser editor has a default repo field, host/org/repo or empty for a scratch folder.

Test plan:
- `HEADLESS_ONLY=quickPaste,pulls,recogniserRepo node scripts/test-board-headless.js`: a ticket on Enter opens with
  repo "", an issue opens, not_openable falls back to /v1/launch, Shift+Enter shows the repo field with the default and
  "no repo" sends "none", a PR shows no repo field, a pulls-tab issue paste attaches its card, and default_repo
  round-trips in the editor.
- By hand: Ctrl+Alt+R, paste a Zendesk ticket, Enter, and the card starts in a zendesk-N worktree of openziti/ziti.
  Again with Shift+Enter, pick "no repo", and it starts in a scratch folder.
