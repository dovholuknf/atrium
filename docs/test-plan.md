# Atrium test plan

Manual test scenarios for every shipped feature. Run end-to-end before tagging a build, after touching the
daemon or hook code. Each scenario lists steps, expected behavior, and the most common failure mode.

Sections A, B and D covered Mode A, the v1 hub and agent loop, and are retired with it. C covers the permission
surface, which the daemon kept. E covered Mode B and is retired with it. F is the resilience sweep. G covers the
daemon, which is where the work happens. H covers the overlays, I covers importing rules from Claude Code,
and J covers what landed most recently, written the night it was built so the first person to run it is checking
claims rather than remembering intent.

## Pre-flight

```powershell
cd <atrium-repo>
go build -o build.claude\ .\...
go vet ./...
go test ./...
bash scripts/check-board.sh
pwsh -NoProfile -File scripts/check-powershell.ps1
```

Hard stop if any of them fails. Unlike v1, most of the daemon has real tests, so a red suite means stop rather
than "check by hand".

`check-board.sh` parses the board's JavaScript. It is here because the board is one embedded HTML file with no
build step, so a syntax error in it compiles, ships, and shows up as a blank page. It has caught a real one.

**Do not run `go test ./...` and then wonder why a hook stopped working.** It used to overwrite and then delete
the running daemon's address file. That is fixed, and the fix is a test option that is easy to forget when
adding a new test that calls `Run`: `Options.LocationFile` must point somewhere temporary.

## A. Mode A (hub + agent loop): retired

Retired with Mode A. `atrium hub`, `atrium agent` and the `atrium-agent` MCP server no longer exist, so there is
no submit loop to drive, disconnect or time out. The permission surface they shared lives on in the daemon, and
section C covers it.

## B. Multi-agent: retired

Retired with Mode A. Every scenario here drove the v1 terminal UI: its switcher, its tabs, `/rename`, `@agent`
targeting. The board shows every session at once and section G covers it.

## C. Permissions

The permission hook is `atrium-perm-hook.ps1` in the dotfiles repo. It POSTs to the daemon's agent listener at
`/permission` and blocks on the answer. Every scenario here answers on the board, in the card's permission queue.

### C1. Permission gating round-trip

**Steps**

1. Daemon running. A claude session opened in an atrium checkout, which is gated by the rule in C10.
2. Prompt the agent: `run "echo hello" via Bash`.

**Pass criteria**
- The card moves to `needs-permission` and the request shows with the command echoed.
- Approving it on the board resolves the agent's tool call within 1-2 seconds, and the card goes back to running.

### C2. Permission denial

**Steps**

1. Trigger a permission request as in C1.
2. Deny it on the board.

**Pass criteria**
- The agent's tool call returns blocked and the agent says the command was blocked.

### C3. `y`/`n` does NOT leak as prompt: retired

Retired with Mode A. It tested a shortcut in the v1 terminal UI.

### C4. Daemon-down failover (perm gate)

**Steps**

1. A session that is gated (C10, or `ATRIUM_PERM_GATE=on`). The daemon is DOWN.
2. In the session, ask it to run a Bash command.

**Pass criteria**
- The hook fails open. Claude Code's normal permission prompt fires inside the session, and approving it there
  works. A missing daemon must never block a session.

### C4b and C4c. Terminal banners: retired

Retired with Mode A. Both tested the v1 terminal UI's banner. The board's own notification is covered in G8.

### C5. Permission gating for Write/Edit

**Steps**

1. Gated session. Prompt the agent: `write a file foo.txt with content "hi"`.

**Pass criteria**
- A request shows on the board for the `Write` tool, displaying the file path and the content being written.
- No in-session claude permission prompt fires.

**Common failure**
- Claude's own permission prompt fires inside the session. The hook is filtering by `tool_name -ne 'Bash'`: an old
  hook or a settings.json regression.

### C6. Read-only tools NOT gated

**Steps**

1. Gated session. Prompt the agent: `read README.md and tell me the first heading`.

**Pass criteria**
- No request on the board for the `Read` tool.

**Common failure**
- Read raises a request. The hook's `$skipTools` list got pruned by mistake.

### C7. Footgun guard still wins

**Steps**

1. Approve, on the board, a command that the existing `pre-tool-use-hook.ps1` would reject (for example one
   matching the inline-env-prefixed-docker pattern: `FOO=bar docker ps`).

**Pass criteria**
- Even after the approval, the second hook (the footgun guard) blocks the command. The agent reports the block.

### C8. Deny with free-form guidance

**Steps**

1. Trigger a permission request.
2. Deny it on the board with the reason `no, use a temp file under ./build instead`.

**Pass criteria**
- The agent's tool call returns blocked, and the model sees the typed reason and course-corrects (retries with
  the suggested approach) rather than just reporting a bare block.

### C9. Permissions-only mode

**Steps**

1. Set `ATRIUM_PERM_GATE=on` (env block of settings.json, or `$env:ATRIUM_PERM_GATE='on'`). Daemon running.
2. Launch a plain claude session in a directory with NO `.mcp.json` that has not run `atrium join`. Ask it to
   run a Bash command.

**Pass criteria**
- The request appears on the board, on a card named after the session's cwd leaf (or `ATRIUM_AGENT_NAME`).
- Approving or denying it on the board resolves the tool call.

**Common failure**
- No request arrives: `$forceGate` parsing broke, or the env var did not reach the session. It is read at
  session start, so restart the session after editing settings.json.

### C10. An atrium checkout is still gated

**Steps**

1. `ATRIUM_PERM_GATE` unset. Open a plain claude session (not launched by atrium, not joined) in any atrium
   worktree.
2. Ask it to run a Bash command.

**Pass criteria**
- The request appears on the board. The repo's `.mcp.json` holds no servers, but it still contains the word
  `atrium-agent`, which is what the hook's auto-detect looks for.
- `claude mcp list` in that directory reports no MCP config error.

**Common failure**
- No request arrives: somebody removed the word from `.mcp.json`, or the hook stopped reading it.

## D. Choices picker: retired

Retired with Mode A. The `{choices}` block was a convention taught by the `atrium-agent` tool description and
rendered by the v1 terminal UI, and both are gone.

## E. Mode B (read-only aggregator): retired

Retired with Mode B. `atrium serve`, `atrium status` and `atrium watch` no longer exist. `gwt watch` tails the same
ledger, and the board shows every session.

## F. Resilience regression sweep

### F1. ANSI sentinel translation: retired

Retired with Mode A. Only the v1 terminal UI translated `{green}` and friends, and it is gone.

### F2 and F3. Long-poll keepalive and hub restart: retired

Retired with Mode A. Both exercised the submit loop. The daemon's own restart behavior is covered in section G.

## G. The daemon

Most of this is covered by `go test ./...`. What is listed here is the part a test cannot see: whether the
board is usable.

Start with a throwaway database so nothing here touches real state:

```powershell
.\build.claude\atrium.exe daemon --addr :7877 --http :7878 --db $env:TEMP\atrium-test.db
```

### G1. Cards, columns and clearing

**Steps**

1. Open <http://localhost:7878>. Register a session, or start one from **+ new agent**.
2. Move a card to done, another to dead, another to shelved.
3. Press **clear** in the done column header, confirm.

**Expect** the done column empties, dead and shelved are untouched. Press clear on dead: it empties, shelved
still stands.

**Failure mode** shelved cards disappearing. Shelving is a promise to come back; the store refuses to sweep
one no matter what it is asked, so if this happens the guard has been bypassed rather than loosened.

### G2. Live activity on a card

**Steps**

1. With the activity hooks wired (see `docs/backlog.md`), have a session run something slow.
2. Watch its card.

**Expect** a badge reading `running Bash` that breathes, with an age once it passes five seconds. A subagent
count appears when the session spawns one. The badge is absent while the card is waiting, because the column
already says that.

**Expect after a daemon restart** every badge is gone. This is correct: the daemon does not know any more, and
a card claiming to run a tool inside a process that no longer exists is worse than a card saying nothing.

**Failure mode** a badge stuck on a session that died. It should expire after fifteen minutes on its own.

### G2b. A finished worker stops thinking

**Steps**

1. Launch a worker that ends its brief with `atrium_report` status `done`, then ends its turn.
2. Watch its card, in the board and in the terminals list, as the turn ends.

**Expect** the card lands in `done` and the badge does not read `thinking`. The report marks the card done during
the tool call, the tool-end sets `thinking` again, and the Stop that follows puts it to idle.

**Failure mode** a `done` card whose badge still reads `thinking` for up to fifteen minutes.

### G3. Auto mode and the review

**Steps**

1. Open a card, press **auto mode**, confirm.
2. Have that session make several tool calls, including some repeats of the same command.
3. Press **what did it do?**

**Expect** nothing was asked, the card shows an `auto` badge, and the review shows every call grouped by
tool with repeats folded into one line carrying a count. The totals count decisions, not lines.

**Then**: write a `never` rule for something, and have the session try it.

**Expect** it is still blocked. Auto mode means stop asking me new questions, not forget the answers I gave.
Same for a shelved card and for a queued message, both of which still reach the session.

### G4. A folder rule

**Steps**

1. **perms**, then **allow a folder**. Give it a directory and pick **everything**.
2. Have a session run a command naming a file inside that folder, quoted, with backslashes.

**Expect** no prompt. The same command against a sibling folder whose name merely starts the same, for
example `D:/tmp` versus `D:/tmpfiles`, still asks.

**Failure mode** this is the case a command glob silently fails: `rm -f "C:/x/*"` does not match
`rm -f "C:/x/y.db"` because of the closing quote. If a folder rule ever starts behaving that way, it has been
turned back into a glob somewhere.

### G5. Which agent is asking

**Steps** with two sessions gated, let both make a request.

**Expect** each pending card names its agent above the command, the decisions log has an agent column, the
search box matches on it, and a CSV export includes it.

### G6. Stopping is not killing

**Steps**

1. Start a runner from the board so atrium owns its terminal. Attach to it.
2. Run `atrium stop`, or POST to `/v1/shutdown`.

**Expect** the CLI returns immediately, and the daemon's log narrates: event streams released, supervised
runners given up to ten seconds, each listener closing and closed, then the database path and total time.

**Compare** `taskkill /F` on the daemon: every supervised terminal dies at once with no chance to finish.
That is the difference this endpoint exists for.

**Then** with `--shutdown-token some-token`: a request with no token, or the wrong one, is refused with 403
and the daemon keeps running.

### G7. The directory picker

**Steps** open **+ new agent**, press **browse**.

**Expect** the daemon's filesystem, drives at the top level, checkouts marked and sorted first. Recent
directories appear as one-click buttons under the path field. Enter starts the runner from any field.

**Expect on a phone** the same listing, because it is the daemon's filesystem being listed and not the
browser's. This is the whole reason it is not the native picker.

### G8. Notifications take themselves down

**Steps** set an expiry under the gear, trigger a permission request, and leave it.

**Expect** the notification disappears on its own after that long. Set **never, until answered** and it stays
until the request is decided from anywhere.

**Failure mode** notifications piling up in the Windows action centre, which is what sticky means there.

### G9. The inbox, filled by hand

**Steps**

1. Post an item to the throwaway daemon:

```powershell
$body = @{
  source = "github"; external_id = "openziti/ziti#4211"
  url = "https://github.com/openziti/ziti/issues/4211"
  title = "tunneler drops DNS on resume"
  suggested_cwd = "D:/git/github/dovholuknf/atrium"
  prompt = "read the issue and tell me what you think the fix is"
} | ConvertTo-Json
Invoke-RestMethod -Method Post -Uri http://localhost:7878/v1/intake -Body $body -ContentType application/json
```

2. Post it a second time, unchanged.
3. Post it a third time with `source = "GitHub"`.

**Expect** one card, in an **inbox** column that was not there before. The second and third posts answer
`created: false`. The chip on the card reads `openziti/ziti#4211` and opens the issue.

**Failure mode** three cards. That means the deduplication key is not being canonicalized, and a poller would
fill the board on every tick.

### G10. Starting an offered card

**Steps** press **start** on the inbox card. The launch dialog opens with the directory, the title and the
first instruction already filled in. Press **start it**.

**Expect** ONE card, the same one, now running, still carrying its `#4211` link. The instruction is what the
session begins on.

**Failure mode** two cards, one running and one still sitting in the inbox. That means the card was registered
rather than claimed, and the work has lost its link to what it was for.

### G11. A source on a timer

**Steps** in **runners**, add a source pointing at `scripts/sources/github-assigned.ps1` with `gh` installed
and logged in. Leave it disabled. Press **run it now**.

**Expect** it says how many new items it found, or says nothing was new, or shows the reason it failed. A
failure puts the reason on the row rather than only in the log.

Then break it on purpose: change the command to something that does not exist and press **run it now** three
times.

**Expect** the row switches itself off after the third, with the reason still attached. Turning it back on
clears the count.

**Failure mode** a source retrying forever against a script somebody deleted, which is a process spawned every
interval to produce an error nobody reads.

### G12. An agent saying it finished

**Steps** in a session that is on the board, run:

```powershell
atrium finish "bumped the dep, ran the tests, opened a pull request"
```

**Expect** the card moves to **done** and carries a `recap` chip whose tooltip is that sentence. Run it again
in another session with no argument.

**Expect** that card is done with a `no recap` chip instead.

Then try `atrium finish --hand-back "got as far as the build failing"`.

**Expect** **ready**, not done, with the recap attached. That is a different claim and the board should show it
as one.

**Failure mode** the card not moving at all, which usually means `ATRIUM_AGENT_NAME` is unset and the directory
name does not match the card's wire name.

### G13. An action, including the exit half

**Steps** open a card with a supervised terminal and press **write it up and finish** under `do this`.

**Expect** the prompt is typed into the terminal and the toast says `typed into its terminal`. On a card with no
terminal, the toast says `queued` and the message arrives on the session's next tool call.

Then make an action with `afterwards: ask the runner to quit` and press it on a supervised card.

**Expect** a confirmation first, then the prompt, then the runner exiting a moment later. On a card atrium does
not own, expect the prompt and a note saying nothing can make it quit.

**Failure mode** the runner quitting before it has taken the prompt. That is the known weak point: there is no
signal that a runner has accepted a line, so the gap between the two is a fixed pause.

### G14. Auto mode running out

**Steps** turn on **approving everything** and choose **for an hour**.

**Expect** the header reads `approving everything, 60m left` and counts down. Turn it off and on again with
**until I turn it off**.

**Expect** no countdown, and the tooltip says there is no deadline.

To see it expire without waiting an hour, set the deadline into the past directly in the database and make a
tool call.

**Expect** the request reaches you, and the badge stops claiming auto mode is on.

**Failure mode** a request being approved after the deadline. Nothing enforces the deadline on a timer by
design, so this means the chain is not reading the clock.

### G15. Pasting a screenshot into a session

**Steps** attach to a supervised terminal, copy an image to the clipboard, and press ctrl-v on the terminal
pane. Then drag a file onto the same pane.

**Expect** both land in `.atrium/incoming` under the card's working directory, and the path is spliced into the
terminal WITHOUT enter being pressed. Type a sentence around it and send it yourself.

**Expect** the agent can read the file at that path.

**Failure mode** the path being submitted on its own, which starts the agent working on half a sentence. That
is the one thing this must not do.

### G16. Files cannot escape a card

**Steps** with the daemon running, ask for something outside a card's directory:

```powershell
curl "http://localhost:7878/v1/tasks/<id>/files?path=C:/Windows/win.ini"
curl "http://localhost:7878/v1/tasks/<id>/files?path=../../../../etc/passwd"
```

**Expect** `403` for both, and the same `403` for a path outside the card that does not exist, so the endpoint
cannot be used to find out what is on the machine. A file INSIDE the card that is missing answers `404`, which
is fine.

**Failure mode** anything being served, or the two outside cases answering differently.

### G17. Everything that has ever run here

**Steps** open **history**. Search for a word from a card's recap. Switch the filter to **never written up**.

**Expect** archived cards appear alongside live ones, the search matches titles, reasons, tags, recaps and
external identifiers, and the two filters partition the list.

**Failure mode** archived cards missing, which means the view is reusing a board query that excludes them and
therefore answers the wrong question entirely.

## H. The overlays

The one part of atrium that cannot be tested without a network somebody else runs. Everything here is manual on
purpose, and H1 has been run and passed.

### H1. zrok, end to end

**Steps**

1. Gear, `reach this board from elsewhere`. zrok should read as ready, meaning this machine has an environment.
2. Mode `private`. Press start.
3. The panel shows `zrok access private <token>`. In another terminal:

```powershell
zrok access private <token> --bind 127.0.0.1:9911
curl http://127.0.0.1:9911/v1/tasks
```

4. Press stop. Run the `zrok access` line again.

**Expect** the board's own card JSON through the tunnel on step 3, and a token that no longer resolves on
step 4.

**Failure mode** a share that starts and cannot be reached. That is usually the backend field pointing
somewhere other than the human listener, which is `localhost:7778` and not `:7777`. **Never put the agent
listener on a share.**

### H2. A file comes back out over the share

**Steps** with the share up, open a card, unfold `files`, and download one through
`http://127.0.0.1:9911` rather than through localhost.

**Expect** the same bytes. This is the case the whole file panel exists for: the machine you are sitting at is
not the machine the agent is on.

**Failure mode** the download working on loopback and not through the tunnel, which would mean something in
that path is bound to the local address rather than served by the board's own handler.

### H3. A share that was never set up refuses before it tries

**Steps** with no ziti identity configured, press start on the OpenZiti panel.

**Expect** atrium's own refusal naming the next step, not ziti's message about a file it could not load.

**Failure mode** a stack trace or a library error reaching the board. `docs/overlays.md` has the reasoning:
report the state, offer the next command, never invent one.

### H4. OpenZiti, end to end. NOT YET RUN.

There is no enrolled identity on this machine, so the ziti listener has never been exercised. Everything up to
it is covered by tests. When an identity exists: enroll from the gear, pick a bindable service from the list the
service field offers, start, and reach the board from another machine on that network.

Until somebody does that, `docs/overlays.md` says so rather than implying both halves are equally proved.


### H5. The zrok account block counts what the account is holding

**Steps**

1. Gear, `expose the board`, with zrok enabled. The block sits under the `enabled against ...` line, above
   `configure zrok`.
2. Compare it against the account itself:

```powershell
$tok = (Get-Content ~/.zrok2/environment.json | ConvertFrom-Json).zrok_token
curl.exe -s -H "x-token: $tok" https://api-v2.zrok.io/api/v2/overview
```

3. Press `check again`.

**Expect** the same three counts: environments on the account, shares summed across all of them, and names
with `reserved` true. Not just this machine's. The line underneath says how many shares are this machine's and,
when there are any, how many reserved names start with `atrium-`.

**Expect no fraction anywhere.** zrok does not tell an account token where its ceilings are, so a "3 of 5" in
this block would mean somebody invented a denominator. `docs/overlays.md` has the reasoning.

**Failure mode** counts that only cover this machine. The limit is per account, and every machine that ever ran
`zrok enable` is an environment on it, so a count that stops at this one reads comfortable while the account is
full.

### H6. A limit says which limit

Hard to stage deliberately. Do it the next time an account is actually at one.

**Expect**, when zrok reports the account as limited, that the block keeps its counts and adds a warning naming
the TRANSFER allowance. That flag is bandwidth only, so a warning that blames the counts sends somebody to
delete shares over a block deleting shares does not lift.

**Expect**, when a name reservation is refused for the name ceiling, a message that says to release a name.
**The failure this replaces is the one to watch for:** "that name is taken, try a longer one", which is what an
account at its name limit used to be told, and no longer name would have helped.


### H7. Pasting into a terminal over a share

**Steps** with the share up, open the board at the share address rather than at localhost, attach a terminal,
and paste three ways. First `ctrl-v`. Then right click on the terminal. Then `ctrl-shift-v`.

**Expect** `ctrl-v` pastes, exactly as it does on loopback: that path is the browser's own `paste` event and no
permission is involved in it. Right click pauses for about a second and then opens a paste box in the middle of
the pane. `ctrl-shift-v` opens the same box straight away. Press `ctrl-v` in the box and the clipboard lands in
it. Press `ctrl-enter` or `send` and it reaches the runner, with a multi-line paste arriving as one paste rather
than one Enter per line. Close it and the box is empty the next time it opens.

**Then do the same on loopback.** Expect NO box: the paste goes straight in.

**Failure mode** right click doing nothing at all, silently and forever, which is what shipped. Reading the
clipboard from script is a permission granted per ORIGIN. Loopback was answered once and remembered, so it
looked like it worked everywhere. Every share is a new origin, the prompt stands there unanswered, and
`navigator.clipboard.read()` and `readText()` NEVER SETTLE. Not a rejection, which would have been caught and
said out loud. The second failure mode is the box appearing on loopback, which means the bound is firing on an
origin that had already granted the permission.

## I. Importing rules from Claude Code

The one part of the permission surface with no scenario, named in Known gaps for months. It matters because it
is the only path that writes a standing rule atrium did not watch somebody make, and a standing rule is the
thing that answers without asking.

### I1. Import, and what it refuses to bring

**Steps**

1. Rules tab, `import from claude code`. Leave `include broad` off.
2. Read the list of what it skipped.
3. Import again, unchanged.
4. Turn `include broad` on and import once more.

**Expect**

- Rules whose pattern matches EVERY request for a tool are skipped, and each skipped row says why in a sentence
  rather than a code. A rule that answers everything is not a rule, it is auto mode with worse provenance.
- Step 3 adds nothing and updates nothing. Import is idempotent: the same settings file imported twice is the
  same set of rules, or every restart of a habit doubles somebody's rule list.
- Step 4 brings the broad ones, and they arrive marked with where they came from.

**Most common failure** the count reported does not match the rows that appear, because `added` and `updated`
are counted separately and one of them is not shown.

### I2. Export and re-import

**Steps**

1. Rules tab, export as JSON.
2. Open the file. It should be a `{"rules": [...]}` object, not a bare array.
3. Import it back with `source: json`.

**Expect** no change at all: the same rules, no duplicates, no updates. The export is shaped so it can be handed
straight back, and a round trip that adds anything means the two ends disagree about what identifies a rule.

## J. What landed on 2026-09-05

Written the night the work was done, so the first person to run these is checking claims rather than
remembering intent. Every one of these is fast.

### J1. The event log answers with the newest events

**Steps**

1. Open a card that has been running a while. Card dialog, the timeline.
2. Compare the newest entry against what that session just did.

**Expect** the LAST events, ending in something that happened in the last few minutes, ascending down the page.

**What it used to do** answer with the oldest 200, so a busy card showed the day it was created and nothing
since. If the timeline ends hours or days ago on a card that is working, the fix is not in.

### J2. The directory picker refuses what is outside its roots

**Steps**

1. Launch dialog, press browse. Note what the root list offers.
2. Walk into a card's directory, then press up repeatedly.
3. Settings, this machine, `the picker may open`. Read `Right now:`.
4. Add a directory of your own, save, press browse again.

**Expect**

- The root list is your home directory plus every directory a card, fixture, source or harness already names,
  rather than `C: D:`.
- Pressing up from a root returns to the root list rather than climbing to its parent.
- The line under the box names exactly what resolved. A path that does not exist is dropped, and the only way
  to notice is that it is missing from that line.

**Also check** the picker fills in the field that OPENED it. Open it from a fixture's directory box, choose
something, and confirm it landed there rather than in the launch dialog's box.

### J3. Hooks stop reading `points elsewhere`

This one needs the manual step in `REPORT.md` first: install, restart the daemon from the installed path, then
`atrium hook install`.

**Steps**

1. Gear, hooks. Read the state beside each row.
2. Hover any row that says `points elsewhere`.

**Expect** every atrium row reads `wired` once the three steps are done. Before that, hovering a stale row names
BOTH paths: what the entry runs, and what the daemon is running. The two differing by one directory is the whole
story.

**Then rebuild and check again.** `go build`, and the rows must still read `wired`: the identity is the
installed binary, not the one you just built, which is the entire point.

### J4. Priority is weight, not order

**Steps**

1. Right-click a card, `how much it matters`, `high`.
2. Look at the column it is in. Then mark a card in `needs-permission` normal and a card in `running` high.
3. Click the `high` chip.
4. Find a card marked high more than a week ago, or change the clock, and look at the chip.

**Expect**

- Nothing reorders. A `needs-permission` card stays above a high-priority idle one, because a blocked agent is
  blocked whatever you think of the work.
- The chip filters the stack to `!high`.
- A priority older than a week is drawn faded, and the tooltip says how old it is.
- The flyout ticks the level already set, and picking that same level clears it back to normal.

### J5. A held note says so on the card

**Steps**

1. Open a card, write something in `notes to self`, close the dialog without sending.
2. Look at the card on the board and its row in the stack.
3. Hover the `note` chip.
4. Open the card and press `send it`.

**Expect** an amber `note` chip in both places, carrying the text in its tooltip and NO send button. Sending
clears both the note and the chip. A chip that survives a send means the note was not cleared, which is the one
outcome that matters: the note is cleared only after it is safely somewhere else.

### J6. The logon task points somewhere that does not move

Do not run this on a machine where atrium is already registered unless you mean to replace the registration.

**Steps**

```powershell
.\scripts\atrium-autostart.ps1
Get-ScheduledTask -TaskName atrium | Select-Object -ExpandProperty Actions
Get-ScheduledTask -TaskName atrium | Select-Object -ExpandProperty Principal
```

**Expect** the action runs `conhost.exe --headless <installed path> daemon --db ...`, and the principal names
the identity in the form this machine uses (`DOMAIN\user` or `MicrosoftAccount\...`), not a bare username. The
path must NOT be under `build.claude`.

**Then** log out and back in, and confirm no console window appears.

## K. What landed on 2026-09-05, the second batch

The shell is the one to run first: it is a new process on the machine, it is the only thing here that could
leave something behind, and nothing about it has ever been seen rendered.

### K1. A shell beside the agent

**Steps**

1. Attach to a supervised card. The terminal bar shows `agent | shell`.
2. Press `shell`. Type `pwd` on Linux or `cd` on Windows.
3. Press `agent`. Press `shell` again.
4. Right-click the same card on the board. Read the entry.
5. Attach to a DIFFERENT card. Look at the toggle.

**Expect** the shell opens in the CARD'S directory, not the daemon's. Step 3 returns to the same shell with the
`pwd` still on screen rather than a fresh one. At step 4 the card menu says `go to its shell` rather than
`open a shell here`. At step 5 the pane is on `agent`, because a shell belongs to the card it was opened in.

**Also expect** the card does not move. Its status, its column and its timeline are exactly as they were.

### K2. Closing a shell does not kill a card

**The failure this exists for.** The obvious implementation puts shells in the same map as runners, and then
closing one runs the runner's exit path: an exit event, and the card filed as `dead`.

**Steps**

1. Open a shell on a card whose agent is running.
2. Type `exit`.
3. Look at the card, its column, and its timeline.

**Expect** nothing happened to the card. The agent is still running, the status has not moved, and there is no
new entry in the timeline. The terminal says `[atrium] this shell has closed`, not `this runner has exited`.

### K3. A shell does not outlive its card

**Steps**

1. Open a shell on a card you are willing to delete.
2. Delete the card.
3. On Windows, try to remove the card's directory.

**Expect** the removal works. A shell left holding that directory open is the failure, and on Windows it
presents as a permission error on the delete rather than as anything to do with atrium.

### K4. A lent session is not a command line

**The one to actually run, because it is the security claim.**

**Steps**

1. Share a card with `share this session`, WRITABLE.
2. Open the share's address. Confirm the terminal works.
3. In that page's dev tools, open the same socket with `?kind=shell` appended:
   `new WebSocket(location.origin.replace(/^http/, "ws") + "/v1/tasks/<id>/attach?kind=shell")`
4. Try `fetch("/v1/tasks/<id>/shell", {method: "POST"})` in the same console.

**Expect** step 3 fails with 403 and a sentence saying a shared session is the agent's terminal. Step 4 also
fails with 403 and no shell appears on the card.

**Why by hand as well as in a test.** The Go test asserts the handler refuses. This asserts the SHARE refuses,
end to end, over the real address, which is what somebody actually has.

### K5. The skins

**Steps**

1. Settings, the board, `how the board looks`. Change it. Do not close the dialog.
2. Try `noir`, then `sandstone`, then back to the first entry in the list.
3. Pop a terminal out into its own window, then change the skin on the board.
4. Reload the board.
5. Open the board in a second browser, or a private window.

**Expect** it repaints as you pick, without closing the dialog. The popped-out window changes at the same
moment rather than at its next reload. The reload comes back in the chosen skin with no flash of navy. The
second browser is also in the chosen skin, because it is a daemon setting and not a browser one.

**Look for the failure this is really about:** any element still wearing the old palette. A chip, an inset, a
scrollbar, the recessed box behind an input. That is a colour that was written as a literal instead of a
variable, and `scripts/check-skins.sh` cannot see it.

### K6. A skin nobody ships

**Steps**

```powershell
curl.exe -s -X POST http://localhost:7778/v1/settings `
  -H "Content-Type: application/json" -d '{\"board_skin\":\"hot-pink\"}'
```

**Expect** a 400 naming the skins that would have worked. Not a 200: a skin that saves and does nothing is the
worst answer available, because the setting looks like it took.

### K7. The runners page has five panes

**Steps**

1. Runners tab. Read the spine on the left.
2. Move between panes. Note the scroll position.
3. Reload, go back to the runners tab.
4. Launch dialog, press `configure`.

**Expect** five panes: start, runners, fixtures, sources, actions. Switching scrolls to the top rather than
landing halfway down. The reload comes back on the pane you left. `configure` lands on `runners` rather than on
whichever pane was last open.

### K8. A share that dies says so

**Hard to stage deliberately, so this is the honest version.**

**Steps**

1. Start a zrok share.
2. Take the machine's network away, or stop the zrok environment from elsewhere, and wait.

**Expect** a toast and a desktop notification naming what stopped and why, without the gear being open.

**What does NOT alert:** pressing `stop sharing`, and a board that opens onto a share that died an hour ago.
Both are correct. The alert is for a share ending while you were not looking.

### K9. Reserve and share in one press

**Steps**

1. Settings, expose the board, configure zrok. Set the mode to `public`.
2. Type a name in `the address to keep`. Press `reserve and share`.

**Expect** it asks the public-share confirmation first, then reserves and starts, and the address appears. The
convenient path must NOT skip the warning that this puts a board with no login on the internet.

### K10. A grouping expression cannot be stored on the daemon

**Steps**

```powershell
curl.exe -s -X POST http://localhost:7778/v1/settings `
  -H "Content-Type: application/json" -d '{\"group_by\":\"return task.repo\"}'
```

**Expect** a 400 that explains WHY, not just that it refused: the expression is compiled and run by whichever
browser loads the board. If this ever answers 200, the rule has been broken and the thing to read is the
comment above it in `internal/api/settings.go`.

## Notes for future automation

- The simple stdin mode (`atrium hub --simple`) is a single-process script-friendly target. A Go-test could
  spin up a hub on a random port, fire a synthetic agent client, drive prompts, assert responses. Worth
  building when the surface stabilizes.
- The choices parser (`extractChoices`) is pure Go and easy to unit test. Same for `wrapLines` / `wrapOne` /
  `visibleLen`. Adding `internal/tui/parse_test.go` would catch regressions cheaply.

## L. The preview board

These are the scenarios `atrium preview` exists to make safe, so each one is written as the damage it must not
do. Run them with a real daemon running and at least one live claude session gated through it, because a preview
that behaves on an idle machine proves nothing.

### L1. A preview does not take the hooks

**The failure this exists for.** Every daemon writes its address to one file and every hook reads that file. A
second daemon without `--location-file` takes all of them, and the symptom is not an error: it is the real board
going quiet.

**Steps**

1. Note the real board's address. Have a gated claude session open.
2. From a worktree: `atrium preview --http 50022 --from live`.
3. In the gated session, run any tool call.
4. Watch the REAL board.

**Expect** the activity badge and any permission prompt appear on the REAL board. The preview draws the cards it
copied and does not move. Stop the preview, run another tool call, and the real board still works: a preview
must not remove the address file on its way out either.

### L2. A preview starts nothing and re-binds nothing

**The failure this exists for.** Everything in the copied database is real. The first version of preview spawned
the operator's fixtures and went after their reserved zrok name.

**Steps**

1. Have at least one fixture defined and one card with a lent share.
2. `atrium preview --from live --fresh`.
3. Watch the preview's own log, and the machine: is there a new terminal? Does the real share still answer?

**Expect** no fixture terminal opens, no share is re-bound, and nothing is swept off the zrok account. The board
still lists the fixture and the share as rows, because a copy shows what it copied.

### L3. The copy takes the sidecars

**Steps**

1. With the real daemon running, make a visible change on the board, for example move a card to another column.
2. `atrium preview --from live --fresh` immediately, without stopping the real daemon.

**Expect** the change is there. Recent writes live in the `-wal` file, so a copy without it looks like a board
that is mysteriously an hour out of date rather than like a bad copy.

### L4. Ports, and saying no rather than colliding

**Steps**

1. Start two previews with no `--http`.
2. Start a third with `--http` set to a port that is already taken.

**Expect** the first two each print a different address and both work. The third refuses with the port named,
before starting a daemon.

### L5. Cards are kept unless you say otherwise

**Steps**

1. `atrium preview --from live`. Shelve a card on the preview board. Ctrl-C.
2. `atrium preview --from live` again.
3. `atrium preview --from live --fresh`.

**Expect** step 2 keeps the shelved card and SAYS it is keeping the cards this preview already had. Step 3 starts
from a new copy. A preview that silently re-copied would produce "why are these cards stale" an hour later, and
one that silently kept them would produce the same question the other way round.


## M. Cutting a release

Nothing here touches the board. It is in this document because a release is the one procedure that gets run
rarely, by a person, under pressure, and the parts of it a script cannot check are the parts that reach a
stranger.

`scripts/check-release.sh` already asserts every refusal in CI, so do not re-test those by hand. What is left
below is what only a human and a network can answer.

### M1. The dry run is the whole run

**Steps**

1. On a clean tree: `bash scripts/cut-release.sh v0.1.0`
2. Read the output from the top.
3. `git tag -l` and `git status --porcelain`.

**Expect** it compiles five platforms, builds four Linux packages, prints `atrium version --short -> v0.1.0`,
says linux/amd64 builds byte for byte the same from the commit alone, verifies every hash, writes
`build.claude/release/v0.1.0/scoop/atrium.json`, and prints the `gh release create` command it did NOT run.

**Also expect** step 3 shows no new tag and no change to the tree. A dry run that created the tag would be a
decision, and the default is meant to be the option that decides nothing.

### M2. The version the binary reports

**The failure this exists for.** The version is stamped by the linker and is `dev` when it is not. A release
that reports itself as `dev` is one a package manager will never offer an upgrade over, and it looks completely
normal until somebody runs it.

**Steps**

1. Unzip `build.claude/release/v0.1.0/atrium_v0.1.0_windows_amd64.zip`.
2. Run `atrium.exe version` from where it unpacked.

**Expect** `atrium v0.1.0` and the commit the tag names. Not `dev`, and not a commit with `(dirty)` after it.

### M3. The scoop bucket, which is the real test of the release shape

**Steps**

1. Publish: `bash scripts/cut-release.sh v0.1.0 --execute`, or let the tag push run the workflow.
2. Copy `build.claude/release/v0.1.0/scoop/atrium.json` into the bucket repository as `bucket/atrium.json`,
   commit and push.
3. On a Windows machine that has never had atrium: `scoop bucket add dovholuknf ...` then `scoop install atrium`.
4. `atrium version`.

**Expect** scoop downloads the zip, its hash matches the manifest, and step 4 prints `v0.1.0`.

**What a failure here means.** A hash mismatch means the manifest and the asset disagree, which is the one
failure the publish path re-hashes specifically to prevent, so it points at the bucket copy rather than the
release. A "cannot find atrium.exe" means `extract_dir` and the archive layout disagree.

### M4. The workflow and the local publish do not both win

**Steps**

1. Run `bash scripts/cut-release.sh v0.1.0 --execute` and let it push the tag.
2. Watch the `release` workflow, which the push started.

**Expect** exactly one of the two creates the release and the other fails saying it already exists. That is
the designed outcome and not a bug. Prefer the workflow once it has worked once, and use `--from-tag` locally
after that.


## N. Which of these want me

`atrium peers` grouped by what each card wants. The states it separates are the ones that were repeatedly
confused when sixteen agents were running: finished, stopped and asking, asking while still working, and gone
quiet with nothing recorded.

Run these against a preview (`atrium preview --http <port> --from live`) so the asks land on a copy.

**Watch out for `ATRIUM_TASK_ID`.** A supervised session has it set, and `atrium ask` prefers it over `--name`,
so every ask typed from inside one lands on that session's own card whatever name you pass. Clear it for these
steps. This is correct behaviour and it will waste ten minutes if you forget.

### N1. The four states read differently

**Steps**

1. On a card whose session is running: `atrium ask "which schema is authoritative"`.
2. On a second one: `atrium ask --continue "should the postgres path be stubbed"`.
3. On a third: `atrium finish "wired the reaper to the pty"`.
4. Leave a fourth alone, with no hooks reporting for it.
5. `atrium peers --fleet`.

**Expect** four groups with different headings. The first card is under `STOPPED AND ASKING` with its question
printed. The second is under `ASKED WHILE STILL WORKING` and its card has NOT moved into a waiting column. The
third is under `FINISHED` with the first line of its recap. The fourth is under `NOTHING RECORDED`.

**Also expect** the groups in that order, most wanting a human first, and the longest wait first inside each.

### N2. A card in `done` is not a session that finished

**The failure this exists for.** Everything dragged to `done` by hand, adopted, or tidied by the sweep is in the
same column as a session that ran `atrium finish`. Pointing the first version of this at a real board buried
nineteen recaps under twenty five rows of old cards nobody had claimed.

**Steps**

1. Find a card in `done` that no session ever finished, or drag one there.
2. `atrium peers --fleet`.

**Expect** it is not listed at all. Only a card with a recap, or one an agent filed a `finished` for, appears.

### N3. Finished work ages out and blocked work does not

**Steps**

1. `atrium peers --fleet` and note the finished group.
2. `atrium peers --fleet --since 1m`.
3. `atrium peers --fleet --since bananas`.

**Expect** step 2 shows only what finished in the last minute, and every blocked or quiet card is still there:
the window is on finished work only. A session that has been blocked since yesterday is exactly what this list
exists to surface and must never age out. Step 3 refuses with a message naming the format, rather than quietly
using the default and looking like an answer.

### N4. A finished session is still not somebody to talk to

**Steps**

1. `atrium peers` with no `--fleet`, on a board with a finished card on it.

**Expect** the finished card is absent. The plain list is the addressing list for `atrium tell`, and offering a
session that has ended wastes a turn and produces a message nobody reads. It is still grouped and ranked.


## O. The board does not throw away where you were

**Read this before running any of it.** Every case here looks like it passes on a quiet board, because a quiet
board was never the problem. The board only repaints when something changes or the poll lands, so a board with
two idle cards on it will sit still whatever the code does. Each case below therefore says what has to be
MOVING while you look, and if nothing is moving you have tested nothing.

The easiest way to get movement without launching anything: leave a card selected in the detail dialog and
watch the ages tick, or start one short-lived shell fixture. `atrium preview --from live` gives you a copy of
the real cards to do it against.

### O1. Scrolling down stays down

**Steps**

1. Open the board with enough cards that a column scrolls. The `finished` column with `done` expanded is the
   usual one.
2. Scroll that column to the bottom.
3. Wait through at least four polls, which is twenty seconds, with at least one card running so ages tick and
   events arrive.

**Expect** the view has not moved. Not "moved and came back", which is what the old scroll-restore did and what
you would see as a jump: it does not move at all.

**Also** repeat it on the stack and on the terminal switcher, which are separate lists and were separately
broken.

### O2. A selection survives a card ticking

**The one that catches a half fix.** Reconciling rows by id but rewriting every matched row fixes the scroll
and leaves this broken, because the age changes every second and the card being rewritten is the card being
read.

**Steps**

1. Find a running card and drag-select the text of its title. Do not release.
2. Hold the selection across at least two age ticks.
3. Release, then leave the selection alone for another ten seconds.

**Expect** the selection is still there and still covers the same text, and the age beside it changed while you
held it.

### O3. Focus is not moved out from under you

**Steps**

1. Open the stack. Click into the search box and type a partial filter.
2. Leave it. Let several polls land, with something running.

**Expect** the caret is where you left it and the text is what you typed. The list under it filters and
redraws around the box.

### O4. A drag files ONE move

**The failure this exists for.** A reconciled board hands back the SAME card element after a repaint, with the
listeners it already had. Wiring them again on every render adds a second `drop` handler, and then one drop
files the move twice, a moment apart, against ranks that have already changed. It presents as a card that
jumps to a second position by itself, seconds after you let go.

**Steps**

1. Leave the board open for a minute with something running, so it has repainted many times.
2. Drag a card between two others in another column.
3. Watch it for ten seconds without touching anything.

**Expect** it lands once and stays. Check the card's timeline: one status change, not two.

### O5. Ticks in the file picker survive a repaint

**Steps**

1. Open a card's files. Tick three files in a directory with enough entries to scroll.
2. Scroll down. Wait for a poll.

**Expect** the three are still ticked, the download button still says three, and the scroll has not moved. This
one is a side effect of writing attributes rather than properties, and it is worth checking because it is the
cheapest evidence that a repaint really is not rebuilding rows.

### O6. Cards still arrive, leave and reorder

**The regression the fix could cause.** A reconciler that matches too eagerly shows you a stale board, which is
worse than one that flickers, and it will look calm while doing it.

**Steps**

1. Launch a card. Watch it appear.
2. Shelve it, unshelve it, and drag it between columns.
3. Change its title from the detail dialog.
4. Delete it.

**Expect** every one of those shows up on the board within a poll, in the right column, with the right text.
A card that arrives in the wrong place, keeps an old title, or refuses to leave is this fix failing.


## P. Saying only what was verified

Four places where the board reported an outcome it had not checked. Every one of them looks fine on a board
with one window open, so all four need a second window and one of them needs a pasted url.

### P1. A raised window is its own answer

**Steps**

1. Pop a supervised card out with the icon on its row. A second window opens with the terminal in it.
2. Go back to the board and press `attach` on that same card. Then click an alert toast for the same card.
3. Watch the window that comes to the front.

**Expect** the popped-out window is raised both times, and neither window draws a toast about it. A toast on
either side is the bug. The board says "it is in its own window" only in a browser with no `BroadcastChannel`,
where it cannot tell whether the raise landed.

### P2. A window the board did not open is admitted to, not lied about

**The failure this exists for.** A window is found again by name, and only a script-opened window has one. The
board would fail to find it, open a second window onto the same terminal, and say the first one was gone.

**Steps**

1. Copy the board's address, open a NEW TAB by hand, and paste `<address>/#term=<card id>` into it. Use a
   supervised card. The terminal attaches.
2. Go back to the board. Press `attach` on that card.

**Expect** a dialog titled "this terminal is open in another window", telling you to switch to that window, or close
it and pop the card out again. **No second window opens.** Two views on one terminal is the situation `docs/supervision-design.md`
says nothing arbitrates, so opening one here is a failure even though it looks helpful.

### P3. A stale claim still opens a window

**The other half of P2, and the reason it has to be asked rather than assumed.**

**Steps**

1. Pop a card out.
2. Close the popped-out window and IMMEDIATELY, within a couple of seconds, press `attach` on that card.

**Expect** a window opens, with `it was not there any more`. The claim was still inside its fifteen seconds, so
the board had to call the roll to find out nobody was answering.

### P4. An alert for a popped-out card raises it rather than stealing it

**Steps**

1. Pop a card out and leave the board on the terminals view with nothing attached.
2. Make that card ask for a permission, so the board raises a toast for it.
3. Click the toast.

**Expect** the popped-out window comes forward and the message lands in it. The board's own pane stays empty.
The failure is the terminal being pulled into the board's pane, leaving the popped-out window showing a
terminal it no longer has.

### P5. The notification test answers when nothing appeared

**Steps**

1. Turn OFF notifications for your browser in Windows Settings, System, Notifications. Leave the browser's own
   permission granted.
2. Open the gear, go to alerts, press the test button.

**Expect** `nothing appeared`, naming Windows as the likely cause. Before, the button was silent, and silent
reads as working. Turn notifications back on and press it again: `it is on screen`.


## Q. The login on the published board

Everything here needs a provider, which is what the Go tests cannot have. They run against a fake one that
checks its own side of the exchange, and that is worth a lot, but a fake provider agrees with whatever atrium
believes about the real ones. Nothing in this section has been run against a real provider yet.

### Q1. A sign-in, end to end. NOT YET RUN.

**Steps**

1. Register a client at a provider you administer, a Keycloak realm for instance. Set its redirect URI to the
   PUBLISHED address plus `/auth/callback`, which is not localhost.
2. Settings, `who may open it`. Fill in the issuer, the client id, the redirect and your own email address.
   Leave the secret empty if you registered a public client.
3. Start the share. Open the published address in a browser that has never signed in to that provider.

**Expect** the provider's login form, then the board. The authorize URL it sent you through carries
`code_challenge` and `code_challenge_method=S256`, which you can read in the address bar on the way past.

**Expect also** that the provider is happy with the exchange. A provider configured to REQUIRE PKCE and a
provider that has never heard of it must both work, because atrium sends the challenge either way and has no
switch to turn it off.

### Q2. The loopback board still never asks. NOT YET RUN.

**Steps** with the login configured and enabled, and a share running, open `http://localhost:7778` on the
machine itself.

**Expect** the board, immediately, with no login and no redirect. This is the one that breaks every hook on the
machine if it is wrong, and it breaks them quietly, because a hook that fails is designed never to fail a
session. `TestTheLocalBoardIsNotWrapped` asserts the construction. This asserts the machine.

### Q3. A session that ran out renews without a form. NOT YET RUN.

A session lasts twelve hours, which is a long time to sit and watch. To provoke it, sign in, then delete the
`atrium_session` cookie in the browser's dev tools and press a button on the board.

**Expect** the page to reload itself and come back signed in, with no login form, because the provider still
has a session for that browser and answered `prompt=none`. What you must NOT see is the board filling up with
red errors, which is what it did before it knew what a `401` meant.

### Q4. A provider that has forgotten you gets a form, not an error page. NOT YET RUN.

**Steps**

1. Sign in to the board.
2. Sign out AT THE PROVIDER, in another tab, so its session is gone and atrium's cookie is not.
3. Delete the `atrium_session` cookie and press a button on the board.

**Expect** the provider's login form. What you must NOT see is a page saying `the provider refused:
login_required`. A declined silent renewal is the expected answer rather than an error, and showing it as one
means every expired session lands on the word "refused" with no way forward.

### Q5. A login cannot send you somewhere else. NOT YET RUN.

**Steps** with the login enabled, open the published address with
`/auth/renew?back=//example.com` on the end, and sign in.

**Expect** to land on the board's own front page. The `back` value rides through a redirect to the provider and
comes back, so it is under an attacker's control from end to end, and a link that drops somebody on another
domain the moment they finish signing in is the oldest phishing primitive there is.
`TestALoginCannotSendSomebodyOffThisBoard` covers the parsing. This covers the browser, which is the only thing
that decides what `//example.com` means.


## R. Statusline telemetry

The endpoint is `POST /telemetry` on the agent listener. See `docs/statusline-telemetry.md` for the contract.

Nothing here needs a statusline script: every step is a `curl` you can run yourself, which is the point of a
contract precise enough to be implemented from.

### R1. A context figure lands on the right card

**Steps**

1. Pick a card with a runner on it. Read its `resume_id` from `GET /v1/tasks`, or open the card and use its id
   as `task_id`.
2. Post a figure:

```bash
curl -s -X POST localhost:7777/telemetry -H 'content-type: application/json' -d '{
  "session_id": "THE-RESUME-ID", "context_used": 184000, "context_window": 200000,
  "model": "Opus 5", "five_hour": {"pct": 91, "resets_at": "2030-01-01T00:00:00Z"}
}'
```

3. Watch the board for five seconds, which is its poll interval.
4. Hover the chip.

**Expect** the card grows a `ctx 92%` chip in the danger colour and an amber `5h 91%` beside it. The tooltip
says `184k of 200k tokens`, the model, the limit and when it resets, and how long ago the figure arrived.

**Also expect** the card did not move. Its status, its column and its idle clock are exactly as they were: a
statusline redraws when a terminal does, including when a human types in it, so a post is not activity.

### R2. A session atrium has never heard of

**Steps**

1. Post the same body with `"session_id": "not-a-real-id"`.
2. Read the response and the daemon's log.

**Expect** `{"ok":true}` and nothing recorded anywhere. No error, no log line, no card changed. A statusline
runs in every session on the machine and most of them are not on the board.

### R3. Nothing it is sent can fail it

**Steps**

1. Post `not json`, then `[]`, then `{"session_id":123}`, then an empty body.
2. Check every status code.

**Expect** `200` every time. A statusline that treats a non-2xx as a failure would surface one on every render,
and a terminal that stops drawing is the failure this posture exists to prevent.

### R4. It disappears on restart, and does not come back wrong

**Steps**

1. Post a figure to a card and see the chip.
2. `atrium stop`, then start the daemon again.
3. Look at the same card.

**Expect** no chip. The figure was never written down, because it described a process that the restart ended.
A card claiming `92% context` about a conversation that no longer exists is the failure mode the whole
never-stored rule is about.

### R5. It outlives the activity badge

**Steps**

1. Post a figure to a card whose session then goes quiet for twenty minutes.
2. Look at the card.

**Expect** the activity badge is gone, at fifteen minutes, and the `ctx` chip is still there, until thirty.
That difference is deliberate: an activity goes wrong fast, a context figure only grows, and an idle card is
exactly the one whose context decides whether you resume it.


## S. The switcher

Everything here is a browser question, which is why none of it is in `go test`. `scripts/check-switcher.js`
holds the shapes. What is below is what only a person in front of two windows can answer.

### S1. The key opens it, and the terminal never sees it

**Steps**

1. Attach to a supervised card and click INTO the terminal, so the runner has the focus.
2. Press `ctrl-shift-k`.
3. Press it again.

**Expect** the switcher opens over the terminal with the field focused, and the runner receives NOTHING: no
stray character, no cleared line, nothing in the scrollback. Pressing it again closes it.

**The failure this catches** is a handler that does not stop the event: the switcher opens and a control
character is typed into whatever the agent was doing.

### S2. Filtering, and the order with nothing typed

**Steps**

1. Open the switcher with several sessions running. Note the order.
2. Type part of a directory name. Then type part of a tag.
3. Press Escape. Go to a session. Open the switcher again.

**Expect** the filter matches the title, the worktree and the tags. At step 3 the session you just went to is
at or near the top of the recents, and the card you are currently showing is listed LAST with `here` on it.

### S3. Rebinding, and a browser that takes the key

**Steps**

1. Settings, `the board`, the switcher key. Press the button, then press `alt-k`. Try it.
2. Press the button, then press `ctrl-t`.
3. Press the button, then press `k` on its own.
4. `use the default`.
5. In FIREFOX, with the default bound, press `ctrl-shift-k`.

**Expect** `alt-k` binds and works. `ctrl-t` is refused with a reason, and no tab opens after it is refused.
A bare key is refused. At step 5 Firefox opens its Web Console, and atrium says the browser also took that key
and points at the setting. That message is the whole reason the binding is a setting.

### S4. Switching inside a popped-out window

**The one that matters.** A solo window IS one card, so switching there is not attaching, it is moving.

**Steps**

1. Pop a card out into its own window. Leave the board open on another card.
2. In the popped-out window, press `ctrl-shift-k` and go to a THIRD card.
3. Look at the popped-out window: its title bar, and what is in the terminal.
4. On the board, look at the session list.
5. On the board, click the card the window used to be showing.
6. In the popped-out window, press F5.

**Expect** the window is now driving the third card and its title bar says so. In the list, the arrow that says
"in a window of its own" has MOVED from the old card to the new one. Step 5 attaches in the board's pane
normally, with no "raised it for you" toast and no second window. Step 6 comes back on the third card, not the
one the window was opened on.

### S5. Two windows never land on one terminal

**Steps**

1. Pop out card A. Pop out card B. Two windows.
2. In window A, open the switcher and try to go to card B.
3. On the board, attach to card C. In window A, switch to card C.

**Expect** step 2 is refused, saying B already has a window. At step 3 the board LETS GO of card C, its pane
goes back to nothing attached, and it says so. Neither case ends with two live views on one terminal.


## T. A session asking another session for help

Two gated sessions on the board, on the same machine. `atrium peers` from either one names the other, and the
handle it prints is what these steps mean by `<them>` and `<you>`.

### T1. A question reads as a question, not as a note

**Steps**

1. Put something in `why am I doing this` on a card, from the board. A sentence you will recognise.
2. In that session: `atrium ask --continue "which of these two schemas is authoritative"`

**Expect** the card shows BOTH: the question labelled **this agent has a question**, and the `why` you typed,
still there and unchanged. The card does NOT move, because `--continue` says the session is carrying on. If the
`why` is gone, the ask is writing to the wrong field again.

### T2. A blocked ask moves the card and says what it is waiting for

**Steps** in a session: `atrium ask "which branch is base"`

**Expect** the card moves to waiting, wears an `asked you` chip, and sorts ABOVE cards that merely finished
their turn under `waiting on you`. The desktop alert, if alerts are on, says the card asked you something
rather than that it is ready.

### T3. Saying anything to the card answers it

**Steps** send a message to that card from the board.

**Expect** the question comes off the card. Typing the same answer into the TERMINAL instead must leave it
there: atrium cannot see the terminal, and pretending otherwise would clear questions nobody answered.

### T4. A question routed to a peer

**Steps** in one session: `atrium ask --peer <them> "which branch is base for the release notes"`

**Expect**

- Your card says **asked `<them>`** rather than "this agent has a question", and shows an `asked a peer` chip.
- Nothing was typed into the other session's terminal. Watch it: the question must NOT appear at its prompt.
- The other session receives it on its next tool call or at the end of its turn, with `atrium answer <you>` in
  the message.

### T5. A handle nobody has refuses, and teaches

**Steps** `atrium ask --peer nobody-here "anything"`

**Expect** it refuses, prints the handles that WOULD have worked, and says nothing was asked. Then check the
card: it must not be asking anything and must not have moved to waiting. A refused route that still filed the
card would have it claiming a peer owes it an answer when nobody does.

### T6. The answer coming back

**Steps** in the other session: `atrium answer <you> "base is main"`

**Expect** the asker's card stops asking. The answer is queued for it, arrives with the original question
quoted back, and the card returns to running when the session actually reads it, not before.

### T7. A finished card is not still asking

**Steps** ask something, then `atrium finish "worked it out on my own"` in the same session.

**Expect** the card is in done with the recap, and the question is gone. A card in done that is still asking
makes the field meaningless.


## U. Sending work to another machine

The outward half of federation. `docs/remote-launch.md`. All of it needs two atriums, so the second one can be
another daemon on this machine with its own database and port, started as a room pointing at the first.

Set up once, on the machine that will be the room:

```powershell
atrium daemon --http :7878 --addr :7877 --db $env:TEMP\room.db --location-file $env:TEMP\room.json
atrium room --hub http://localhost:7778 --name testroom --board http://localhost:7878 --local http://localhost:7878
```

### U1. A queued item starts on the other machine

**Steps**

1. On the hub: `atrium dispatch to testroom --runner claude --prompt "say hello and stop"`.
2. Watch the room's log.

**Expect** within twenty seconds the room logs `started claude for the hub`, a card appears on the room's own
board at :7878, and `atrium dispatch list --all` on the hub shows the item as `running` with a card id.

**Expect** no card is created on the HUB. The work is on the other machine and the hub keeps a pointer, never a
copy.

### U2. A directory that is not there fails immediately, with the reason

**Steps**

1. Restart the room with `--workspace $env:TEMP`.
2. `atrium dispatch to testroom --runner claude --dir $env:TEMP\nothing-here`.

**Expect** the item goes `failed` within twenty seconds and the row says `is not a directory on this machine.
atrium does not create worktrees`. Nothing starts. This is the one that matters: a card that fails readably
beats one that starts in the wrong place.

### U3. A room with no workspace refuses any directory the hub names

**Steps**

1. Restart the room WITHOUT `--workspace`.
2. `atrium dispatch to testroom --runner claude --dir $env:TEMP`.

**Expect** `failed`, and the reason names `--workspace`. A hub may not name a directory on a machine that has
not said it may.

### U4. A directory outside the workspace is refused

**Steps** With the room on `--workspace $env:TEMP\inside`, dispatch with `--dir $env:TEMP\outside`.

**Expect** `failed`, saying it is not inside the workspace. Try the sibling case too: a workspace of
`$env:TEMP\work` and a directory of `$env:TEMP\work-elsewhere` must be refused.

### U5. A room started with --no-launch is handed nothing

**Steps**

1. Restart the room with `--no-launch`.
2. Queue an item for it and wait a minute.

**Expect** the item stays `queued`, not `failed`. The rooms pane says that room takes no work. The item's
attempts must still be zero: a room that was never going to run it does not get to spend its retries.

### U6. An item cannot be withdrawn once a room has taken it

**Steps** Queue an item, wait for the room to claim it, then `atrium dispatch cancel <item>`.

**Expect** a refusal that says to stop it on that machine. Cancelling BEFORE the room checks in must succeed,
and the withdrawn item must never be handed out afterwards.

### U7. The queue survives a hub restart

**Steps**

1. Stop the room, so nothing collects.
2. Queue two items for it. `atrium stop` the hub, start it again.
3. `atrium dispatch list`.

**Expect** both items still there and still `queued`. Start the room and they run. A promise that evaporates on
restart is not a queue, which is why this table is durable while the room list beside it is not.

### U8. A stale result is refused rather than overwriting

**Steps**

```powershell
curl.exe -s -X POST http://localhost:7778/v1/dispatch/<item>/result `
  -H "Content-Type: application/json" -d '{\"token\":\"nonsense\",\"ok\":true,\"card_id\":\"x\"}'
```

**Expect** 409, and the row unchanged. The token is the whole of the authorization and there is no room
identity to trust. If this ever answers 200, anything that can reach the board can mark somebody's queue item
started.


## V. What the round 1 to 8 review turned up, fixed

Every scenario here is a defect the operator found by using the board, written up in `docs/dispatch-queue.md`
and then fixed. They are the ones a Go test cannot answer: two browser windows, a restart, a light skin.

### V1. Two windows on one terminal are refused

**The failure this replaces.** A `#term=` url pasted into a tab attached a second viewer. The pty was then
sized to the smaller window and the LARGER one drew every wrapped line on top of itself, so
`accepting newlines for some reason` read as `acceptingenewlineshforisomewreason`. The small window looked
perfect, so the window being typed in read as the broken one.

**Steps**

1. Attach to a supervised card in the board's own pane.
2. Copy the board address, open a NEW TAB by hand, and paste `<address>/#term=<card id>` for that same card.

**Expect** the new tab refuses to attach and says the card is open in another window. It offers **leave it
there** and **take it anyway**.

3. Press **take it anyway**.

**Expect** the new tab attaches, and the ORIGINAL pane tears itself down saying another window took the
terminal. One viewer, always.

4. Close the tab that took it. Within a second or two, press `attach` on that card from the board.

**Expect** it attaches with no refusal. The claim is answered by a roll call rather than by a fifteen second
timer, so a window that was closed without releasing does not lock a card out.

### V2. A lent session's address says what it is

**Steps** share a session, then open the share address with the `#term=<id>` fragment REMOVED.

**Expect** a page saying what the address is for and that the link needs its fragment back. Not an empty board.

**The failure this replaces** is that it drew the board's own chrome with every list empty, because the guest
handler refuses `/v1/tasks` on purpose. It looked exactly like atrium being broken, and the first person to see
it asked whether it was a clone of his own board.

5. On the terminal at the correct address, try to paste a picture, and try to drag a file onto it.

**Expect** it says `this link is one terminal. nothing else here is shared.`, which is the daemon's own
sentence. NOT `that did not go up`, and not the word `Forbidden`.

### V3. Scrollback survives a restart

**The one to run properly, because it is the reason the whole round exists.**

**Steps**

1. Attach to a supervised card and let real output pile up in it. Scroll up and confirm it is there.
2. Stop the daemon PROPERLY with `atrium stop`, not a kill. The log says `saved scrollback for N card(s)`.
3. Start it again. The log says `<card> starts with N bytes of scrollback from before the restart`.
4. Open that card's terminal.

**Expect** the output from before the restart is there, above a dim `atrium restarted here` divider.

5. Close the window entirely and open the terminal again from the stack page.

**Expect** it is STILL there. This is the half that failed before: the daemon holds the copy now, so closing
the last open page no longer discards it.

**Expect also**, if your window is at a very different width than the one that produced the output, that it is
dropped rather than replayed. Bytes composed for another width are what make an attach unreadable.

**What it does NOT do:** survive a kill. `taskkill` on the daemon never runs the wind-down, so nothing is
written. That is a stated limit, not a defect.

### V4. Two questions both survive

**Steps** in a gated session:

```powershell
atrium ask --continue "which of these two schemas is authoritative"
atrium ask "which branch is base"
```

**Expect** the card shows BOTH, and the `why` you wrote is still there and unchanged.

**The failure this replaces** is that the second `ask` overwrote the first and nothing anywhere recorded that
the first had ever been asked.

3. Answer one of them from the board.

**Expect** the other is still standing, and the card is still asking.

4. Ask a peer as well: `atrium ask --peer <them> "..."`. Have that peer run `atrium answer <you> "..."`.

**Expect** the peer's answer settles ONLY the question that was routed to it. A question left for a human is
not answered by a peer who never saw it.

5. Ask eleven things without answering any.

**Expect** ten outstanding, and the OLDEST retired with a note saying it was dropped because there were too
many. The newest survives, because it is the one the session is stopped on.

### V5. The question is a control

**Steps** with a card that is asking something, click the question on the stack row.

**Expect** the card opens with the caret in the box for saying something to it.

6. Open the card dialog directly.

**Expect** the question is IN the dialog, beside what it says it did, read only, naming the peer when it was
routed to one.

### V6. The card dialog says that it saves

**Steps** open a card, type into `why am I doing this`, and look at the buttons.

**Expect** `save and close`, not `close`, and a line saying fields are kept when you leave them.

### V7. `ctrl-shift-v` pastes straight through on loopback

**Steps** on `localhost`, attach a terminal, copy some text, press `ctrl-shift-v`.

**Expect** it pastes. NO box.

7. Do the same over a share.

**Expect** the box, after about a second. The clipboard permission is granted per origin and a share is a new
origin, so the read never settles there and the box is the way through.

### V8. The restart banner is readable

**Steps** with a LIGHT skin, and with no terminal attached in the pane, trigger a restart.

**Expect** the `atrium is restarting` banner is legible. It was pale text on a near-white pill.

**The failure this replaces** is worth knowing because it was fixed once before and came back: detaching
cleared one theme variable and left two behind, so the pane carried half of the last terminal theme.

### V9. The zrok account toggle

**Steps** gear, `expose the board`. Press `give atrium its own`, then press `use this machine's zrok`.

**Expect** it switches. It used to refuse with `that environment is already enabled against
https://api-v2.zrok.io/. disable it first...`, because the address it was comparing differed by a trailing
slash.

**Expect also** that a refusal for a REAL move still happens, and now names the environment and both addresses.

### V10. A refusal is not buried

**Steps** `atrium ask --peer nobody-here "anything"`.

**Expect** the refusal, the list of handles that would have worked, and `nothing was asked.` Nothing else. No
usage block, no repeated error line.

8. Then `atrium ask --url http://localhost:9 "anything"`.

**Expect** the usage block IS still printed, because that failure has no sentence of its own. Suppressing it
everywhere would make a real failure silent.

## W. Scrollback survives being looked at

Three bugs stacked on one symptom, and each fix uncovered the next. Every scenario here is one of them, so a
regression in any single scenario reads as the whole thing being broken again.

### W1. Resizing a window keeps the scrollback

1. Open a supervised terminal that has been running long enough to have real history in it. The atrium card
   itself is the honest case: scroll up and confirm you can read work from earlier.
2. Drag the browser window narrower by a couple of inches, or change the browser zoom one step.
3. Scroll up.

**Expected:** everything that was there before is still there. Above it, one grey line naming what the history
was drawn for and what the terminal is now.

**The bug:** the pane emptied and left `earlier output was written for a terminal of another width and cannot
be redrawn here`. The width mark is laid before the pty is told its new size, so the run of output composed at
the new width was zero bytes old.

### W2. Resizing twice does not cut the history to the last resize

1. Same terminal. Resize it, wait for the agent to draw something, then resize it again.
2. Scroll up as far as it goes.

**Expected:** history from before BOTH resizes. The grey line says the session was resized while it ran and
names each width in the order they happened.

**The bug:** the second attempt returned the trailing run of output, and a run ends at every width mark. Only
what was drawn since the last resize came back, which for a quiet agent is a page or two. It looks exactly like
W1 and is a different fault.

### W3. The replay cannot erase itself

1. Attach to a card whose agent has redrawn its own block many times, which is any claude session that has
   been working for a while.
2. Scroll up through the whole buffer.

**Expected:** every line of history is readable and in order, and a grey line marks where the flattened
history ends and live output begins. Text that redrew in place, a spinner or a progress bar, appears as each
version in turn rather than only the last one. Colour is intact.

**The bug:** the daemon sent two megabytes and the browser showed two screens. The history is full of absolute
cursor moves and erase-in-line sequences, so each replayed redraw landed on the history rather than on the
older version of itself. Measured on one card: 33,957 cursor moves and 1,879 erases in 1.6MB.

**Also check:** live output after the boundary line still renders normally. A terminal user interface must
work as it always did from the moment you are attached, since only history is flattened.

### W4. Scrollback survives a clean restart, and says a restart happened

1. Note what is on screen in a supervised terminal.
2. Restart the daemon with `atrium stop` and bring it back, or use `restart_atrium`. It must be a STOP: a kill
   skips the wind-down and there is nothing to save the buffer.
3. When the card comes back, attach and scroll up.

**Expected:** the output from before the restart, then a grey divider saying atrium restarted here, then the
new session. The divider is the point: without it the join reads as the agent repeating itself.

**Also check:** a session that was resized before the restart still gets its history. That case used to be
declined outright, on the grounds that a file holds one width and cannot carry a warning. It can, because the
warning is drawn by whoever replays it.

### W5. A kill loses it, and nothing pretends otherwise

1. `taskkill` the daemon rather than stopping it.
2. Bring it back and attach.

**Expected:** whatever the last clean stop wrote, or an empty terminal if there has never been one. No claim
that history exists.

**Why it is in the plan:** two restarts in a row went this way because `POST /v1/shutdown` refuses while a
share is running, so the wind-down never ran and the carryover was never written. The scrollback fix looked
broken when it was working. See `docs/dispatch-queue.md` group T.

### W6. A restart reopens the terminals that were open

1. Note which cards have a live terminal. The terminals tab counts them.
2. Open a terminal on a card that is NOT a fixture, so the case being tested is the one that used to fail.
   Unshelving something, or launching from a directory, both do it.
3. Stop the daemon and bring it back. A stop, not a kill.
4. Look at the terminals tab.

**Expected:** every card that had a terminal has one again, with its scrollback and a restart divider in it.
Fixtures come up first and are pinned and themed as they always were.

**The bug:** only fixtures came back. Six terminals went down and four returned, and the two that did not were
the two nobody had written a fixture for.

**Also check:**

- **A shelved card does not come back.** Shelve one, restart, and it stays down. Putting work down is a
  standing no and a restart must not undo it.
- **A card whose worktree has been deleted** logs a line and does not stop the others reopening.
- **Closing everything and then stopping** leaves nothing to reopen. An empty list is written rather than
  skipped, so the previous list cannot come back to life.

### W7. Raising the scrollback limit works without a restart

1. Settings, scrollback. Note the current megabytes.
2. Raise it and save.
3. Open a terminal that was already running before the change.

**Expected:** the buffer for that runner is now the new size, holding everything it already held.

**The bug:** the size was read once at spawn, so raising it did nothing until every runner had been restarted,
which is the thing somebody raising it is trying to survive.

**Also check:** LOWERING it does not throw scrollback away. It applies to runners started afterwards, the way
it always did.

### W8. Hitting the limit is announced

1. Set the scrollback to its minimum and start a runner that produces a lot of output.
2. Let it produce more than the limit.
3. Attach and scroll to the very top.

**Expected:** a grey line saying THIS IS NOT THE START OF THE SESSION, naming the limit and where to raise it.

**Why it matters:** without it a scrollback that stops reads identically whether it is complete or truncated,
and an hour lost to a kill got reported as the buffer being too small.

### W9. A terminal shows one session, and the older history is a click away

1. Attach to a card that has been through at least one clean restart.
2. Scroll to the top of the terminal.
3. Open the terminal's cog and choose **history from before the restart**.

**Expected:** the terminal holds only what this process has produced, the way a terminal running claude does.
The cog item opens a tab of plain text holding what the terminal said before the last stop, oldest first, with
a restart divider between each generation and no escape codes.

**The bug:** the carried scrollback was joined onto the front of every attach. A resumed session reprints its
own recent history, so the last hour appeared twice with nothing to say why, and every attempt to fix the
scrollback made that worse rather than better.

**Also check:**

- **A card with nothing saved** answers with the reason rather than an empty tab. The file is written when
  atrium is stopped, so a card whose daemon was killed has none.
- **It reaches back further than one restart.** Several dividers in one file, because each stop folds the
  previous generation into the one it writes.
- **A lent session cannot reach it.** Open a share and request the older scrollback route against it. The
  answer is 403 and the message says the link is one terminal.

### W10. A reopened terminal comes up the right width

1. Note how wide the browser window is. Attach to a card and let the agent draw something.
2. Stop atrium and bring it back.
3. Attach to that card and scroll through the newest generation.

**Expected:** no note saying the history was drawn for a terminal of another width, and no stretch of output
breaking a third of the way across the window.

**The bug:** every terminal opened at 120 columns and was resized by the first browser to attach, so every
restart put a stretch of 120 column output into the scrollback. The operator saw one width-mismatch note per
restart.

**Also check:** a card that has never had a terminal still opens at 120, and a card whose runner exited before
any viewer said how big it was does not have its recorded width overwritten with zero.

## X. Choosing a model when you launch

The whole feature is one flag on a command line, so the risk is not that it fails. It is that it succeeds at the
wrong thing, and every way it does that is invisible on screen. What you see instead is output that reads
differently, or a bill.

Two readings have to hold at once and they sound contradictory. **One time with respect to the runner**: the
control is off every time the form opens and nothing is written to the runner's row. **Sticky with respect to
the card**: a session that started on a model stays on it, across restarts, for its own lifetime.

### X1. The control starts off and goes back to off

1. Open the new agent form. The model tickbox is unticked and there is no name box.
2. Tick it. A name box appears with the cursor in it. Type a model and start the session.
3. Open the form again.

**Expected:** unticked, with nothing in the box, every time. Open the runners page and check the claude row:
its model arguments are `--model` and `{model}`, and nothing about the model you chose is on it.

**The bug this prevents:** a one-time choice you have to remember to turn back off is not a one-time choice. It
becomes a setting, and the session you start next week is on a model you picked for a different reason.

### X2. Unticking clears the name

1. Tick the box, type a model, then untick it without starting.
2. Tick it again.

**Expected:** empty. A name left sitting behind an unticked box reads as off and is not.

### X3. The card says which model, and only when one was chosen

1. Start two sessions in the same directory on the same runner, one with a model named and one without.

**Expected:** the one you chose for wears a chip with the model name on it. The other wears nothing, and looks
exactly as every card did before this existed.

### X4. The model survives a restart. THIS IS THE ONE THAT GOES WRONG SILENTLY

1. Start a session with a model named. Leave it attached.
2. `atrium stop`, then start the daemon again.
3. When the card comes back, look at its chip, and ask the session what model it is running as.

**Expected:** the same model. The card is reopened by rebuilding a launch out of it, so a model that was not
written down would revert to the runner's default here, with nothing on screen saying so.

**Also check:** the decision log entry for the relaunch records the model.

### X5. A runner that cannot take a model says so

1. Open the new agent form and pick the shell runner.

**Expected:** the model control is not there at all. A shell has no model and would try to execute the flag, so
offering a tickbox that can only produce a refusal is worse than offering nothing.

2. From a terminal: `atrium launch --runner shell --model claude-opus-5`

**Expected:** refused, and the message says to put `{model}` in the runner's model arguments. NOT started on
the default.

### X6. The names offered are a history, never a catalog

1. Start sessions on two different models, then open the form and click into the name box.

**Expected:** both names offered. Typing something that is not on the list is accepted: atrium does not know
which models exist and must never decide.

### X7. Nothing changed for a launch that names no model

1. Start a session the way you always have, without touching the control.

**Expected:** identical to before. Same command line in the decision log, no chip on the card, and the runner
on its own default.

## Y. Rooms, tested with two local accounts

Rooms were built for two machines, and a second local account gives you four of the things a second machine
gives you: its own home directory, its own `~/.atrium` and database, its own address file under its own
`%LOCALAPPDATA%`, and its own hooks. So this runs the whole rooms path without a second box.

It is also the safe way to run two daemons here. Two daemons under ONE account fight over the address file, and
`docs/preview-design.md` says what that costs: the second one silently takes every hook on the machine, and the
symptom is not an error, it is activity arriving at a board nobody is looking at. Different accounts have
different `%LOCALAPPDATA%`, so the files cannot collide.

**What this does NOT test, and it is the interesting half:** network latency, the overlay transport, a machine
that goes away and comes back, and clocks that disagree. Rooms exist to survive those. This proves the protocol
and the board and nothing about the network, so it replaces the tedious part of a two-machine test, not the part
worth doing.

Set up once. Make a second local user, log in as it, and install atrium there too.

As you, the hub, on the ordinary ports:

```powershell
atrium daemon
atrium name hub
```

As the second account. PORTS ARE PER MACHINE EVEN THOUGH EVERYTHING ELSE IS PER ACCOUNT, so this daemon must be
given its own:

```powershell
atrium daemon --addr :7877 --http :7878
atrium name second
atrium room --hub http://localhost:7778 --name second --board http://localhost:7878 --local http://localhost:7878
```

### Y1. Two daemons, and neither has taken the other's hooks

1. As each account, start a session on its own board and let it call a tool.

**Expected:** each session's card, activity and permission requests land on the board belonging to the account
it is running as. Nothing from the second account appears on your board except through the room, below.

**The bug this prevents:** one address file serving both accounts, which looks like your board going quiet
rather than like an error.

### Y2. The room reports itself, and its cards

1. On your board, open the runners page and find the rooms pane.

**Expected:** `second` is listed within twenty seconds, with its host, its version and a row per session on it
saying what that session is doing and what state it is in. The rooms pane is where they appear. They are not
cards in your columns, because they are not yours to act on.

2. Stop the second account's `atrium room` process and wait.

**Expected:** after a minute the room reads as stale, which says it was here and cannot be seen now, and after
ten it is gone from the list entirely. Its sessions do not linger in your rooms pane as a claim about an account
nobody can reach.

### Y3. A permission request crosses, and the answer crosses back

1. In the second account's session, ask for something that gates.
2. On YOUR board, the room's row says an agent there is frozen. Follow it to the perms tab and answer there.

**Expected:** the request appears within a couple of seconds, because a room holding a pending request checks in
every two seconds instead of every twenty. Approving releases the agent in the other account. The decision, and
any rule it creates, are written in THAT account's database, not yours.

### Y4. Attaching is not federated, and says so

1. In the rooms pane, click the name of a session running on `second`.

**Expected:** it opens that room's own board at `http://localhost:7878` on that terminal, rather than attaching
here. The row says its terminals stay on that machine. A pseudo terminal belongs to the process that made it,
and that process is running as another user, so this is the answer rather than a gap to file.

### Y5. Your daemon does not run anything as the other account

1. From your board, launch a session in a directory that only the second account can read.

**Expected:** it fails on permissions, as the account YOUR daemon runs as. A runner starts under the token of
the daemon that launched it, so one daemon supervising another account's work is not a thing atrium does.
Dispatch the work to the room instead (section U) and the second account's daemon starts it as itself.

### Y6. A script running as you can still find the other daemon

1. On the second account, set `shared_location` to a path both accounts can read.
2. Restart that daemon, then as YOU read the file at that path.

**Expected:** the second daemon's address is in it, and you can reach `http://localhost:7878` from a script that
had no way to read that account's per-user address file. Clear the setting and only the per-user file is
written, which is right for a machine where the daemon and its callers are the same person.

## AA. Runner setup

What stops a runner working in the folders atrium launches it in, and the fixes. See
`docs/runner-setup-design.md`. Run these against a throwaway room whose account has its own home, or with
`GEMINI_CLI_HOME` set on the gemini row to a scratch directory, never against a `~/.gemini` somebody uses.

### AA0. Set up, once

1. Install gemini (`npm install -g @google/gemini-cli`) and add a runner row whose command is `gemini`.
2. Add a provider under runners, providers, with a root, and turn worktrees on with a worktree root.

### AA1. The chip counts what is wrong

1. Open runners, runners.

**Expected:** the gemini row carries `setup: N to fix` in the attention color. The claude row carries `setup ok`
or its own count. A row whose command is `ollama` carries no setup chip at all.

### AA2. Trusting a root once

1. Press the gemini row's setup chip. The `trusts the workspace` line reads `fail` and lists both roots, each
   with a `trust it` button.
2. Press `trust it` beside the worktree root and confirm.

**Expected:** a toast names the backup file. `trustedFolders.json` gains one `TRUST_FOLDER` line for that root
and every line that was there before is unchanged. `trustedFolders.json.atrium-original.bak` and
`.atrium-last.bak` sit beside it. The line now lists only the other root.

### AA3. A new worktree is trusted at launch

1. With the worktree root NOT trusted (restore the original backup), launch a gemini card in a new worktree under
   it.

**Expected:** gemini starts without its trust prompt. The room log says `trusted <folder> for gemini`. The file
gains exactly one line, for that worktree, and launching there again writes nothing.

### AA4. Outside every root, and a no, are left alone

1. Launch a gemini card in a directory outside every provider root.
2. Add `"<a worktree>": "DO_NOT_TRUST"` to the file by hand and launch a card in that worktree.

**Expected:** gemini asks its own trust question both times and the file is not touched. The setup dialog
explains a `DO_NOT_TRUST` root with no fix button.

### AA5. Sign-in is explained, never applied

1. With `selectedType` unset in gemini's `settings.json`, open the setup dialog.
2. Put `GEMINI_API_KEY` in the gemini row's env and reopen it.

**Expected:** step 1 reads `fail` with `gemini` and a copy button, and no fix button. Step 2 reads `warn` and says
atrium does not hold credentials, and the key's value appears nowhere on the board.

### AA6. Claude hooks through the same dialog

1. Remove one atrium hook from `~/.claude/settings.json` in the throwaway room's home.
2. Open the claude row's setup chip and press `wire them`.

**Expected:** the hooks line goes from `fail` to `ok`, and the hooks dialog agrees, since both read the same
report.

### AA7. A room's fix lands on that room

1. On a hub with two rooms, each with a gemini row, press `trust it` on the second room's row.

**Expected:** the trust file changes on the second room's machine only.

## AB. Agent-to-agent reliability, stage 1

A session launched by another session (`atrium_launch`, the `origin:agent` tag) owes its launcher a report every
turn. A turn that ends without one is a silent stop: the launcher is told, then the board on a backoff. Atrium
never forces a turn. See `docs/a2a-reliability-design.md`.

Run these in a throwaway hub and room, never against the live board. Start one session in the throwaway room as
the launcher, and launch workers from it with `atrium_launch`. Shorten the thresholds for the test with
`ATRIUM_A2A_SILENT_STOP=30s` and `ATRIUM_A2A_LONG_TOOL=1m` in the room daemon's environment.

### AB1. A worker that stops without reporting is reported, and never pushed

1. From the launcher, `atrium_launch` a worker with the prompt `print the date, then stop. do not call any atrium
   tool.`
2. Watch the launcher's terminal and the worker's card.

**Expected:** the worker ends its turn and stays in needs-input. It does not start another turn. Within seconds
the launcher receives `<worker> ended its turn without reporting ... card <id>`, from the worker's handle. On the
board, a minute after the stop, a notification reads `<worker> is STUCK: it stopped without reporting, 1
minutes`, with `launched by <launcher>` as the body, and the card shows a `stuck` chip.

**Common failure:** the worker's session was started without atrium hooks, so no Stop fires. The launcher then
hears at `ATRIUM_A2A_SILENT_STOP` from the watchdog instead of at once. That is the fallback working, not a bug.

### AB2. One notice per prompt, and a report or a message pays the turn

1. Let AA1's worker stop silently a second time without prompting it again (for example, run the room's Stop hook
   by hand for it). Then prompt it once more and let it stop silently.
2. Launch a new worker with `do nothing, then call atrium_report with status done and no_commit "test"`.
3. Launch another with `do nothing, then atrium_say your launcher "done"`.

**Expected:** step 1 gives exactly one more notice, for the new prompt, not one per Stop. The worker from step 2
gives the launcher one `report from <worker>: done` message and no silent-stop notice. The worker from step 3
gives the launcher only its own `done`.

### AB3. A human's card is untouched

1. Start a session by hand in the throwaway room, with the Stop hook installed. End a turn.

**Expected:** no notice to anybody, no escalation, and the Stop hook answers exactly as it did before.

### AB4. An incomplete report is refused in the same turn, and an unverified sha is flagged

1. From a worker, call `atrium_report` with `status: done` and no sha.
2. Call it again with `sha: 0000000` (a commit that does not exist).
3. From another worker, call it with `status: blocked` and no `ask`.

**Expected:** step 1 is refused naming `sha` or `no_commit`, and the card does not move. Step 2 is accepted, the
card goes to done with a `sha unverified` chip, and the launcher's report says `unverified`. Step 3 is refused
naming `ask`. `atrium finish` from a hand-started session with no sha still works as before.

### AB5. A stuck permission rings on the backoff

1. Turn auto mode off. Launch a worker whose first step is a gated command. Do not answer.

**Expected:** board notifications titled `<worker> is STUCK on a permission, n minutes` at 1, 2, 5 and 10
minutes. The launcher is not asked to answer it. Answering it stops the nag. With auto mode on, the same launch
raises no prompt and nothing rings.

### AB6. A stuck tool is reported, not killed

1. Launch a worker that runs `Start-Sleep 600`, with `ATRIUM_A2A_LONG_TOOL=1m`.

**Expected:** at a minute the launcher gets one `has been in one Bash call for 1 minutes` notice. The board rings
at 1 and 2 minutes after that. The sleep keeps running. When it ends and the worker moves on, the `stuck` chip
goes and the next stuck episode starts from 1 minute again.

### AB7. A message nothing will deliver says so at send time

1. Start a gemini session by hand in the throwaway room, so atrium does not own its terminal. `atrium_say` to it
   from another session.
2. Start a claude session from a shell whose settings have no atrium hooks, and `atrium_say` to it.

**Expected:** the gemini send answers `undeliverable` with a note naming the runner and the alternatives
(relaunch under atrium, or ask the human to relay it). The claude send answers `queued-unconfirmed` with a note.
Both messages are still in the card's queue.

### AB8. The Stop hook rides along on an agent launch only

1. On a machine with no Stop hook in `~/.claude/settings.json` but the other atrium hooks installed, launch a
   worker with `atrium_launch`. Read the `launched` event on its card, then check its process command line.
2. Start a session from the board's dialog.
3. Install the Stop hook from the hooks pane and launch another worker.

**Expected:** the worker's claude command line starts with `--settings` holding one Stop hook,
`<atrium> turn --event end`, where `<atrium>` is the binary the other hooks use. The board's session has no
`--settings`. With the Stop hook installed, no `--settings` is added, since Claude Code already runs it.

### AB9. Lineage is recorded

1. Launch a worker from the launcher, and a session from the board's dialog.

**Expected:** `GET /v1/tasks/<worker>` shows `spawned_by` as the launcher's handle and `spawned_by_id` as its card.
The dialog's card shows `spawned_by: "@human"`. Reopening the worker's card does not change either.

### AB10. The Stop hook survives an unreachable daemon

1. Stop the throwaway room daemon. End a turn in a launched worker.

**Expected:** the turn ends normally. No hang, no error shown to the model.

## AC. Seen tracking

Whether the operator has seen a card's latest turn, and whether its Open Questions are answered. See
`docs/seen-design.md`. Run against a throwaway room with the Stop hook (`atrium turn --event end`) and the
`UserPromptSubmit` hook wired, and two supervised sessions on it: `asker` and `peer`.

### AC1. A turn nobody saw is marked

1. With the board on another tab, have `asker` end a turn with plain text and no questions.

**Expected:** the `asker` card and its terminal strip row carry a teal dot. No `?` chip.
`atrium_task` with `card` empty, called from `asker`, answers `seen.unseen: true`.

### AC2. Looking at it clears it, passing through does not

1. Attach `asker` and click away to another card within a second.
2. Attach `asker` again and stay on it, scrolled to the bottom, for 3 seconds.

**Expected:** the dot survives step 1 and clears during step 2. `seen.seen_via` is `viewed`.

### AC3. A window behind another one does not count

1. Pop `asker` out into its own window. End another turn on it.
2. Put the board window in front of the popped-out one and wait 10 seconds.
3. Scroll the popped-out terminal up by a screen, bring it to the front, and wait 10 seconds.
4. Scroll it back to the bottom.

**Expected:** the dot survives steps 2 and 3 and clears about 3 seconds into step 4.

### AC4. Open Questions are recorded and answered by a reply

1. Have `asker` end a turn with:

   ```
   Open Questions:

   ---

   1. first question
   2. second question
   ```

2. Hover the `? 2` chip. Then attach and read it, without typing.
3. Type a reply into `asker` and submit it.

**Expected:** step 1 draws `? 2` beside the dot, and the tooltip lists both questions. Step 2 clears the dot and
leaves `? 2`. Step 3 clears `? 2`. `atrium_task` answers `answered: true` and no `open_questions`.

### AC5. A turn with no block keeps the questions

1. Repeat AC4 step 1. Then have `peer` send `asker` a message with `atrium_say`, so `asker` runs a turn that ends
   with plain text.

**Expected:** `? 2` stays. The peer's message typed into `asker` does not clear it, and neither does the turn it
started. `atrium_peers` from `peer` lists `asker` with `unseen: true, open_questions: 2`.

### AC6. A message from the board answers

1. Repeat AC4 step 1. Send `asker` a message from its card dialog.

**Expected:** `? 2` and the dot both clear, and `seen.answered_via` is `message`.

### AC7. Typing into the terminal sees the turn

1. End a turn on `asker` with the board attached to it but scrolled up.
2. Type one character into the terminal and delete it.

**Expected:** the dot clears at the keystroke, with `seen_via: typed`. A keystroke into the card's shell instead
does not clear it.

### AC8. It survives a restart

1. Leave `asker` with a dot and `? 2`. Restart the throwaway room.

**Expected:** both marks are still there after the restart. Typing into the terminal clears the dot as in AC7.

### AC9. The hook never costs a turn

1. Point `transcript_path` at a file that does not exist (run the hook by hand with a payload), and at a
   transcript over 50 MB.

**Expected:** the hook prints nothing and exits 0 both times, inside its 2 second budget. The card records the
turn and keeps whatever questions it had.

## AD. Event sink, stage 2

Opt-in routing of event kinds out of the db, and the offline compact. See `docs/backlog-2.md`, first section. Run
against a throwaway room. Never point any of this at the live `atrium.db`.

### AD1. The default is unchanged

1. On a throwaway room with no `event_sink` and no `event_cold_kinds`, approve one permission on a card.
2. Open the card's detail dialog.

**Expected:** the history shows the permission row. Nothing under the filters says anything is not shown.

### AD2. Perm events go to the file archive only

1. Set `event_sink` to `db,file` and `event_cold_kinds` to `perm-requested,perm-decided`. Restart the throwaway
   room.
2. Approve one permission on a card, then open its detail dialog.
3. Look in the `events` directory beside the throwaway db.

**Expected:** the dialog shows no row for the new permission and says `not shown: perm-decided, perm-requested
kept in the event archive only`. The newest `events-*.jsonl` file holds both perm events for that card. The
Permissions pane still lists the decision.

### AD3. No cold sink, no routing

1. Clear `event_sink`, keep `event_cold_kinds`, restart the throwaway room.

**Expected:** the room log says `event_cold_kinds ... needs a cold sink`. A new permission appears in the card's
history, and the dialog says nothing is missing.

### AD4. Compact a copy

1. Stop the throwaway room. Run `atrium2 db compact --in <db> --out <new>`.
2. Run it again with `--window-bytes 262144 --drop-kinds perm-requested,perm-decided` and another `--out`.
3. Start the throwaway room, then run step 1 against its db again.
4. Run it with `--drop-kinds created`, and once with `--out` equal to `--in`.

**Expected:** steps 1 and 2 print both sizes and the second prints `events N -> M`. The input's size and rows do
not change. Step 3 fails with `database is open elsewhere` and writes no file. Step 4 fails both times without
writing anything. Swapping a copy into place (room stopped, old file and its `-wal`/`-shm` moved aside) starts a
room that no longer logs `not in incremental auto_vacuum mode`.

## Z. The terminals pane after a room restart

### Z1. The main board's pane reattaches by itself

1. Attach a claude card in the main board's terminals view, and pop out a second card into its own window.
2. Restart the room, and watch the board's DevTools console for lines starting with `[atrium`.

**Expected:** both come back without a click. The pane is torn down while the room is away, the board waits for
the card, and it attaches again as soon as the card is supervised.

Reported as broken on 2026-09-23 (the pane stayed unattached while the popped-out window reconnected). It was
not reproduced in seven throwaway hub+room variants, and not reproduced on the live board at the 19:33 room
restart that day. Capture from clint's board console:

```
19:33:55.253 pane torn down
19:33:56.477 boot. remembered view terms card ...
19:33:56.564 try 1 supervised=false
19:33:57.573 try 2 supervised=true
it is back. attaching
```

If it recurs, capture the same lines. The line after `waiting for` names the branch that gave up.

## AE. The terminals list's theme switches

Three checkboxes in the gear's board pane, under `board cards wear their terminal colours`. Run on a board with
at least three sessions in different terminal themes, one of them pinned and exited.

### AE1. The defaults are the old look

1. Open the gear on a browser that never set them. Attach one session.

**Expected:** `the selected row wears its theme` is checked, the other two are not. The attached row is filled
with its terminal's background, every other row is the skin's card with a coloured left edge, and the exited row
is faded to grey.

### AE2. Each switch moves only its own rows

1. Uncheck `the selected row wears its theme`.
2. Check it again, and check `rows wear their theme when not selected`.
3. Uncheck that, and check `exited rows keep their theme, faded`.
4. Check `rows wear their theme when not selected` again, and uncheck `exited rows keep their theme, faded`.

**Expected:** after 1 the attached row is the skin's card with a frame in the skin's accent, still open toward
the terminal. After 2 every live row is in its theme and the attached one has a two-pixel frame. The exited row
is still grey. After 3 the live rows are back to the skin's card and the exited row is its theme, pale and
faded, still tinted, on a light skin and a dark one alike (never darker than the list). Hovering it brings most
of it back. After 4 the live rows are in their theme and the exited row is the skin's card faded to grey.

### AE3. Kept per browser

1. Reload. Open the board in a second browser.

**Expected:** the reload keeps all three. The second browser shows the defaults. A browser that had the old
`cards wear their terminal colours` on opens with `rows wear their theme when not selected` checked.

## AF. Terminals list: the bridge, group removal, and one settings read

Run on a board with custom grouping on (gear, grouping, `custom`) and one pinned session filed into a group, so
it is drawn twice: once under `pinned` and once under its group.

### AF1. Both copies of the attached row bridge into the terminal

1. Attach the pinned, filed session.
2. Check `rows wear their theme when not selected`, and uncheck it.

**Expected:** both copies of the row are framed the same way, and each has a strip of its background running
over the divider to the terminal's edge, with the row's border along its top and bottom. With idle rows worn
the frame and the strip's edges are both two pixels, so there is no step where they meet. Scrolling one copy
out of view drops only its strip.

### AF2. A group made here can be removed here

1. Press `+ new group` in the list's tray and name it `spare`.
2. Right click the `spare` heading and choose `remove from the view`.
3. Right click the heading of the group holding the filed session and choose `remove from the view`.

**Expected:** after 2 the `spare` heading is gone. After 3 that heading is gone too, and the session is drawn
once, under `pinned`, still pinned and still carrying its tag. Nothing is closed. Adding the group back with
`+ new group` files the session into it again. Right clicking any other heading (the tree's, or `untagged`)
opens no menu of its own.

### AF3. A board load reads settings once

1. On a hub, open the browser's network tab and reload the board.

**Expected:** one `GET /v1/settings` for the load. Reloads after the first answer in a few milliseconds rather
than over 100, on a room with many worktrees. Opening the gear reads it again.

## AG. A peer message waits for an empty line and the end of the turn

Run with two supervised sessions, `a` and `b`, both attached on the board. Room restarted on this build.

### AG1. A message that arrives mid-turn waits for the turn to end

1. Give `b` a prompt that keeps it working for a minute (`run sleep 60 in bash, then say done`).
2. While `b` is working, have `a` call `atrium_say` to `b` with `ping from a`.
3. While `b` is still working, type `half a thought` into `b`'s terminal and leave it there. Do not press Enter.
4. After `b`'s turn ends, clear the line with backspace and take your hands off the keyboard.

**Expected:** after 2 nothing appears in `b`'s terminal and `b`'s card shows the held-message chip for `a`. `b`'s
next tool calls do not carry the message. After 3 and the turn's end, the message still does not appear, because
the line has text. After 4, about two seconds later, `[atrium] a says: ping from a` is typed and sent on its own,
and `half a thought` is not part of that prompt. The chip clears.

### AG2. An idle session still takes a message at once

1. With `b` idle and nothing in its line, have `a` call `atrium_say` to `b`.

**Expected:** typed and sent at once, same as before this change. `atrium_say` answers `terminal`.

## AH. Codex on the board: its hooks and its cursor

Run on a room with the codex runner on and its hooks wired (runners tab, codex, atrium hooks). The room needs the
rebuilt `atrium` hook binary as well as the rebuilt room: the hook lines codex runs are `atrium hook`, `atrium
session` and `atrium turn`. Start a codex session in a scratch directory and approve its hooks once.

### AH1. No hook fails

1. Ask the codex session to run `dir` (or `ls`) in its shell.

**Expected:** no `Hook failed` / `hook exited with code 1` line after the command. While the command runs the
card's badge says it is running a tool, then `thinking`, then `idle` when the turn ends. The card keeps the codex
mark through all of it, and after the turn ends too.

### AH2. The cursor stays at the prompt

1. Give the codex session a task that takes a minute or more, so it animates its input box while it works.
2. Watch the input box.

**Expected:** the cursor is at the start of codex's input line or not shown. It does not jump across the input
box to the dots codex draws there. Once codex is idle the cursor sits at the prompt, and typing lands there.

### AH3. Claude is unchanged

1. Open a claude session and type a few characters.

**Expected:** the cursor follows the typing with no lag.

## AI. The work ledger records (stage 1)

Run in a throwaway room, never against the live board. See `docs/work-ledger-design.md`. The room needs this
build. The hook binary is unchanged. `work-ledger.md` is in the room's database directory.

Set up: a session `orch` on the board. From `orch`, `atrium_launch` a worker `w1` with a brief, in a scratch
directory.

### AI1. A launch makes a work item

1. Open `work-ledger.md`.
2. Run `atrium2 ledger --db <the room's database>`.

**Expected:** both list `w1` under "Open", launcher `orch`, with the brief's first line. A session started from
the board's own launch dialog is not listed.

### AI2. A done report does not close the work

1. From `w1`, call `atrium_report` with status `done` and a sha.

**Expected:** `w1`'s card moves to `done`. The report answer carries `work_state: reported`. `work-ledger.md`
lists `w1` under "Reported, waiting on a verdict" with the report's first line and the commit. `orch` receives
the report once.

### AI3. A crash is flagged, once

1. Launch a second worker `w2` and have it call `atrium_report` with status `progress` and a summary.
2. Kill `w2`'s runner process from Task Manager, not through atrium.
3. Wait for one reaper tick, about 30 seconds.

**Expected:** `work-ledger.md` lists `w2` first, under "Ended without a report". `orch` receives exactly one
notice: `w2 ended without a final report (process is gone at HH:MM). last report: progress HH:MM "<summary>".
outputs: none. card <id>`. Waiting another tick sends nothing more.

### AI4. Nothing resumes by itself

1. Restart the throwaway room.

**Expected:** `w2` is still "Ended without a report" and no process was started for it.

2. Resume `w2` from its card.

**Expected:** `w2` moves back to "Open". Its log says it is running again.

### AI5. An exit after a done report leaves it reported

1. Exit `w1`'s session.

**Expected:** `w1` stays under "Reported, waiting on a verdict". `orch` receives no ended notice.

### AI6. Messages are on the work

1. Have `w2` `atrium_say` to `orch`, and `orch` `atrium_say` to `w2`.
2. Run `atrium2 ledger --json` and find `w2`.

**Expected:** the listing matches the file. The room's database holds both messages verbatim in `w2`'s work log,
as a `say` and an `instruction`.

### AI7. The file survives the room

1. Stop the room.
2. `cat work-ledger.md`, and run `atrium2 ledger` again.

**Expected:** both still list every open item. `atrium2 ledger` opens the database read only and writes nothing.

### AI8. The backfill

1. On a COPY of a room database with launched cards from the last two weeks, with fixtures turned off in the copy,
   start a room on it.

**Expected:** the log line `work ledger: backfilled N launched card(s)...`. Every backfilled item says "inferred by
the backfill" in `work-ledger.md`. A second start does not backfill again.

## AJ. A turn a Stop hook continued still ends

Run on a room with the rebuilt room and the rebuilt `atrium` hook binary, since `atrium turn` is the Stop hook.
Start a claude session `c` by hand in a terminal the room does not own, with the atrium hooks installed, so a board
message reaches it through its hooks and is not typed. The silent-stop notice to a launcher is covered by
`TestAFlaggedStopEndsTheTurnAndNeverBlocks`, since a hand-started card has no launcher.

### AJ1. The card leaves running

1. Give `c` a prompt that ends in a reply with no tool calls, for example "say hi".
2. As the turn ends, the card is `needs-input`. Send `c` a message from the board's message box that also needs no
   tool call, for example "say bye". It stays queued, because `c` is idle.
3. Give `c` another prompt that ends quickly. Its Stop carries the queued message and `c` answers it.

**Expected:** while `c` answers the message the card is `running`. When `c` stops, the card is `needs-input` with
the badge `idle` and an unseen turn. It does not stay `running`. `c` is not sent back to work a second time.

### AJ2. A message sent during the continued turn waits

1. Repeat AJ1, and while `c` answers the queued message, send it a second message from the board.
2. Let `c` stop.

**Expected:** `c` stops, and the card is `needs-input`. The second message is still queued: it arrives with `c`'s
next tool call or next turn end, not before.

## AK. The after-restart wake

Run in a throwaway room, never against the live board. See `docs/restart-wake.md`. The room and the hub both need
this build: the tool is served by the hub, the queue and the typing are the room's. The hook binary is unchanged.

Set up: a supervised claude session `orch` on the throwaway room, idle, with nothing in its input line.

### AK1. A wake is typed in once after the restart

1. From `orch`, call `atrium_wake_after_restart` with `text` `we up. check the build`.
2. Look at `orch`'s card.
3. Restart the throwaway room.
4. Keep your hands off `orch`'s keyboard and wait for it to come back.

**Expected:** after 1 the tool answers `queued: true`. After 2 the card has a `wake queued` chip whose tooltip
holds the text. The session running now gets nothing typed into it. After 4, about five seconds after the resumed
session starts, `we up. check the build` is typed and sent, and `orch` starts a turn on it. The chip is gone. The
card's events have a `prompted` event with `from: restart-wake`. A second restart types nothing.

### AK1a. The wake carries the atrium label

1. Attach to `orch`'s terminal on the board, queue a wake with `text` `label check`, and restart the room.
2. Watch the terminal as the wake lands.

**Expected:** the line reads `[atrium] restart wake: label check`, with `[atrium] restart wake:` in the same grey a
peer's typed message is labelled with (`[atrium] <peer> says:`), and `label check` in the normal colour after it.
The label and the text are one prompt, sent once. The card's turn is not marked seen by that prompt.

### AK2. It waits for your line

1. Queue a wake on `orch` as in AK1 and restart the room.
2. As soon as `orch` is back, type `half a th` into its line and leave it there.
3. After ten seconds, clear the line with backspace and take your hands off the keyboard.

**Expected:** nothing is typed while the line has text. About two seconds after 3 the wake is typed and sent on
its own, and `half a th` is not part of that prompt.

### AK3. A newer wake replaces the older one

1. Call `atrium_wake_after_restart` with `first`, then again with `second`.
2. `curl -s http://127.0.0.1:<room port>/v1/tasks/<orch's card id>/restart-wake`.

**Expected:** the second call answers `replaced: first`. The GET shows one wake, `second`. After a restart only
`second` is typed.

### AK4. A deploy script can queue one

1. `curl -s -X POST -d '{"text":"we up","by":"deploy.ps1"}'
   http://127.0.0.1:<room port>/v1/tasks/<orch's wire name>/restart-wake`.
2. `curl -s -X DELETE http://127.0.0.1:<room port>/v1/tasks/<orch's wire name>/restart-wake`.

**Expected:** 1 answers `queued: true` and the card shows `wake queued`. 2 answers `cleared: true` and the chip
goes. An empty `text` answers 400 and an unknown card 404.

### AK5. A wake waits however long the card takes

1. Queue a wake with `text` `still here` on `orch`, then stop `orch`'s runner from the board.
2. Leave it stopped for more than 30 minutes. Look at the card now and then.
3. Start `orch` again from the board and keep your hands off its keyboard.

**Expected:** through 2 the card keeps its `wake queued` chip and nothing changes on it. There is no `wake expired`
chip and no `notified` event with `what: expired`. After 3, once the session is up, `[atrium] restart wake: still
here` is typed and sent.

## AL. The website skin

Run on the board from this build, with the docs site open beside it for comparison. The hub alone needs the
build.

### AL1. The board wears the site

1. Gear, skin, pick `website`.

**Expected:** the colours are harbour's. `+ new agent` and every other primary button (`save`, `add a runner`)
is filled with the teal-to-blue gradient in dark text, on a teal glow that turns blue on hover. The header is
more see-through than harbour's and its bottom edge is the grid colour. A soft teal and blue glow sits behind the
top of the board, under the grid. Text sizes, spacing and corners are the same as harbour's.

### AL2. A disabled primary button still looks disabled

1. In the browser console, run `document.querySelector("button.go.newagent").disabled = true`.
2. Reload to put it back.

**Expected:** after 1, `+ new agent` is dimmed and flat, with no gradient and no glow.

### AL3. No other skin changes

1. Pick `harbour`, then `noir`, then `daylight`.

**Expected:** each looks as it did before this build: `+ new agent` is the flat accent chip, the header is the
same gradient as before, and there is no glow behind the board.

## AM. The hub restart gate

Run against a throwaway hub built from this branch, never the live one. See `docs/hub-restart-gate.md`. Open the
throwaway hub's board in two windows and pop one terminal out into a third.

### AM1. No board open goes at once

1. Close every board window.
2. Run `scripts/hub-restart-gate.ps1 -Hub <the throwaway hub>`.

**Expected:** `go: no board is open`, exit 0, within a second.

### AM2. Typing holds the countdown back

1. Open the board. Type into an attached terminal without stopping.
2. Run the script with `-Idle 5`.

**Expected:** no toast while you type. Stop typing. About five seconds later every window, the popped-out one
included, shows "atrium restarts in 5s", counting down. At zero every window shows the "atrium is restarting"
modal and the script prints `go`, exit 0.

### AM3. Typing during the countdown starts the wait again

1. Run the script. When the countdown shows, press a key in any window.

**Expected:** the countdown goes from every window. It comes back once the boards have been quiet for the idle
window.

### AM4. A click pauses everywhere, and resume lets it through

1. Run the script with `-Wait 120`. Click the countdown toast in the popped-out window.

**Expected:** the countdown goes from every window and each shows "restart on hold" with a resume button. The
paused toast stays past nine seconds and stays when other toasts fill the stack.

2. Open a fourth board window.

**Expected:** it shows the paused toast too.

3. Click resume in any window.

**Expected:** the paused toast goes from every window. After the idle window a fresh countdown shows, then the
modal, and the script prints `go`.

### AM5. A pause outlasts the deploy's wait

1. Run the script with `-Wait 20`. Click the countdown.

**Expected:** after 20 seconds the script prints `held: the restart is paused from the board`, exit 3. The paused
toast is still up. A second run while paused also ends `paused`.

Superseded by AO1: a script from this change waits out a pause with no timeout. AM5 still holds for a script that
does not send `hold`.

### AM6. The modal holds until the new hub answers

1. Let a countdown run out, then stop the throwaway hub and start it again.

**Expected:** Escape and clicks outside do not close the modal while the hub is down. It clears by itself when the
board reconnects, and the attached terminal reattaches.

### AM7. Loopback, one at a time, and an old hub

1. From another machine, over an overlay, `curl -X POST <the hub's board address>/_hub/restart`.

**Expected:** 403.

2. Run the script twice at once.

**Expected:** the second prints `held: another restart is already waiting for an answer`, exit 3.

3. Point the script at a hub built before this change.

**Expected:** `go: this hub is older than the restart gate`, exit 0, with no countdown on its board.

## AN. A prepare command next to a chatty profile

Run on a room built from this branch. Pick a runner and note its prepare command so you can put it back. Your
profile stays as it is.

### AN1. Output from the profile or the command does not fail the launch

1. Give the runner the prepare command `Write-Host 'hello from prepare'; $env:ATRIUM_AN1 = 'yes'` on Windows, or
   `echo 'hello from prepare'; export ATRIUM_AN1=yes` elsewhere.
2. Launch a card on that runner. In its terminal, print `ATRIUM_AN1`.

**Expected:** the launch succeeds and the variable is `yes`. If your profile prints something of its own, such as
`docker -> ...`, the launch still succeeds.

### AN2. A shell that printed no environment says what it printed

1. Give the runner the prepare command `Write-Host 'where did it go'; exit 0` on Windows, or
   `echo 'where did it go'; exit 0` elsewhere.
2. Launch a card on that runner.

**Expected:** the launch fails, and the reason reads "the shell did not print the environment. It printed:"
followed by `where did it go` (after anything your profile printed). It does not mention JSON or an invalid
character.

### AN3. A failing prepare command still reports the shell's error

1. Give the runner the prepare command `atrium-no-such-command`.
2. Launch a card on that runner.

**Expected:** the launch fails with "the prepare command failed:" and the shell's complaint that the command does
not exist. Put the runner's prepare command back.

## AO. A held restart stays held, and toasts stay on screen

Run against a throwaway hub built from this branch, never the live one. See `docs/hub-restart-gate.md`. Open the
throwaway hub's board in two windows. Use `scripts/hub-restart-gate.ps1` from this branch.

### AO1. A pause holds the deploy with no timeout

1. Run the script with `-Wait 20`. Click the countdown toast.
2. Wait a full minute.

**Expected:** the script prints `waiting: the restart is paused from the board, until somebody resumes it` and is
still running after the minute. The paused toast is still up in both windows.

3. Click resume.

**Expected:** the script prints `waiting: resumed from the board, ...`. After the idle window a fresh countdown
shows, then the modal, and the script prints `go`, exit 0.

### AO2. A pause made with no deploy waiting holds the next one

1. Run the script, click the countdown, and stop the script with Ctrl+C. The paused toast stays up.
2. Run the script again and wait a minute.

**Expected:** no countdown shows. The script says it is paused and keeps waiting. Resume lets it through.

### AO3. A hub that dies while held stops the deploy

1. Run the script and click the countdown.
2. Stop the throwaway hub.

**Expected:** the script prints `held: the hub went away while the restart was waiting`, exit 4. It does not
print `go`.

3. Start the hub again, run the script, click the countdown, then restart the hub within a few seconds.

**Expected:** the script ends with `held: ...` and exit 4, either "went away" or "forgot the ask".

### AO4. The countdown and paused toasts stay until the hub moves on

1. Run the script with `-Countdown 30`. While the countdown shows, cause four or more other toasts (for example,
   copy a card path four times).

**Expected:** the countdown stays, and the ordinary toasts stack up to three beside it.

2. Narrow the window to phone width and click the countdown.

**Expected:** the paused toast stays with one ordinary toast beside it as more arrive. It is still there after a
minute, after a room attaches or detaches, and after the board's stream drops and comes back.

### AO5. An alert about news stays its full life

1. Launch a card from a shell with `atrium launch` while the board is in front of you.

**Expected:** the "... is on the board" toast stays for nine seconds. It does not pop and go.

2. Make a card ready while the board is in front of you, then answer it from another window.

**Expected:** its toast goes within a poll of the answer.

## AP. A popped-out terminal stays out of the board across a room-set change

Run against a throwaway hub built from this branch, with two throwaway rooms joined, never the live one. The
headless check is `HEADLESS_ONLY=popoutTagFlip node scripts/test-board-headless.js`.

### AP1. A popped-out card keeps its placeholder when a room leaves

1. With both rooms attached, open the terminals view and pop a card out. Its window's url carries `room~id`.
2. Stop the other room, so one room is attached and the board spells every id bare.

**Expected:** the board shows the popped-out marker on the card's row. The board's pane does not attach it. The
popped-out window keeps its terminal.

3. Start the other room again.

**Expected:** the same. Nothing attaches on the board.

### AP2. A popped-out card stays out of the board through a restart

1. Attach a card on the board, then pop it out, with both rooms attached.
2. Restart the hub and both rooms, so the rooms re-attach one at a time.

**Expected:** the board comes back on the terminals view and does not attach the card. The popped-out window
reconnects and keeps the terminal.

### AP3. The popped-out window wins when both hold the card

1. With one room attached, attach a card on the board. In a second tab, paste the card's `#term=room~id` url.

**Expected:** the board lets go of its pane and says the terminal moved into its own window. Only the new window
shows the session.

## AQ. The gate says which go it gave, and counts every board

Run against a throwaway hub with ONE room attached, the live shape (`/_hub/health` says `"only"`). Never the live
hub. See `docs/hub-restart-gate.md`.

### AQ1. A go with no board says so

1. Close every board window. Run `scripts/hub-restart-gate.ps1 -Hub <the throwaway hub>`.

**Expected:** `go: no board is open`, exit 0.

### AQ2. A go after a countdown says how many streams saw it

1. Open the board, attach a terminal, and pop a second terminal out into its own window.
2. `GET /_hub/restart` on the throwaway hub.

**Expected:** `"boards":2`.

3. Leave both windows alone and run the script with `-Idle 2`.

**Expected:** both windows show the countdown, then the restarting cover. The script prints
`go: counted down on 2 board stream(s) and nobody paused`, exit 0. The hub's audit tab shows the same words after
`restarting:`.

### AQ3. A board scoped to the room is counted

1. Close every board window. Hold a scoped stream open with `curl -N <the throwaway hub>/v1/events/room/<name>`.
2. `GET /_hub/restart`.

**Expected:** `"boards":1`.

3. Run the script with `-Idle 2`.

**Expected:** the curl prints `event: hub-restart` with `"state":"countdown"`, then `"state":"restarting"`.

## AR. Group colours, and moving groups on the terminals pane

Run on a hub built from this branch. In settings, set grouping to `custom` and make two groups, `ar-one` and
`ar-two`. File one card into `ar-one` with `into group` on its menu. Pin a second card. The headless sections
`groupColor` and `groupDrag` cover the same ground.

### AR1. A terminals pane heading wears its group's colour

1. Open the terminals pane.

**Expected:** the `ar-one` and `ar-two` headings are drawn in their own colours, the same hues as the stack's
headings, and the line down each group's rows takes the same colour. `pinned` and `untagged` look as before.

2. Right click `ar-one` on the terminals pane and pick `recolor…`. Choose a green swatch.

**Expected:** the heading turns green at once. On the stack and the board, `ar-one` is green too, and so is the
`ar-one` tag chip on the card.

3. Repeat on the `daylight` and `website` skins, with card colours on and then off.

**Expected:** the colour follows the recolour every time, and the name stays readable on `daylight`.

### AR2. A colour is kept in this browser and reaches every window of it

1. Reload the page.

**Expected:** `ar-one` is still green everywhere.

2. Open the board in a second window. In the first window, recolour `ar-one` to blue.

**Expected:** within a second, the second window's terminals pane shows `ar-one` in blue, without waiting for a
poll. A different browser or machine keeps its own colours. The colour is a per-browser preference, not a
daemon setting.

### AR3. Drag a group heading to move the group

1. On the terminals pane, hover `ar-two`.

**Expected:** a grip shows before the caret, the cursor is a hand, and the tooltip says "drag to move it".

2. Drag the `ar-two` heading above `ar-one` and let go.

**Expected:** the whole `ar-two` group, heading and rows, sits above `ar-one`. `pinned` stays on top. On the stack
and the board, `ar-two` is now above `ar-one`, and right click `ar-one` offers `move up`.

3. Start dragging a heading and let go outside the list.

**Expected:** the groups go back to the order they had.

### AR4. A heading drag and a row drag do not mix

1. Drag the card in `ar-one` onto the `ar-two` heading's group.

**Expected:** the card is filed into `ar-two` as before. The group order does not change.

2. Drag the `ar-one` heading onto the pinned bucket.

**Expected:** nothing is pinned and the order does not change.

3. Switch grouping to `tag` or `recency`.

**Expected:** no heading has a grip or drags. A heading's tooltip says that groups are reordered by hand in the
custom grouping.

### AR5. Take a card out of a group

1. Right click the card in `ar-one`, on the stack, the board and the terminals pane.

**Expected:** each menu has `out of ar-one`, and the terminals pane menu now has `into group` too. On a card in both
groups the entry is `out of group`, with a flyout of the two.

2. Pick `out of ar-one`.

**Expected:** the card leaves `ar-one` and lands in `untagged`. No other tag changes.

3. File it again. On the terminals pane, drag its row from `ar-one` onto the `untagged` group.

**Expected:** the same result as step 2.

## AS. The room's echo line splits runner time from atrium time

Run against a throwaway hub and room built from this branch, never the live ones. Start the room with
`ATRIUM_DEBUG_INPUTLAG=1`, attach a terminal from the board, and watch the room's stderr.

### AS1. A slow redraw lands on the runner side

1. In an attached shell terminal, run `powershell -c "Start-Sleep 1"` and press a key at once.

**Expected:** a line `room <task> echo: ws frame in -> first output out ~1000ms (runner ~1000ms, atrium 0.xms, ...)`.
The `runner` figure carries the delay. No `room <task> in:` line.

### AS2. Ordinary typing stays quiet

1. Type a sentence into an idle shell.

**Expected:** no echo line, or ones under 50ms. Each echo line that shows has a `runner` and an `atrium` figure,
or `runner/atrium split unknown`.

## AT. Every tooltip on the board wears the skin

Run on a hub built from this branch, with at least one supervised card and one message held for it. The headless
section `tooltip` covers AT1, AT2 and AT4, and `scripts/check-titles.sh` fails the build on a new native `title`.

### AT1. A card chip, a toolbar button and a terminals row

1. On the terminals pane, rest the pointer on the `!` held-message chip of a row for half a second.

**Expected:** the tooltip is the board's panel, in the skin's colours and font, not the browser's white box. It
reads "message from ... waiting ..." as before.

2. Do the same on the header's gear, and on a chip of a card on the stack.

**Expected:** the same panel each time. Passing the pointer across a card without stopping raises none.

3. Move the pointer away, or scroll the stack.

**Expected:** the panel goes at once. Output arriving in an attached terminal does not take it down.

### AT2. Two skins

1. Wear `daylight` and repeat AT1 step 2. Then wear `noir`.

**Expected:** the panel is light on `daylight` and dark on `noir`, matching the board around it.

### AT3. Keyboard and phone

1. Press Tab until the header's gear has focus.

**Expected:** its tooltip shows at once, with no half-second wait. Clicking a button with the mouse does not leave
its tooltip up.

2. On a phone, tap a chip, then press and hold one.

**Expected:** a tap does what the chip does and raises no tooltip. A press and hold raises it, and lifting the
finger takes it down.

### AT4. No native title is left

1. Walk the stack, board, terminals, history, rooms and perms tabs. Hover icon-only buttons such as the terminal's
   folder, the find bar's arrows and the skin lab's arrows.

**Expected:** no browser tooltip anywhere. A screen reader still names each icon-only button, from its
`aria-label`.

## AU. A room that stops answering does not fill the fetch cap

Run on a hub built from this branch with one room attached, the board open in two tabs, one card popped out, and
`log terminal input lag` on in settings. The headless section `idleRate` covers AU1 and AU2 against a mocked hub.

### AU1. Idle

1. Touch nothing for a minute. Watch the network tab of one board tab.

**Expected:** a handful of reads every ten seconds per window, plus one pass after each event. The board does not
repaint on its own between passes, and the console prints no `[inputlag]` stall lines.

### AU2. The room goes silent

1. Do a hub-only deploy, and keep the board open through the minutes before the room answers again.

**Expected:** reads to the room fail after 15 seconds each rather than hanging. With a keystroke typed into the
popped-out window, the `[inputlag]` warning reads `fetches 6/6 in flight` with at most a few queued, never a
queue that keeps growing. `/v1/health` still answers in the network tab.

2. Wait for the room to reattach.

**Expected:** within one poll every list repaints and the fetch counts go back to zero. Nothing needs a reload.

### AU3. The hub answers for a silent room

1. During the same window as AU2, watch a board read such as `/v1/waiting` in the network tab.

**Expected:** it comes back as a 503 after about 12 seconds, saying the room is attached but did not answer and may
still be reconnecting. It does not hang until the room returns. The event stream and an attached terminal stay
open throughout. `go test ./internal/link -run 'SilentRoom|QuietEventStream|SlowBody'` covers this against a room
that never answers.

## AV. A group opened by hand stays open in custom mode

Run on a hub built from this branch with grouping set to custom, at least one group of your own, a card in none of
them, and something working so cards change. Open the board in two tabs. The headless section `foldStill` covers
AV1 and AV2.

### AV1. Opening untagged

1. On the board view, open the `untagged` group. Leave both tabs alone for a minute.

**Expected:** it stays open in the tab you opened it in, and the other tab shows it open on its next repaint.
Nothing expands or collapses on its own, and the network tab shows a pass every ten seconds or per event, not a
stream of them.

### AV2. Shutting it again

1. Shut `untagged`. Leave both tabs alone for a minute, then reload one.

**Expected:** it stays shut in both, and after the reload.

## AW. One atrium binary, and the atrium2 shim

Stage 3 of `docs/one-atrium-plan.md`. `atrium` now carries the hub and the room, and `cmd/atrium2` is a shim that
answers the live scripts' lines until the cutover. This covers the parsing, the collisions, the hook lines, the
defaults and the one-machine key minting:

```powershell
go test ./internal/cli -run 'OneBinary|Colliding|SettingsHook|Defaults|Hints|RunLeaves|RunMakes'
```

The steps below are the runs a test cannot make.

Run everything from a shell with `ATRIUM_LOCATION` pointed at a private file under the throwaway directory and
`ATRIUM_SHARED_LOCATION=-`, on ports nothing live uses, or the room takes this machine's hooks.

### AW1. The shim answers the live scripts

**Retired 2026-09-26.** `cmd/atrium2` is deleted, so there is no shim to test.

1. `build.claude\atrium2.exe <line> --help` for each line in `~/.atrium2/scripts` (`hub --addr ... --link ...
   --link-advertise ... --dir ...`, `room --dir ... --db ... --http ... --agent ...`), and for `join`, `hub room
   add`, `db compact` and `ledger`.

**Expected:** each exits 0 and prints that command's help. `atrium2 hub --no-such-flag --help` exits 1, which shows
`--help` still parses the line.

2. With a throwaway hub on private ports: `atrium2 hub`, `atrium2 hub room add demo`, then `atrium2 join <string>
   --isolated` and, after stopping it, `atrium2 room --isolated` with the same flags.

**Expected:** the hub shows one room attached each time. The room's address file names `atrium2.exe`, and
`atrium2 hook --event tool-start` runs, so a hook line naming it would work.

### AW2. atrium run makes this machine's room

1. On private ports and directories: `atrium run --addr ... --link ... --atrium-dir <t>\hub --isolated --dir <t>\room
   --db <t>\room\atrium.db --http ... --agent ...`.

**Expected:** within a few seconds `/_hub/health` reports one room. `atrium rooms ls --atrium-dir <t>\hub` lists it
under this machine's name, attached. `<t>\room\room.log` holds the room's output, and its address file names
`atrium.exe`.

2. Stop `atrium run`. Check the room's `/v1/health`. Start the same `atrium run` again.

**Expected:** the room answers throughout. The second run logs that the room answers and is left alone, the room's
pid does not change, and the room reattaches.

### AW3. The new live scripts rehearse

**2026-09-26:** `cutover.ps1` is deleted. Only the `-WhatIf` runs of the other scripts still apply.

1. `scripts\live\cutover.ps1 -WhatIf`, and `-WhatIf` on every other script in `scripts\live`.

**Expected:** the cutover names exactly the running `atrium2.exe` hub and room pids, the database copies, the
stage-then-two-renames of `.atrium\bin\atrium.exe`, the scripts swap, and the two start lines in
`docs/one-atrium-cutover.md`. Nothing on the machine changes. The deploy scripts find no process to stop until the
cutover has run, because they match `atrium*.exe` in `.atrium\bin` by subcommand.

## AX. Untagged follows the sort pill

Run on a hub built from this branch with grouping set to custom, at least one group of your own, and four or more
live cards in none of your groups, named so their name order differs from their path order. The headless section
`untaggedSort` covers AX1 and AX2 against a mocked hub.

### AX1. Sorted by name

1. On the terminals pane, set the tray to `sorted by name`. Read the `untagged` group.
2. Set the stack's sort pill and the board's sort pill to `name`, and read `untagged` on each.

**Expected:** all three read a to z by the name each row shows first, not by the dim path under it. Pinned keeps
its pin order, and each group of your own keeps the order you dragged it into.

### AX2. Sorted by activity, and the stack's other pills

1. Switch each view to activity, then walk the stack through `project` and `runner`.

**Expected:** `untagged` follows every pill in every view. Your own groups do not move.

## AY. The unexpected-exit notice

See `docs/unexpected-exit-wake.md`. The tests cover a crash, a planned stop, an idle
card, a self-queued wake winning, restarts in a row and the setting off:

```powershell
go test ./internal/daemon -run 'Crash|PlannedStop|IdleCard|SelfQueued|RestartsInARow|SettingOff'
go test ./internal/api -run UnexpectedExit
```

The steps below are the runs a test cannot make. Run a throwaway room from this branch, with `ATRIUM_LOCATION`
pointed at a private file and `ATRIUM_SHARED_LOCATION=-`, on ports nothing live uses. Launch one claude card on it.

### AY1. A crash mid-turn

1. Give the card a long task, for example "count slowly to 200, one Bash sleep per number". While it is working,
   kill the room process with `Stop-Process -Force`. Start the room again with the same flags.

**Expected:** the card comes back on its resumed conversation. About five seconds after its session starts, the
terminal shows a grey `[atrium] unexpected exit:` label followed by `atrium went away while you were working
(crash) at <time>. Your session was resumed. Check where you were and carry on.`, and the session carries on. The
card's history has a `notified` event and a `prompted` event, both from `unexpected-exit`. Nothing is typed a second
time.

### AY2. A planned stop mid-turn, and an idle card

1. Launch a second card and let it finish its turn. Give the first card a long task again, and while it works stop
   the room with `POST /v1/shutdown`. Start it again.

**Expected:** the working card gets the notice with `(restart)`. The idle card gets nothing.

### AY3. A card's own wake wins

1. While the card works, call `atrium_wake_after_restart` from it with `text: "we up"`. Kill the room and start it
   again.

**Expected:** the card gets `[atrium] restart wake: we up` and no unexpected-exit line.

### AY4. Switched off

1. Open the room's cog and clear `after atrium goes away mid-turn`. Give the card a long task, kill the room and
   start it again.

**Expected:** the card comes back and nothing is typed. With the box ticked again, the next crash mid-turn types the
notice.

## AZ. A card this window has not seen says so

Run on a hub built from this branch with the board open on the stack. The headless section `newCard` covers AZ1 to
AZ4 against a mocked hub.

### AZ1. A new card

1. Launch a session from a shell with `atrium launch`, and watch the stack.

**Expected:** the new card pulses a teal ring three times over about three seconds, then keeps a teal `new` chip.
If it lands below the fold of a list you are not scrolling, the list scrolls just far enough to show it. Focus
stays where it was. Switch to the board and the terminals pane: it wears the chip there too.

2. Launch another while scrolling the stack with the wheel.

**Expected:** it pulses and gets its chip, and the list does not move under you.

### AZ2. It clears

1. Click the card, or rest the pointer on it for a second, or attach to it.

**Expected:** the chip goes at once, in every view and in any other window of this browser. A chip nobody looks at
goes on its own after five minutes.

### AZ3. A reload and a hub restart are not new

1. Reload the board.

**Expected:** nothing pulses. A card whose chip you had not cleared keeps it, and no other card gets one.

2. Do a hub-only restart and let the room reattach.

**Expected:** nothing pulses and no chip appears, including when a second room is attached and every id changes
its spelling.

### AZ4. Motion and skins

1. Turn on reduced motion in the OS, and launch a card.

**Expected:** no pulse, and the chip still appears.

2. Wear `daylight`, `website` and `noir`, and launch a card in each.

**Expected:** the ring and the chip read clearly on each skin.

## BA. A theme preview recolours the card

Run on a hub built from this branch with `cards wear their terminal colours` on and the terminals list's idle rows
worn, and two live cards in different themes. The headless section `themePreview` covers BA1 to BA3 against a
mocked hub.

### BA1. The preview reaches the card

1. Attach to one card. Open the theme picker on the terminal bar and arrow through a few themes.

**Expected:** with each pick the terminal, the attached row, the bridge between the row and the pane, and the
pane's frame all take the picked theme together. The card on the stack and the board wears it too. The other card
keeps its own colours.

### BA2. Leaving without use it

1. Preview a theme, then press escape.
2. Preview a theme, then attach to the other card.
3. Preview a theme, then close the terminal.

**Expected:** each time the first card goes back to its saved colours everywhere, and the picker closes. Nothing is
saved: a reload shows the saved theme.

### BA3. Use it

1. Preview a theme and press `use it`. Reload the board.

**Expected:** the card keeps the new theme on every surface, before and after the reload.

## BB. A paste says it is on its way, and the lag log stops counting the key's own frame

The headless section `pasteSpinner` covers BB1 steps 1 and 2 against a mocked socket: a one-line paste held for 50ms
shows the spinner, a paste that drains and echoes inside 20ms does not, and typed input never does. Run the rest on a
board built from this branch against a throwaway room with one card, so a paste into a live card is not the test.

### BB1. A paste shows `pasting`

1. Attach the card. Copy about 200KB of text, for example a large log file, and paste it with ctrl-v.

**Expected:** a small box with a spinner and `pasting 200KB` appears at the top right of the terminal after a
moment. It goes when the runner shows the paste, for example Claude Code's `[Pasted text #1 +N lines]`. The paste is
still one paste, not several.

2. Paste one line of text, and type a few words.

**Expected:** a paste of any size shows the box if it is still in flight after 20ms, for example `pasting 9B` over a
slow share. On loopback a one-line paste usually lands inside 20ms and shows nothing, not even a flash. Typing never
shows it. Right click, ctrl-shift-v and a dropped file show it the same way. So does a popped-out window.

3. Paste 200KB into a card's shell tab with `cat > /dev/null` running, so nothing echoes.

**Expected:** the box goes after 20 seconds at most, and nothing else happens.

### BB2. `socket had N bytes unsent` only when there was a backlog

1. Tick `log terminal input lag` in the cog. Open the browser console and type a line into the card.

**Expected:** each `[inputlag] ... key` line has no `socket had 18 bytes unsent` clause. It shows only when the
socket was already backed up before the key, for example a key typed while a 200KB paste is still leaving, and
then it reads `socket had N bytes unsent before this key`.

## BC. The restart and wait screens wear the skin and say what is happening

Run on a hub built from this branch, with the board open. The countdown, the pause and the restarting cover need a
hub-only deploy through `scripts/hub-restart-gate.ps1`. The headless section `restartGate` covers BC1 to BC3
against a mocked hub, and checks that the cover is not drawn while it is shut.

### BC1. The countdown and the hold

1. Leave the board idle and start a hub-only deploy.

**Expected:** a toast says `atrium restarts in 5s`, with the seconds in a turning ring on its left and a teal bar
along its foot that drains to nothing over the same five seconds. Its line reads `an update is ready. keep working
and it waits, or pause it until you are done.`

2. Start another deploy, and click the toast or its `pause` button during the countdown.

**Expected:** the countdown goes and a toast says `restart on hold`, with a still pause mark in the warning colour
and the line `atrium keeps running as it is until you press resume.` It stays until you press `resume`.

### BC2. The restarting cover

1. Let a countdown run out.

**Expected:** a card covers the board, which blurs behind it: a turning ring, `INSTALLING AN UPDATE`,
`atrium is restarting`, and `back in a few seconds. your agents keep running, and this page picks up where you
left off.` Under that is a moving bar, `nothing to do, this page reconnects on its own`, and a clock counting the
seconds. There is no focus ring round the card. It comes down when atrium answers.

2. Hold the new hub back for more than 30 seconds.

**Expected:** the line changes to `this is taking longer than usual. your agents keep running while atrium comes
back.` The clock keeps counting.

### BC3. Every skin, and the other waits

1. Repeat BC1 and BC2 wearing `harbour`, `website`, `paper`, `daylight`, `noir` and `slate`.

**Expected:** every card is in the skin's own colours, light cards on the light skins, with the teal-to-blue edge
along the top. Nothing is white on a dark skin, and nothing is a bare box with a rule across it.

2. Switch rooms from the room chip.

**Expected:** the switch cover is the same card: `SWITCHING ROOMS`, `opening <room>`, `this takes a second or two.`

3. Stop the hub for a few seconds with the board open.

**Expected:** the header shows a red `reconnecting` pill with a breathing dot, and it goes when the board is live
again.

## BD. A click on an alert lands where the alert is about

Run on a hub built from this branch with desktop notifications allowed. Try each step three ways: from the toast
with the board in front, from the desktop notification with the board behind another window, and from the row in
the toast log (the bell). The headless section `land` covers BD1 to BD6 against a mocked hub, including the service
worker's message and a plain desktop notification.

The rule: an alert about one card with a live terminal lands on that terminal, attached and focused, in its own
window if it is popped out. An alert about one card with no live terminal lands on its request if it has one, and
otherwise on the card with its detail open. An alert about no one card lands on the view it names, and an alert
about nothing lands nowhere.

### BD1. A new card

1. With the board behind another window, launch a session from a shell with `atrium launch`. Click `X is on the
   board` as soon as it appears.

**Expected:** the board comes forward on the terminals pane with the new card attached and the cursor in it. It does
not land on the stack, even when the click comes before the session's terminal is up: the board waits up to eight
seconds for it.

2. Put a card in the inbox from an intake source, and click its `is on the board`.

**Expected:** the card's detail opens at once. An inbox card has no terminal coming, so there is no wait.

### BD2. Ready, asked, stopped, stuck

1. Let a supervised card finish its turn, ask a question, wait on a peer, stop without reporting, and run one tool
   too long. Click each alert.

**Expected:** each lands on that card's terminal. A card that has exited lands on its detail instead.

### BD3. A permission

1. Have a supervised card ask for a permission. Click `X needs permission`, and the `STUCK on a permission` nag a
   minute later.

**Expected:** each lands on the card's terminal, where the request shows with its buttons.

2. Have an unsupervised session (a hook-only claude) ask, and click it.

**Expected:** the permissions tab, scrolled to that request and flashing, with its command box focused.

3. Let two agents ask at once, and click `2 agents need permission`.

**Expected:** the permissions tab.

### BD4. Board-wide notices

1. Open the gear, and click a toast that is about nothing in particular, such as `copied`.

**Expected:** the toast goes and the gear stays open.

2. Break a fixture and restart the room. Click `1 fixture did not start`.

**Expected:** the runners tab.

### BD5. A popped-out window

1. Pop a card out. Let it go ready while its window is behind the board, and click the desktop notification.

**Expected:** its own window comes forward. The board does not attach it.

2. With the popped-out window in front, let a different card go ready, and click the toast in that window.

**Expected:** the board attaches the other card. The popped-out window keeps its own card.

### BD6. No board open

1. Close every atrium tab. Launch a session, and click the desktop notification.

**Expected:** a board opens on the new card's terminal, and the address bar shows no `?land=` after it lands.

## BE. The restart notice stays until atrium is back

Run on a hub built from this branch, with the board open on the terminals pane and a card attached. Each step needs
a hub-only deploy through `scripts/hub-restart-gate.ps1`. The headless section `restartStays` covers BE1 to BE4
against a mocked hub, frame by frame, and `restartGate` covers the cover clearing on a new hub.

The rule: from the moment `atrium restarts in 5s` appears until the new atrium answers and the board has caught up,
the countdown or the cover is on screen in every frame. The cover comes down only once `GET /_hub/restart` names a
different hub process from the one that said `restarting`, the board build has been read, and one refresh has
finished.

### BE1. Countdown to cover to board

1. Leave the board idle and deploy a hub-only build with no board change.

**Expected:** the countdown runs to `0s` and stays until the cover replaces it, with no frame of bare board between.
The cover stays while the hub is down and comes down once the new hub answers and the cards have been read again.
It does not flash off and back on.

### BE2. A new board build

1. Deploy a hub-only build that changes the board.

**Expected:** the page reloads onto the new build while the cover is up, and the reloaded page comes up with the
cover already on it, its clock still counting from when the restart began. The cover comes down once the reloaded
board has its cards. At no point is the board bare with atrium still away.

### BE3. Nothing else takes it down

1. During a restart, press Escape, click outside the cover, and click a toast that goes to a view or a card.

**Expected:** the cover stays. Its clock keeps counting.

2. During a restart, reload the page yourself.

**Expected:** the page comes back with the cover up, and it comes down when atrium is back.

### BE4. A window that missed `restarting`

1. Open two board windows. In one, open the browser's dev tools, set the network to offline just before the
   countdown runs out, and set it back online once the new hub is up.

**Expected:** that window keeps the countdown at `0s` while it is offline. When it reaches the new hub, the cover
takes over from the countdown and then comes down. The countdown is not left on screen.

## BF. Atrium is down, and nobody said it would be

Run on atrium built from this branch, opened on `http://localhost` or `127.0.0.1` (a service worker needs a secure
origin, and loopback counts). Open the board once with atrium up first, so the service worker installs and keeps
its offline page. The headless section `atriumDown` covers BF1 to BF4 against a mocked atrium.

### BF1. An open board loses atrium

1. With the board open, stop atrium from its terminal, or kill the process.

**Expected:** for about five seconds the board shows only the red `reconnecting` pill. Then a card covers it, in the
restart cover's shape but with a red and amber edge and ring: `NOT RUNNING`, `atrium is not running`, `start it with
atrium run and this page comes back by itself.`, a moving bar, `trying again every few seconds` and `down for Ns`,
counting from when atrium stopped. Escape and clicks do not take it down.

2. Start atrium again with `atrium run`.

**Expected:** within a few seconds the cover comes down and the board is current. Nothing needs a click or a reload.

### BF2. A blip and a planned restart do not show it

1. Stop and start atrium within two seconds.

**Expected:** no down cover. Only the `reconnecting` pill shows, briefly.

2. Run a hub-only deploy through `scripts/hub-restart-gate.ps1` and hold the new hub back for ten seconds.

**Expected:** the countdown and the `atrium is restarting` cover, as in BE. The red down cover never shows.

### BF3. A reload while atrium is down

1. Stop atrium. Reload the board, and open a new tab on the board's address.

**Expected:** both show the same red card, on the board's own background and skin, instead of the browser's `can't
reach this page`. Its clock counts. It works with the network cable out.

2. Start atrium.

**Expected:** within a few seconds both tabs load the board by themselves.

### BF4. Behind a share

1. Open the board through a share or overlay, then stop atrium and reload.

**Expected:** the red card, not the share's own `bad gateway` page. It loads the board once atrium is back.

What is not covered: a browser that has never opened the board has no service worker yet and still gets its own
error page. So does a board opened over plain `http://` on a LAN address, where the browser allows no service
worker. The open-board cover in BF1 works in both.

## BG. Selecting the attached terminal again does not replay its history

Run on the board, with a supervised card that has a long scrollback (a few screens at least). The headless section
`reselect` covers BG1 and BG2 against a mocked attach socket.

### BG1. The row, again

1. Open the terminals pane and click the card's row. Scroll the terminal up a screen or two.
2. Click the same row again.

**Expected:** the terminal takes focus and stays where you scrolled it. It does not go blank and redraw its history,
and the row stays selected. Typing goes to the terminal.

### BG2. Every other way back onto it

1. With the card still attached, go to the stack view and click its `attach`.
2. Raise a toast for the card (for example let it finish a turn), then click the toast.
3. Open the switcher (`ctrl-shift-k`) and pick the same card.

**Expected:** each lands on the terminals pane with the terminal focused and scrolled where it was. None redraws the
history.

### BG3. What still re-attaches

1. Click a different card's row, then the first card's row again.

**Expected:** each switch attaches and draws that card's history, as before.

2. Restart the card's room, or wait for its socket to drop and come back, then click its row.

**Expected:** a card whose socket closed re-attaches and draws its history. Switching the pane between `runner` and
`shell` also re-attaches, as before.

## BH. Over a terminal the toasts hang from the top right

Run on the board with a supervised card attached on the terminals pane. The headless section `toastsTop` covers BH1
to BH4 against mocked endpoints.

### BH1. Top right on the terminals view, bottom right everywhere else

1. On the stack view, raise two toasts (for example copy a card's id twice with different cards, or let two cards
   finish a turn).
2. Switch to the terminals view while they are up.

**Expected:** on the stack view they sit bottom right. On the terminals view the same toasts sit top right, just
under the terminal's bar and inside the pane's right edge. They move without fading in again. The newest is the top
one. The terminal's input line and status bar at the bottom are clear. Back on the stack view they return bottom
right.

### BH2. Nothing at the top of the terminal is covered

1. On the terminals view, paste a large block (over 256 KB) so `pasting …` shows at the top right of the pane.
2. Raise a toast while it shows. Then press `ctrl-f` to open the find bar and raise another.

**Expected:** the toasts sit below the paste indicator and the find bar. Neither is covered. The `pop out` and other
buttons on the terminal's bar are clear too.

### BH3. The restart notices move with them

1. On the terminals view, let the hub announce a restart so the countdown toast shows, then hold it so the paused
   toast shows.

**Expected:** both sit top right with the other toasts. On the stack view they are bottom right.

### BH4. A popped-out window, and every skin

1. Pop a card out and raise a toast for it in that window.
2. On the board, switch skins (harbour, daylight, website, noir, paper) on the terminals view with a toast up.

**Expected:** the popped-out window's toast sits top right, under its bar. In every skin the board's toasts stay top
right on the terminals view.

## BI. A say is typed in mid-turn, unless it asks to wait for the turn

Run on the board with two supervised claude cards, one as the sender (A) and one as the worker (B). Give B something
that takes a minute or more (for example "run the whole test suite and summarise it"). Go tests in
`internal/daemon/typing_race_test.go` cover BI1 to BI5. The headless section `sayWhen` covers BI5 and BI6. The fake
runner test `TestFakeRunnerDoneSayRidesTheStopHook` covers the Stop hook half of BI2.

### BI1. Immediate is the default

1. While B is working, with B's input line empty, have A call `atrium_say` to B with "stop now and say what you
   were doing". Do not pass `when`.

**Expected:** the text appears in B's terminal within a few seconds, mid-turn, with A's grey `[atrium] ... says:`
label. The answer says `terminal` and `when: immediate`. B reads it at its next step and stops.

### BI2. `when: "done"` waits for the turn

1. Start B working again. Have A call `atrium_say` to B with `when: "done"` and "when you finish, rebase".

**Expected:** nothing is typed while B works, and the answer says `queued` and `when: done`. B's terminal row shows
the `!` chip, and its tip says the message waits for the session's turn to end. It does not tell you to clear your
line. B's next tool calls are not interrupted. About two seconds after B's turn ends the text is typed in and sent,
and the chip goes.

### BI3. A runner set to no falls back to done

1. Under rooms, runners, the claude row says `mid-turn`. Edit it, untick `this runner takes typed input mid-turn`,
   and save. The row now says `turn end`.
2. Repeat BI1.

**Expected:** the say waits for B's turn to end, as in BI2, and the answer says `when: done`. Tick the box again
afterwards.

### BI4. The gate still holds

1. While B works, type half a line into B's terminal and leave it. Repeat BI1.

**Expected:** nothing is typed into your half line. The chip's tip says the message delivers when your input line is
clear. Clear the line and wait two seconds: the message goes in, mid-turn.

2. Get B to raise a permission dialog of its own (not atrium's gate), then repeat BI1.

**Expected:** nothing is typed, and the chip's tip says a dialog is open. Answer the dialog and the message goes in.

### BI5. The chip counts

1. With your line part written as in BI4, have A send B two messages.

**Expected:** the chip reads `! 2`, and its tip names the oldest sender.

### BI6. The board's two buttons

1. Open B's card. Under "say something to it" there are two buttons, `send` and `immediately`. The note has
   `send it` and `immediately`.
2. While B works, type a message and press `immediately` (or ctrl-enter).

**Expected:** it is typed into B's terminal mid-turn.

3. Type another and press `send` (or enter).

**Expected:** the hint says it waits for its turn to end, and it goes in once B's turn ends.

4. Write a note and press `immediately`, then another and press `send it`.

**Expected:** the first is typed in mid-turn, the second after the turn ends.

5. Open a card atrium does not supervise and whose Stop hook has never fired, and press `send`.

**Expected:** the hint warns that nothing carries the message at the turn end, and says to send it immediately or
wire the Stop hook.

## BK. A press shows it is working, and a second press sends nothing

BK, not BJ: sa76 was adding a section in parallel and was likely to take BJ. Run on the board with a finished
(dead) claude card that has a conversation to resume. The headless section `busyGuard` covers BK1 to BK4 against
mocked endpoints, and presses every button wired through `busyWhile` twice. The Go test
`TestASecondLaunchOntoAStartingCardAnswersTheFirst` covers BK5.

### BK1. Launch shows it is working and closes

1. Open the `resume claude code` dialog on the card, the one with `pick up where it left off` ticked.
2. Press `launch`, then press it again straight away, and press Enter in the directory box.

**Expected:** at the first press the button turns into a spinner and `starting…`, keeps its width, and the button
beside it (`terminate`, `remove`) goes dim. The later presses do nothing. When the runner is up the dialog closes
and the terminal opens. One card, one runner.

### BK2. A refused launch keeps the dialog and says why

1. Open the launch dialog on a directory that does not exist, and press `launch`.

**Expected:** the spinner shows, then the button comes back as `launch`, the dialog stays open with the form as you
left it, and a red line above the buttons says why. Close the dialog and open it again: the line is gone.

### BK3. The card menu, the perms queue and the say box

1. Right click a dead card with a conversation, choose `resume`, and choose it again at once from a second right
   click.
2. With a permission waiting, double click `approve once` on its card in the perms queue.
3. Open a working card, type a message, and press `send` twice (or Enter twice).

**Expected:** one resume starts, with no error for the second. The approve button shows a spinner and `sending…`
and the request is answered once, with no `too late`. The message is sent once, and the box is empty afterwards.

### BK4. Every save and remove

1. Under rooms, runners, edit a runner and press `save` twice fast. Do the same for a source, a recogniser, a
   provider, an action, a fixture and a theme.
2. On a theme preview, press `use it` twice.

**Expected:** each shows a spinner with `saving…` (or `removing…`, or `working…`) and saves once. The other buttons
in the same row are dim until it answers.

### BK5. The daemon answers a repeat with the first result

1. From a terminal, post the same `/v1/launch` with `task_id` of a dead card twice at once (two `curl` calls in the
   background, or `atrium launch --onto` twice).

**Expected:** both answer the same card, with no `already has a runner on it`. One runner is behind it. A third post
made a minute later is refused as before, since that is a launch onto a card that is already running.
