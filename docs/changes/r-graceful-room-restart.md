# r-graceful-room-restart: a room restart wraps its sessions up first

## Design

A room with open sessions never got deployed. A restart ends every terminal the room owns, so the orchestrator held
room-side builds until the room was empty, and on a busy machine it never is. The restart now asks first.

**What triggers it.** The two ways a room is told to go down and come back:

- `POST /v1/shutdown` on the room, which is `atrium stop` and the deploy scripts' `Stop-Room`.
- A restart the hub forwards (`restart_atrium`), which is `onHubRestart` in `internal/cli/roomrestart.go`.

Ctrl-c, a signal and a kill are NOT graceful. A kill is not a stop, and the wind-down they start is unchanged.
`atrium_deploy` itself never restarts a room, it holds one. The restart of a deploy is the script's `Stop-Room`, so the
deploy takes the graceful path by default through `/v1/shutdown`. The standalone `atrium control` restart tool is not
changed.

**What each card gets.** Only a card that is working. A card is working by the room's one rule (`daemon.BusyCard`): it
is supervised, not waiting on a human, not done or dead, and has fresh activity that is not thinking. A card at its
prompt, waiting on a permission dialog or a question, or finished, is already in the one state that survives a restart
and is left alone: no prompt and no wake. Each working card is typed a labelled prompt through the same gate as the
context cycle (`typeLabelledGuarded`, as soon as the line is free, mid-turn included where the runner takes it):

`the room is restarting. wrap up the current step, write your state to <path>, then run <atrium> ready.`

The path is the card's context handoff path. This is not a context cycle: nothing is cleared. The room reopens the
session with its conversation, so the state file is a help and not a requirement.

**How ready is detected.** Either of two things, whichever comes first:

1. The card runs `atrium ready`. `POST /ready` accepts it while a restart wrap-up waits on that card. No handoff file is
   required, unlike the cycle's, because the conversation is kept.
2. The card goes idle: its turn is over and the runner has stayed out of a turn for `turnSettle` (2 s). A prompt typed
   between turns must have started a turn first, so the gap before the turn begins is not read as idle.

**The timeout.** The room setting `restart_wrap_wait_s`, 300 by default, from 10 to 3600. When it runs out the restart
goes ahead. The log and a `notified` event on each card say `restart: did not answer the wrap-up in 5m0s`, and the
room's log names every such card in one line.

**What happens to every card that was prompted.** Whether it answered, went idle or never answered, it gets a restart
wake queued (`restart-wake.md`) unless it already has one, which is its own and wins. The wake says the room restarted
and to carry on, and names the state file when one was written. A card that never answered also has that said in its
wake. Then the restart goes ahead, and the room reopens sessions as before.

**The opt-out.** `POST /v1/shutdown?now=1`, `atrium stop --now`, `restart_atrium` with `immediate: true`, and
`-Immediate` on the live scripts. `now` also overrides a wrap-up already waiting: a second request with it stops the
room at once. The opt-out on `/v1/shutdown` prompts nothing and queues no wake. On a hub-forwarded restart it keeps the
old flow exactly: park the busy cards with a message and refuse unless `force`. If the wrap-up call itself fails, that
old flow runs too. The hub-forwarded restart waits for the wrap-up before it spawns the restarter, so the restarter's
minute for the port starts after it. The live `Stop-Room` takes `-Immediate` on `deploy-batch.ps1` and `stop-atrium.ps1`.
The sg4-control stop in `stop-atrium.ps1` is unchanged.

**Narration.** Every step is on the log: the request, each card prompted, each card's ready or idle, the timeout and
the cards named, each wake queued, and the hand-off to the wind-down. The wrap-up is bounded by the setting.

**Scripts.** `Stop-Room` waited 45 s for the process to exit before it forced it. It now waits the wrap-up setting
plus the 45 s, so the force never lands inside a wrap-up.

## Test plan

## @LETTER@. A room restart wraps its sessions up

### @LETTER@1. A working card is asked, answers, and is woken

1. Start a card that is mid-turn on a long task.
2. Run `atrium stop`.
3. The card is typed the restart prompt. Run `atrium ready` from it.

**Expected:** the log narrates the prompt, the ready and a queued wake, then the room winds down. After the restart
the card's session is back with its conversation and is typed the wake.

### @LETTER@2. A card that never answers is named

1. Set `restart_wrap_wait_s` to 10 and start a card that ignores the prompt.
2. Run `atrium stop`.

**Expected:** after 10 s the log and the card's history say it did not answer, it gets a wake, and the room restarts.

### @LETTER@3. The opt-out

1. Run `atrium stop --now` with a working card.

**Expected:** no prompt is typed and no wake is queued. The room winds down at once.
