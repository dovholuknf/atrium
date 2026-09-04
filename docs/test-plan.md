# Atrium test plan

Manual test scenarios for every shipped feature. Run end-to-end before tagging a build, after touching the
hub / agent / TUI / hook code. Each scenario lists steps, expected behavior, and the most common failure mode.

Sections A through F cover v1: the hub, the agent loop and the permission surface they share. Section G covers
the daemon, which is where the work now happens.

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

## Notes for future automation

- The simple stdin mode (`atrium hub --simple`) is a single-process script-friendly target. A Go-test could
  spin up a hub on a random port, fire a synthetic agent client, drive prompts, assert responses. Worth
  building when the surface stabilizes.
- The choices parser (`extractChoices`) is pure Go and easy to unit test. Same for `wrapLines` / `wrapOne` /
  `visibleLen`. Adding `internal/tui/parse_test.go` would catch regressions cheaply.
