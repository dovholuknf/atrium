# @review's standing notes

Read on every context cycle, beside HANDOFF and QUEUE.md. These are facts one review found and later reviews relied
on. They are kept short: one line of what, one of where. Add to it when a review finds a class, not a single bug.

## Classes of bug seen more than once

- **PowerShell literals end at ' and at U+2018-201B.** Quote with `-replace "['‘’‚‛]", '$0$0'`.
  Proven in f-allowed-folders (rd and fabric reviews 87ed8711, 9dadb315). `git grep "-replace \"'\""` over `scripts/`
  is the check.
- **A tool run in a PR's checkout loads the PR's own config or code:** claude `.claude/settings.json` hooks
  (r-pr-run), and opencode `.opencode/plugins/` (token routing). Run in `<run>/work`, with no project sources, and
  prove it with a marker. Persona and agent files too: Claude loads project `.claude/agents/`, so a persona is read
  only from the room's `~/.claude/agents`.
- **A timeout on a parent is not a timeout on its output pipe.** Pipe plus ReadAll blocks while any child holds
  stdout, and a Windows `.cmd` shim makes the child the default case (r-opencode-bubbles). Use a capped writer,
  `WaitDelay` and a tree kill.
- **An allowlist that names fields lets any value through.** An agent-set tag or title becomes an exfil channel
  (OTel). Give each field a value class.
- **`askUser`'s body is innerHTML, and inline `onclick="f('${esc(x)}')"` is breakable.** Entities are decoded before
  JS (u-new-sec-dialog-html, u-new-sec-attr-js-strings). Fixed strings or `data-*` only.
- **Hand-written trailers and authorship are text.** Every commit is `dovholuknf`, and m1mini and sg3 are unsigned.
  Identity comes from hub records, not git.

## Facts that are easy to get wrong

- Card status `done` is set by `atrium_report` on a live session (finish.go:180). It is not "session over".
- `edge` wraps every browser-facing listener with CrossOriginProtection and a host check. CSRF and CSWSH are closed
  (the 2026-09-30 audit's C1 and C2).
- `atrium daemon` defaults to `:7777` and `:7778` (all interfaces), while `atrium run` binds loopback.
  `restart_atrium` respawns `atrium daemon --db` (r-new-sec-daemon-wide-bind).
- dovholuknf/atrium is **public**. Verbatim quotes of clint, third parties' messages, credential states and private
  paths do not go in it. Factory log entries are held until clint decides where the log lives.
- The permission skip lists (Go `permSkipTools`, the dotfiles `atrium-perm-hook.ps1`) still say `Task`; Claude Code
  now calls it `Agent`.
- deployready matches only `hub-ok|room-ok|hold`, so `doc-ok` on design reviews is ignored by it.
- claude's `--autocompact` is a hard startup error outside 100k-1M, and an unknown option is fatal on an older
  claude.

## Known red, not the change under review (macOS, m1mini)

- daemon: unix socket bind (`bind: invalid argument`, a long `/var/folders` path), e.g.
  TestAHostStillHoldingRunnersIsReattachedWithTheSettingOff.
- daemon: TestKeepaliveForkCarriesALeanCardsPromptToolsAndMCP (its must-not `/x` matches `/var/folders/xd`).
- api: TestTheWalkerLaunchSetAndClear (`/private/var` symlink).
- gofmt: internal/daemon/fyi_test.go.
- This session's sandbox cannot exec a fake from `$TMPDIR` (exit 126), so the sh parts of scripts tests stop there.

## How I work

- Board changes: read the headless units, don't run the suite. Do `node --check` on what lands.
- A pasted patch: rebuild the files and match the blob hashes in its `index` lines, which proves it byte-exact.
- Mutation-check tests that guard a security property: break the guard and see the test fail.
- Cycle context when idle or after a batch of verdicts, not after each one.
