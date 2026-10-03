# Review: r-git-url-brief b5e61438

Range `e9be3fc3..b5e61438`, one commit. This is hub-forge stage 4's brief line, and test-plan IK.
- `writeBriefFile` appends the `atrium_git_url` line to BRIEF.md once.
- A fresh launch with no brief gets the same line at the end of its launch prompt.
- The stored `task.Prompt` is left alone.

Verdict: **OK** for room, **with one landing condition:** land it with @fabric's `atrium_git_url`, or after it, not
before.

## How it was checked

- I read the diff and the design's section 4. The line is word for word the one the design names.
- In a scratch worktree, vet is clean and the Brief, GitURL and Resume daemon tests pass. That covers the four new
  tests: once, not doubled, the prompt with no brief through a real sh runner, and no line on a resume.
- I accept the worker's mutants: the append, the already-has check and the prompt branch each fail a test. The
  `req.Resume == ""` guard surviving is equivalent, because a resume with a prompt is refused earlier, and
  `TestAResumeTakesNoGitURLLine` pins that.

## Points

- **Once.** The lowercased contains check means an operator who already wrote the line, in any capitalisation, does
  not get it twice. A brief whose line differs slightly does get a second one. That is harmless.
- **Where it goes.** A brief or prompt ends with `\n\n` and the line. BRIEF.md still ends with a newline. The prompt
  is changed only on a fresh start with a prompt and no brief. A launch with neither gets nothing, which is right.
- **The stored prompt.** `wanted` is changed after the prompt is stored, so the card details and a relaunch show what
  the operator wrote.

## The landing condition

`atrium_git_url` does not exist yet. It is not on landing, and `claude/f-git-url` has no tool on it. Every card
launched after this lands is told to call a tool it does not have, and "never ask for a paste". So a card that needs
outside code is left with no way to get it, short of breaking that instruction.

Land this in the same deploy as @fabric's tool, or after it.

## Lows

- **L1: outside-code cards get the line.** A card tagged `atrium:outside-code` has no git token. The design and
  r-hub-remote both say so. If the URL `atrium_git_url` gives is the room's forwarder, that card cannot fetch from it.
  Either skip the line for those cards (`isOutsideCode` is already at hand in `launchLocked`), or make sure the tool's
  answer for such a card says what it can do.
- **L2: runners without the control MCP.** The line names an MCP tool. A runner row that does not register atrium's
  control MCP, such as a plain shell or a runner whose MCP is off, is told to call a tool it cannot see. Add the line
  only when the runner has the control MCP, or word it as "if you have `atrium_git_url`".

Atrium-Verdict: room-ok e9be3fc3..b5e61438
Quality: small and exact, with the line written once, and the resume and the stored prompt left alone. It needs the
tool it points at to land first.

## Re-read: bf291380

One commit on b5e61438, so the range is `e9be3fc3..bf291380`.

Closed:
- **L1.** An outside-code card, set by the flag or the tag through `isOutsideCode`, gets no line in BRIEF.md or in the
  prompt. `writeBriefFile` takes a `gitURL` bool.
- **L2.** The line is now worded "If you have `atrium_git_url`: ...". I accept that over detecting the MCP, since
  there is no reliable per-runner signal and the wording is safe either way. This differs from the design's sentence,
  and the changelog says so.

Checked in a scratch worktree: vet is clean, and the Brief, GitURL, Resume and Outside daemon tests pass. The four
mutants the worker reports are in the tests I read.

Notes:
- **The merge.** It conflicts only in `docs/test-plan.md`, against IN (r-stdio-launch-lineage, landed at 632db27e).
  Keep both sections.
- **This lands second, so r-stdio-launch-lineage's L2 falls to it.** The cli's `writeBrief` should append the same
  line, or a stdio-launched card's BRIEF.md has no line (it rides on the prompt). That can be a follow-up.
- **The landing condition still stands.** Land with or after @fabric's `atrium_git_url`, which is in review now at
  a1a89240.

Atrium-Verdict: room-ok e9be3fc3..bf291380
Quality: both Lows closed simply and tested.
