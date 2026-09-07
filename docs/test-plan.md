# Atrium test plan

Manual test scenarios for every shipped feature. Run end-to-end before tagging a build, after touching the
hub / agent / TUI / hook code. Each scenario lists steps, expected behavior, and the most common failure mode.

Sections A through F cover v1: the hub, the agent loop and the permission surface they share. G covers the
daemon, which is where the work now happens. H covers the overlays, I covers importing rules from Claude Code,
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

## A. Mode A (hub + agent loop)

### A1. Single-agent loop, happy path

**Steps**

1. Start hub: `.\build.claude\atrium.exe hub`
2. In another wt tab: `cd <atrium-repo>` then `claude`. The `.mcp.json` here wires
   `atrium-agent`.
3. Send the claude tab a kickoff: `go`
4. Expect a greeting line in the hub chat view within ~2 seconds.
5. Type a prompt in the hub: `what cwd are you in`
6. Expect a `response` message back with the path.

**Pass criteria**
- Greeting renders in chat view with the `◆` sigil.
- Tab bar shows `chat: atrium`.
- Response renders below greeting with a horizontal rule between them.
- Status bar momentarily shows `-> atrium`.

**Common failure**
- "MCP server not connected" in claude -> `.mcp.json` path wrong or binary missing. Rebuild atrium.

### A2. Long response wrapping

**Steps**

1. With a connected agent, prompt: `send me 100 lines numbered 1 to 100`
2. Watch the chat view fill.

**Pass criteria**
- Each line is wrapped at viewport width if needed (no horizontal scroll).
- Lines are visibly indented 2 cols under the header.
- A muted `─────` separator follows the message before the next one.

**Common failure**
- Lines run off the right edge -> `wrapOne` is regressed or viewport.Width is 0.

### A3. Hub disconnect / reconnect

**Steps**

1. With an agent connected and idle, Ctrl-C the hub.
2. Wait 30 seconds.
3. Restart the hub: `.\build.claude\atrium.exe hub`
4. From the agent's claude tab, send any prompt (this fires the agent's next submit).

**Pass criteria**
- During the outage, the agent's claude tab generates NO new text. No tokens spent.
- After hub returns, the next submit succeeds and the prompt flows.
- Stderr of the agent process (in claude's MCP debug log) shows a single "unreachable" line on first failure
  and a single "resumed" line on recovery.

**Common failure**
- Agent generates filler ("hub is down, retrying...") -> tool description regressed; the LLM is seeing
  errors that should be absorbed silently.

### A4. Long-poll timeout absorption

**Steps**

1. With an agent connected and idle, leave both terminals untouched for 2 minutes.

**Pass criteria**
- No new entries in the hub chat view. The hub's 60s long-poll fires, the agent transparently re-polls as
  `keepalive`, hub does not display keepalives.
- After 2 minutes, type a prompt in the hub: the agent picks it up on the very next poll cycle.

**Common failure**
- Hub chat view shows repeated `<agent/keepalive>` entries -> hub `HandleSubmit` is logging keepalives.

### A4b. Inline-mode scrollback

**Steps**

1. Hub running, agent activated. Send 3 prompts, get 3 responses.
2. Mouse-wheel scroll up in the terminal.

**Pass criteria**
- The full conversation is visible in the terminal's native scrollback.
- Each user prompt shows as `[ts] you → <agent>` followed by the prompt body.
- Each agent message shows as `[ts] <agent>/response` followed by the body.
- No perm-request lines appear in the scrollback.
- The floating bottom frame stays in place at the cursor.

### A4c. Perm requests stay out of scrollback

**Steps**

1. Trigger a Bash perm request: prompt the agent `run "echo hi"`.
2. Approve via `y`.
3. Trigger another. Deny via `n`.
4. Scroll up through terminal scrollback.

**Pass criteria**
- Neither perm-request appears in scrollback. They were transient banner entries only.
- The eventual `echo hi` output (or denial result) DOES appear in scrollback as part of the agent's
  response.

### A4d. Hook skips MCP tools and ToolSearch

**Steps**

1. Agent activated. Watch the hub for perm-requests as the agent loops.

**Pass criteria**
- No perm-request fires for `mcp__atrium-agent__submit` (the agent's own loop tool).
- No perm-request fires for `ToolSearch`.
- Bash, Write, Edit, etc still DO fire perm-requests.

**Common failure**
- Every loop turn produces a perm-request: the skip rules in `atrium-perm-hook.ps1` regressed. Confirm the
  `mcp__*` glob match and the `ToolSearch` literal are both present in `$skipTools` or the like-pattern.

### A5. Opt-in activation

**Steps**

1. Hub running. Launch claude in a dir whose `.mcp.json` wires `atrium-agent`.
2. Send a normal prompt like `what time is it`.

**Pass criteria**
- The agent answers normally. Hub chat view stays empty (no greeting).
- The model does NOT call `atrium-agent.submit`.

3. Now send: `atrium`

**Pass criteria**
- Agent calls submit; greeting appears in the hub chat view.
- Loop begins.

4. From inside the loop, send: `stop atrium`

**Pass criteria**
- Agent acknowledges and returns to normal claude session behavior.
- No further submits unless re-activated.

**Common failure**
- Greeting appears on the FIRST normal message (regression to auto-start). Tool description regressed; the
  ServerOptions.Instructions block must keep the opt-in language.
- Casual mention of the word "atrium" triggers activation. Tighten the language in instructions: only verb-y
  phrasings count.

## B. Multi-agent

### B1. Two agents, separate scrollback

**Steps**

1. Hub running.
2. Two claude tabs in two different `.mcp.json`-equipped dirs. Each greets.
3. Press `Ctrl-K` to open the agent switcher; pick agent A. Send a prompt. See the response in chat.
4. Press `Ctrl-K`, pick agent B. The chat view should be EMPTY of agent A's messages.

**Pass criteria**
- Each agent's scrollback is isolated.
- Tab bar shows `chat: <activeAgent> (+N)` where N is unread from OTHER agents.
- Agents view shows both with last-contact timestamps.

### B2. Quick-switch by number

**Steps**

1. With ≥2 agents known and chat input empty, press `1` then `2`.

**Pass criteria**
- Each key switches to the corresponding agent. Tab bar updates. Chat view reflows.

**Common failure**
- A digit gets eaten by an in-progress text input (the input wasn't empty). That's expected behavior; the
  guard is in place to prevent eating digits inside prompts.

### B3. Waiting indicator

**Steps**

1. Two agents connected. Type a prompt to agent A. Wait for A to respond.
2. Switch to agent B; don't reply.

**Pass criteria**
- Agents view shows agent A flashing `← waiting` (alternating bold).
- Tab bar's `agents` tab shows `[1 waiting]`.

### B3b. Two agents in the same dir, distinct names

**Steps**

1. Hub running. Launch two claude tabs both `cd`'d to `<atrium-repo>\` (or any single dir
   with `.mcp.json`).
2. Activate both with `atrium`.

**Pass criteria**
- Two distinct names appear in `/agents`: `atrium-<pid1>` and `atrium-<pid2>`.
- Sending a prompt to one does NOT cross-talk to the other.

**Common failure**
- Both register as `atrium` and share a prompt channel: PID-suffix logic in `resolveAgentName` regressed.

### B3c. `/rename` aliases

**Steps**

1. Two named agents `atrium-19432` and `atrium-21876`.
2. `/rename atrium-19432 scout`
3. `/rename atrium-21876 fixer`
4. Type `@scout do thing`.

**Pass criteria**
- Status bar shows `-> atrium-19432`. The right agent receives.
- Chat header and agents view show `scout (atrium-19432)` format.
- `/rename scout` (no second arg) clears the alias.

### B3d. forget a stale agent

**Steps**

1. Two agents known in the all-agents tab.
2. Highlight one with `↑`/`↓`, press `x` (or `Delete`).

**Pass criteria**
- Status bar shows `forgot <name>`. The row disappears from the all-agents tab and the `Ctrl-K` switcher.
- If the forgotten agent's claude is still running, its next submit re-registers it (row reappears).
- Forgetting the active agent reselects the first remaining agent (or clears selection if none remain).

**Common failure**
- Forgetting leaves a phantom in the switcher: `forgetAgent` didn't prune `agentNames` or call `Hub.Forget`.

### B4. `@<agent>` targeting

**Steps**

1. Two agents known. Type `@agentB do thing` in the chat input.

**Pass criteria**
- Status bar shows `-> agentB` regardless of which agent is currently focused.
- Agent A's scrollback is unchanged.

**Common failure**
- Typing `@unknownname ...` should print a warning, not silently route. Verify with a typo.

## C. Permissions

### C1. Permission gating round-trip

**Steps**

1. Hub running. Agent launched in a dir with `.mcp.json` referencing `atrium-agent` (so auto-gate on).
2. Prompt the agent: `run "echo hello" via Bash`.

**Pass criteria**
- A `perm-request` message appears in the hub chat, with the command echoed.
- `perms` tab shows `(1!)` badge.
- Pressing `y` (or `/approve`) in the hub: status bar shows `approve perm #1 (<agent>)`. Within 1-2 seconds
  the agent's tool call resolves and a response submits back.

### C2. Permission denial

**Steps**

1. Trigger a permission request as in C1.
2. Press `n` (or `/deny`).

**Pass criteria**
- Status bar shows `block perm #N`. Agent's tool call returns with `decision: block` and the agent's response
  reflects that the command was blocked.

### C3. `y`/`n` does NOT leak as prompt

**Steps**

1. With NO pending permissions, type `n` and enter.

**Pass criteria**
- Status bar: `no pending permissions to block`. The literal `n` is NOT sent to any agent.

**Common failure**
- If `n` shows up as `<agent/response>` in chat, the shortcut handler is regressed.

### C4. Hub-down failover (perm gate)

**Steps**

1. Set `ATRIUM_PERM_GATE` is implicit-on (dir has `.mcp.json` with atrium-agent). Hub is DOWN.
2. In the agent claude tab, ask it to run a Bash command.

**Pass criteria**
- The hook fails open. Claude-code's normal permission UI fires inside the agent's tab. Approval there
  works. (We're NOT supposed to block on a missing hub.)

### C4c. Multi-pending banner

**Steps**

1. With agent activated, prompt: `write three files: a.txt, b.txt, c.txt each with content "x"`.
2. Expect three permission requests to queue.

**Pass criteria**
- The banner shows `⚠ PERMISSION PENDING — 3 total   (showing oldest; 2 more queued -- see perms tab)`.
- The detail lines correspond to perm #1 (the oldest).
- Switching to the perms tab shows all three with their IDs.
- `y` resolves #1, banner updates to show #2. Repeat until empty.

**Common failure**
- Banner shows only "1 total" when 3 are pending: the perms snapshot isn't being refreshed (check the
  `tickMsg` handler).

### C4b. Permission arrival banner + boop

**Steps**

1. Hub + agent activated. Switch to the chat view.
2. Prompt the agent: `write a file foo.txt with content "x"`.

**Pass criteria**
- A bordered, multi-line, flashing banner appears at the top of the chat view with the perm details. Yellow
  and red alternate.
- Terminal beeps (or visually flashes if you've disabled audible bell) once.
- Status bar shows `⚠ NEW permission #N -- press y/n`.

**Common failure**
- No beep: terminal has BEL suppressed; visual bell setting usually flashes the window instead. Either is
  fine, just confirm SOMETHING happened.
- Banner missing or one-line: renderPermBanner regressed; check that ThickBorder/Width are still applied.

### C5. Permission gating for Write/Edit

**Steps**

1. Hub + agent activated. Prompt the agent: `write a file foo.txt with content "hi"`.

**Pass criteria**
- A `perm-request` shows in the hub for the `Write` tool, displaying the file path.
- No in-tab claude permission prompt fires.

**Common failure**
- claude's own permission UI fires inside the agent tab. Hook is filtering by `tool_name -ne 'Bash'` -- old
  hook binary or settings.json regression.

### C6. Read-only tools NOT gated

**Steps**

1. Hub + agent activated. Prompt the agent: `read README.md and tell me the first heading`.

**Pass criteria**
- No `perm-request` in the hub for the `Read` tool.
- Agent completes the work and submits a response.

**Common failure**
- Read fires a perm-request. The hook's $skipTools list got pruned by mistake.

### C7. Footgun guard still wins

**Steps**

1. Hub running. Approve a permission that the existing `pre-tool-use-hook.ps1` would reject (e.g., a command
   matching the inline-env-prefixed-docker pattern: `FOO=bar docker ps`).

**Pass criteria**
- Even after `/approve`, the second hook (footgun guard) blocks the command. Agent reports the block.

### C8. Deny with free-form guidance

**Steps**

1. Trigger a permission request (e.g. agent wants to run a Bash command).
2. In the perms tab, highlight it, type `no, use a temp file under ./build instead` and press Enter.

**Pass criteria**
- Status bar shows `denied perm #N (<agent>) with guidance`. The request clears.
- The agent's tool call returns blocked, and the model sees the typed reason and course-corrects (retries with
  the suggested approach) rather than just reporting a bare block.
- `/deny <id> <why>` and `/deny <why>` (oldest) produce the same guided block from any tab.

**Common failure**
- Typed text routes to an agent as a prompt instead of denying the perm: the `viewPerms` branch in
  `submitInput` regressed, or you were not on the perms tab.

### C9. Permissions-only mode (no submit loop)

**Steps**

1. Set `ATRIUM_PERM_GATE=on` (env block of settings.json, or `$env:ATRIUM_PERM_GATE='on'`). Hub running.
2. Launch a plain claude session in a dir with NO `.mcp.json` and NO atrium activation. Ask it to run a Bash
   command.

**Pass criteria**
- The perm-request appears in the hub even though the session has no atrium-agent MCP and never greeted.
- The agent shows up in the all-agents/perms views named after its cwd leaf (or `ATRIUM_AGENT_NAME`).
- Approving/denying from the hub resolves the agent's tool call. No submit loop is involved.

**Common failure**
- No perm-request arrives: `_AtriumWired` is still required because `$forceGate` parsing broke, or the env var
  did not reach the session (it is read at session start, so restart the session after editing settings.json).

## D. Choices picker

### D1. `{choices}` block renders as picker

**Steps**

1. Prompt the agent: `Ask me how to proceed, with three concrete options.`

**Pass criteria**
- Agent emits a `{choices}...{/choices}` block (per its tool description).
- TUI strips the markers and renders a styled box with numbered options below the prose.

**Common failure**
- The agent invents `1) ... 2) ...` prose instead. Restart the agent claude so the new tool description takes
  effect; the description tells it to use `{choices}` whenever the reply is a small finite set.

### D2. 1-9 picks an option

**Steps**

1. With an active choices picker (D1), press `1`.

**Pass criteria**
- Status bar: `picked: <text of option 1>`.
- Agent receives that text as its next prompt and responds.
- The picker disappears from the latest message footer.

### D3. Typing a freeform reply clears the picker

**Steps**

1. With an active choices picker, type your own message and press Enter.

**Pass criteria**
- Status bar: `-> <agent>`. Agent's `activeChoices` is cleared so subsequent 1-9 reverts to agent-switching.

## E. Mode B (read-only aggregator)

### E1. `atrium status`

**Steps**

1. With `gwt` having registered some sessions on this box, run `atrium status`.

**Pass criteria**
- Table prints with columns: STATE, BRANCH, WINDOW, PID, WORKTREE.
- Filters `--needs-input` and `--alive` work.

### E2. `atrium watch`

**Steps**

1. Run `atrium watch`.
2. Trigger a state transition in any gwt-tracked claude session (prompt + stop).

**Pass criteria**
- The new lines stream into stdout. Matches `gwt watch`'s output line-for-line.

### E3. MCP tools via `atrium serve`

**Steps**

1. Wire `atrium serve` as an MCP server in any claude session.
2. Ask it: `Use the snapshot tool and tell me what sessions are known.`

**Pass criteria**
- Tool result is a JSON object with a `sessions` array. Each entry has the fields documented in the README.

## F. Resilience regression sweep

### F1. ANSI sentinel translation still works

Send the agent: `Reply with {green}done{reset} and {bold}heads up{reset}.`

**Pass criteria**
- The response renders in real green and bold. No literal `{green}` text visible.

### F2. Long-poll keepalive still silent

Connect an agent, let it idle for 90 seconds. Hub chat must show NOTHING new during that window.

### F3. Restart hub mid-prompt-typing

While typing a long prompt in the hub, Ctrl-C the hub (don't press Enter). Reopen. Agent state on the next
reconnect is fine; conversation is empty (no persistence is intentional).

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

### P1. The toast about a raised window appears in the raised window

**Steps**

1. Pop a supervised card out with the icon on its row. A second window opens with the terminal in it.
2. Go back to the board and press `attach` on that same card.
3. Watch the window that comes to the front.

**Expect** the popped-out window is raised, and the message `the board sent you here` is drawn IN THAT WINDOW.
The board draws nothing. If a toast appears on the board instead, it is being said in the window you are
leaving, which is the bug.

### P2. A window the board did not open is admitted to, not lied about

**The failure this exists for.** A window is found again by name, and only a script-opened window has one. The
board would fail to find it, open a second window onto the same terminal, and say the first one was gone.

**Steps**

1. Copy the board's address, open a NEW TAB by hand, and paste `<address>/#term=<card id>` into it. Use a
   supervised card. The terminal attaches.
2. Go back to the board. Press `attach` on that card.

**Expect** a dialog saying it is in a window this board did not open, and that a page may only raise a window it
opened itself. **No second window opens.** Two views on one terminal is the situation `docs/supervision-design.md`
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
