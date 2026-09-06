# Overnight brief, 2026-09-06

What was asked for late on 2026-09-05, written down before any of it is built. This is a working document.
The parts that turn out to be decisions rather than tasks belong in `docs/overlays.md`, `docs/backlog.md` and
`docs/federation-design-v2.md`, and should be moved there as they land.

## 1. The zrok panel, reworked

The current panel lets you change things that cannot take effect, and hides the things that can. Every item
below is that same complaint from a different angle.

### Enabled means locked

**If zrok is enabled, the configuration is read only.** To change any of it you disable first.

This is already true underneath and the panel does not say so. The endpoint refuses to move while an
environment is enabled, because a token issued by one instance sent to another fails as a broken token rather
than as a wrong address. A form that accepts an edit and then reports that refusal has taught you nothing,
twice: once when you typed it and once when it came back.

So the fields go read only with one control, `disable to reconfigure`, and the panel says what disabling costs.

### Whose account, as a toggle

When nothing is enabled yet, the panel offers a real choice, and it can tell which options are available
because it can look:

- **The token already on this machine.** Shown only when `$HOME/.zrok2/environment.json` exists for the user
  the daemon runs as. Nothing to type. Atrium shares from the same account as the `zrok` command.
- **Atrium's own token.** A separate environment, its own account, usually a different instance.

A toggle rather than a checkbox, because these are two states of one decision and not two independent facts.

**Choosing atrium's own makes the account token MANDATORY.** It is a password field, it is required before the
enable button does anything, and it is validated by being used: enabling either works or comes back with what
the instance said. There is no useful offline validation of a token, and pretending otherwise with a regular
expression would reject valid tokens from an instance that formats them differently.

### The instance

Always shown, always overridable, defaulted to the public zrok. The hint says which of those two it currently
is, because "the public service, because nobody said otherwise" and "the instance you configured" look
identical when only the value is shown.

### Public and private are not a dropdown

They are two independent capabilities and **both can be on at once**. Two checkboxes, not one select.

The current single `mode` is a leftover from when a share was one thing. A machine may reasonably want a public
link for the board and a private share for a lent session, or the reverse. The stored config becomes two
booleans and the share paths read whichever applies.

### `the address to keep` is the wrong words

It becomes **"what share name would you like to use to access the dashboard"**. The field never explained what
it was for, and the answer is short: it is the hostname people will type.

## 2. Authentication on the board

**This reverses a documented decision and that is worth stating plainly.** `CLAUDE.md` and `docs/overlays.md`
both say authentication is out of scope: single machine, loopback, and reaching it from elsewhere is an
overlay's job rather than an auth layer invented here.

That held while the board was only ever on loopback or behind a zrok private share. It stops holding the moment
the board has a public address, which is what the reserved name work just made comfortable. A public link with
no login in front of a board that can read files, answer permission prompts and type into terminals is not a
line worth defending on principle.

What is wanted:

- **Username and password**, for the simple case.
- **OIDC**, for the real one. Google, Keycloak, whatever.
- **Reuse the zrok instance's own login if possible.** The deployment at
  `D:/worktrees/github/openziti/zrok/zrok2-openziti2/deploy` already runs an IdP, with `SOCIAL-LOGIN.md`,
  `ZITADEL.md` and `idp-*.sh` beside the compose files, so an OIDC provider is already standing. If atrium can
  be a client of that same IdP, somebody already signed in to the zrok console is one redirect from being
  signed in to the board.

Open questions that have to be answered before this is built, not during:

- **Who is the session for.** Atrium has no users, no accounts and no roles. The smallest thing that works is
  one identity, one session cookie, and an allowlist of subjects from the IdP.
- **What loopback does.** The rule should stay that a request from the machine itself needs no login, or every
  hook, the CLI and the MCP server need credentials.
- **What a lent session does.** Sharing one card currently rests on the address being the whole credential. If
  the board grows a login, a guest either gets an exemption for their one card or has to sign in, and the
  second answer defeats the feature.
- **The redirect URL.** OIDC needs a stable one, which ties this to the reserved share name.

## 3. OpenZiti, tonight

Same shape as zrok: atrium binds a service and serves the board on it, with no tunneler in the middle.

Read only, from `D:/worktrees/github/openziti/zrok/zrok2-openziti2/deploy`, including
`zrok2-openziti2-docker/CREDENTIALS.md`. Nothing in that tree is to be modified.

To produce by morning:

1. **An identity for atrium**, created and enrolled, that can bind a service for the board.
2. **The service and the policies** it needs: bind for atrium, dial for the operator.
3. **A tunneler `.jwt`** the operator can enroll on their own machine, which then reaches the board directly.

The board is hosted by the atrium process itself, which is already how `startZitiNative` works: the SDK hands
back a listener and the board is one handler.

## 4. Hub and rooms

The multi machine idea, named. `atrium room` or `atrium spoke`, undecided.

- Two Linux machines, `cdwsl` and `cdzrok`. Both reachable over ssh with passwordless sudo.
- Atrium has to be built for Linux and copied over, so a cross build plus scp.
- Each runs the new subcommand and dials back to the hub, which is this machine.
- The goal for the morning is one hub with two rooms attached and visible.

`docs/federation-design-v2.md` already holds the shape and its rules: leaves dial out, the forum holds nothing,
and identity stays somebody else's job. Two constraints from it that decide what a demo can honestly show:

- **A pseudo terminal cannot leave the machine that made it.** So a room federates its CARDS, their status and
  their permission requests. It does not federate attach. Anybody expecting to type into a remote runner from
  the hub is going to be disappointed, and it is better to say that now than to demonstrate it.
- **The transport is a decision, not a detail.** The leaf dials out, and over what is open: zrok, ziti, or
  plain TLS. Ziti is the interesting answer and it is the one being stood up tonight anyway.

## 5. CI is red, and it is right to be

The workflow landed with `cc6c921` and failed on its first run, which is CI doing its job on day one. Four
failures, none of them from that commit, all of them tests that had only ever been run on Windows.

- **`TestHookCommandQuotesAPathWithSpaces`** asserts that `C:\Program Files\...` comes out slashed.
  `filepath.ToSlash` is a no-op on Linux, so the backslashes survive and the prefix never matches.
- **`TestAFloodIsStoppedWhileItIsReadNotAfter`** delivers a 1MB payload to the child in an environment
  variable. Linux caps a single environment string at 128KB, so the spawn dies with `argument list too long`
  before the bound under test is reached.
- **`TestOutputJustUnderTheLimitIsFine`** fails the same way, on the same 1MB environment variable.
- **`TestSpellingTheSamePathDifferentlyIsNotADifference`** asserts that `C:\Users\x\...` and
  `C:/Users/x/...` are one database. On Linux a backslash is a legal filename character, so calling them
  different is the correct answer and the test is the thing that is wrong.

Two of these are assertions that encode Windows semantics, and two are test plumbing that happens to be
Windows shaped. They are fixed differently.

**The path pair gets gated on `runtime.GOOS == "windows"`.** The production behaviour is right on both
platforms and only the assertion is wrong, so weakening the code to satisfy a test would be the wrong
direction. A Windows path assertion belongs behind a Windows guard.

**The flood pair gets fixed rather than skipped.** What they check is platform neutral and load bearing: that
a source's output is bounded WHILE it is read rather than after, which is the difference between refusing a
flood and allocating it first and then refusing it. The environment variable is only the delivery mechanism.
Writing the payload to a temp file and passing the path works everywhere and removes the size ceiling.

**The matrix is wrong and that is the bigger finding.** CI runs `ubuntu-latest` only, and atrium's primary
platform is Windows: ConPTY, the binary swap, the logon task, every path rule. Half of what ships has no
build machine at all. The matrix becomes both, and a Windows runner is what would have caught the reverse of
each failure above.

## 6. Cut the comments and the prose back

**`/code-audit` across the diff, and the same eye on the markdown.** The comments and the docs have grown
gross, and they grew that way one reasonable-looking paragraph at a time.

The rule this repository already states is that a comment explains the SURPRISING line, and that a file where
every line has a comment is a file where none of them is read. That has stopped being true. What is there now
includes comments restating what the next line plainly does, comments arguing with a decision nobody is
about to reverse, and the same reasoning written out three times in three files because it felt load bearing
each time.

The markdown has the same disease in longer form. `docs/backlog.md` and `CHANGELOG.md` have entries that
narrate the process of arriving at a change rather than the change, and several of them re-explain a rule that
already has a home in a design doc.

What to actually do:

- Run `/code-audit` over the diff and DELETE rather than reword. A comment the adjacent code already shows is
  not improved by being shortened.
- Keep the ones that name a failure somebody has already hit. Those are the ones that earned their place, and
  they are the reason this repository comments at all.
- Cut duplicated reasoning down to one home and link to it. The second copy is the one that goes stale.
- Do the same pass on the markdown. Present tense, state the behaviour, drop the archaeology.

Nothing here changes behaviour, so it is safe to do last and safe to interrupt.

## 7. A paste does not finish scrolling to the bottom

`sendInput` calls `term.scrollToBottom()`, which is why this was believed fixed. It scrolls at the moment the
bytes are SENT, and a paste is not finished at that moment: the runner echoes it back, the prompt grows by
however many lines were pasted, and the bottom of the buffer moves after the scroll has already happened. The
view ends up near the end rather than at it, which is worse than not scrolling, because it looks like it
worked.

The scroll has to happen again once the echo has landed, not only when the input leaves. Whatever the fix is,
it belongs in `check-terminal.js` as an invariant afterwards: rule 4 already asserts that `sendInput` scrolls,
and it passed throughout this bug, so the rule as written is not the property that matters.

## 8. A popped-out window still does not close when its runner exits

Three attempts have failed at this and every one of them fixed the wrong thing. The cause is now known and it
is not subtle.

**`markTermDead` is never called.** It is defined, it rewrites the terminal bar down to a single `close`
button, and it is the only caller of `offerSoloClose`, which runs the countdown and closes the window.
Nothing in the file invokes it. The only other mention of the name is a comment elsewhere describing what it
did to the bar.

So the whole close mechanism hangs off dead code, and everything built on top of it is unreachable too: the
countdown, the `solo-close` broadcast that has the board close the window through the handle `window.open`
returned, and the fallback button. All of it correct, none of it called.

**What actually runs** is the `runner exited` branch in the socket's close handler. It writes
`[atrium] this session has ended. you can close this window` and RETURNS. The message is the only thing that
path does, which is exactly what the screenshot shows: the full bar still present, no `close` button, no
countdown, and a line of text telling the operator to do it by hand.

The fix is to make that branch call `markTermDead`, or to fold the two together. What must not happen is a
third parallel path.

**Prove it after fixing, in the browser, by exiting a runner in a popped-out window and watching the window
go.** Two of the previous three fixes were reasoned about and never observed, which is how this survived
three rounds. Then add an invariant to `check-terminal.js`: a function that rewrites the bar for a dead
terminal must have a caller. A defined-but-never-called function is exactly the shape a parser can catch and
a human reading a diff cannot.

## 9. Settings out to source control, and back

`docs/scm-design.md` already has the design and none of it is built. The outbound half is what is wanted:
atrium's own configuration as files a repository can hold, so a machine can be rebuilt from a checkout.

**The hard requirement is what must NOT leave.** A per-field rule, and the default for an unknown field is to
refuse it rather than to export it, because the failure mode here is silent and permanent: a secret pushed to
a repository stays in the history after it is deleted.

Never exported, under any option:

- The zrok account token, in either environment, and anything else in a zrok root.
- Any ziti identity file, and any enrollment JWT. An identity is a private key.
- The shutdown token.
- Share tokens and reserved names, which are addresses somebody may currently be holding.

Exported, because it is configuration rather than credential: harnesses, fixtures, sources, actions, rules,
skins and board settings, scrollback bounds, sweep and prune timers, browse roots, and the shape of the
overlay configuration WITHOUT the parts above.

Two rules that make the difference safe to rely on:

- **The allowlist is per field, not per file.** An overlay's configuration holds both a mode and a token, and
  a file-level rule has to choose between exporting the token and exporting nothing.
- **A new field is refused by default and the export says so.** Somebody adding a field months from now will
  not remember this document, and the failure has to be a missing setting rather than a leaked one.

Importing is the same list in reverse, and it must never silently overwrite something that is already set.
A machine that already has an overlay configured should be told what the file wants to change, and asked.

## 10. Rewrite the backlog as "what is left", ranked by how good it is

`docs/backlog.md` has become a record of everything that was ever discussed, with the finished parts still in
it and the ordering set by when things were thought of. It is not readable as a plan.

**The new shape is one document called what is left.** Only open items. Anything finished comes out entirely:
`CHANGELOG.md` is where things that landed live, and `docs/status.md` says where each item stands. A backlog
that also holds the history is a backlog nobody reads to the bottom of.

**Ranked by how good the thing is, not by how long it takes.** A gut call, made and written down. Effort goes
in the entry as a note, never in the ordering: sorting by cost puts a week of quiet plumbing above the thing
that would change how the tool feels, every time.

Each entry says what it is, why it is worth doing, and what it depends on. Nothing else.

**Multi-tenant atrium goes back on the list.** It was set aside and it is live again.

## 11. Path completion in the browser terminal. Spike it, then build it

Tab belongs to whatever is running in the pty. Atrium sends the keystroke and the runner decides, so there is
no way to add completion to somebody else's input line by asking nicely. The question is whether atrium can do
it from the outside without corrupting the line, and the answer is a qualified yes.

**What atrium cannot know: where the cursor is, or what the input line currently holds.** It sees bytes going
out and bytes coming back. The runner redraws, wraps, opens menus, recalls history and rewrites the line
whenever it likes. Anything that inserts text on an assumption about cursor position will eventually corrupt
what somebody typed, and that is a far worse bug than having no completion.

**What atrium can know exactly: what IT has sent.** Every keystroke goes through one place. Keep a buffer of
what has been sent since the last Enter, and the tail of that buffer after the last whitespace is the token
being typed. That is not an inference, it is a record.

The design is therefore about giving up early rather than about being clever:

- **Track from the keystrokes atrium sent**, not from the screen.
- **Abandon tracking the moment anything ambiguous happens**: an arrow key, any control sequence, a paste, a
  resize, or a burst of output large enough to be a redraw. Once abandoned, no completion is offered until the
  next Enter resets it.
- **The failure mode is silence.** No candidates offered beats the wrong text inserted.
- **Tab passes through untouched unless atrium is confident**, so the runner's own completion keeps working
  everywhere else. Confident means: a tracked buffer, a token that looks like a path, and candidates found.
- **On selection, send only the characters that are missing.** Never a full line, never a backspace-and-retype,
  because both assume the line is what atrium thinks it is.

The trigger the operator suggested is right: a `/` or a `\` in the token is what makes it a path rather than a
word. On Windows both separators appear and a token may mix them.

Candidates come from `/v1/browse`, which already lists the DAEMON's filesystem and is already bounded by
`browseroots.go`. That is the correct source: the paths being typed are on the machine the runner is on, not
the one the browser is on, and the bound is already argued for in `internal/api/CLAUDE.md`.

**Spike first and keep it short.** The thing to prove is that the tracked buffer stays correct through real
use of a claude session for a few minutes. If it drifts, the answer is a different trigger key rather than
Tab, or the feature is refused with the reason written down. Do not ship a version that can insert text into
the wrong place.

## How to work tonight

**An open entry means not done.** If there is anything left in what-is-left, the night is not over. Do not
stop at a tidy-looking milestone and write a report about the three things that got finished.

**Do not stall.** Where a decision is needed, take the defensible one, write down what was assumed, and keep
moving. Everything built tonight is going to be tested and corrected in the morning anyway, so a wrong guess
that ships is worth more than a right question that waits until eight.

**Keep going for as long as there is capacity**, not until a natural-looking stopping point. The goal in the
morning is an empty backlog, and every entry that is still open is one that could have been attempted.

What this does NOT license:

- Skipping tests, or claiming something works that was never run. The three failed attempts at the popped-out
  window are what happens when a fix is reasoned about and never observed.
- Deleting an entry instead of doing it. An item that turns out to be wrong gets a line saying why it was
  refused, which is a real outcome. An item quietly dropped is a lie about progress.
- Pushing, publishing, or anything outward facing that was not asked for.

## What could stop this overnight

- **Permission prompts.** A gated session stops at the first one and waits for a human who is asleep. Either
  the board wide switch goes on or the run stalls at the first `ssh`.
- **The zrok 500.** `POST /share` on `api-v2.zrok.io` still fails for this account, and every probe says it is
  the instance rather than atrium. Anything depending on a zrok share has to go through the new instance.
- **A daemon restart kills supervised runners.** Any overnight work that needs a restart to take effect has to
  reach a stopping point first.
