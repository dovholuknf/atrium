# Runner switch: moving a card to another runner, for example when a Claude account runs out

Status: design by @rnd, 2026-10-02. Nothing built. Asked through the orchestrator: when a Claude account hits its
limit, move a card's work to codex, opencode or gemini and carry on, then come back.

It builds on `docs/rnd/room-handoff-design.md` (`atrium move`, @review OK b56ca32f; stage M1 is at @runtime) and
on `docs/rnd/opencode-token-routing.md` (landed e809db3e).

Read for this design, 2026-10-02:
- `internal/store/harness.go:150` (the seeded runner rows);
- `docs/runtime/other-runners.md` (codex hooks, gate, trust and resume, measured on codex-cli 0.153.2);
- `internal/daemon/newcontext.go:84` (the capture prompt);
- `internal/store/limits.go` and `internal/daemon/telemetry.go:234` (the limit readings);
- the `harness` table on m1mini. It has `claude` and `codex` enabled and `ollama` disabled. There is no opencode
  row and no gemini row.

## 0. The answer

- **The same verb.** The command is `atrium move <card> [room] --runner <runner> [--model <model>]`. Every phase of a
  room move carries over unchanged:
  - the check;
  - the freeze;
  - the capture;
  - the park;
  - the launch;
  - the cut-over;
  - the `moved_to` chain that follows messages.

  A runner switch is a move whose destination is a runner as well as, or instead of, a room. On the same room, the
  git and file steps are skipped, as for a same-machine move.
- **The conversation never carries across runners.** Each runner keeps its own transcript format, and none can resume
  another's. So `--runner` forces `--fresh`: the successor starts cold from a brief. What carries:
  - the handoff, or the recap when there is no handoff;
  - the open todo list;
  - the branch and worktree;
  - the project memory, as text in the brief;
  - the messages queued for the card.
- **Coming back keeps the original Claude conversation.** The old Claude card stays parked, with its transcript.
  `atrium move <card> --back` does three things:
  1. It ends the stand-in.
  2. It resumes the original Claude conversation.
  3. It types in what the stand-in did, from the stand-in's own handoff and commits.

  Only one runner is live on the work at any time.
- **The account that ran out cannot write its own handoff.** The capture of a move is a prompt that needs a model
  turn, and an exhausted account will not take one. So two things change:
  1. When a card's limit reading crosses 90%, atrium captures early, while turns still work.
  2. When no fresh capture exists, the hub builds the brief from data alone. That data is the last capture, the
     card's recap, its todo list read from the transcript, its branch and recent commits, and its queue.
- **Triggered by hand first.** That means a board action, the CLI or `atrium_move`. Later, a usage limit files a
  numbered decision in clint's list (`docs/rnd/operator-focus.md`). For example: "Claude is at 95% of the weekly
  limit, which resets Tuesday. Move @fabric and @ui to codex?" clint answers it. Nothing switches by itself until
  clint sets a rule for it.
- **Not every card may switch to every runner.** The opencode routing findings hold. A cheap model finds and cites
  well and judges badly, so judgment cards (@review, security work, designs that land without a check) may switch
  only to another frontier runner, which today is codex. OpenCode Go models are also held by that doc's terms
  question and its public-repo-only rule.

## 1. What is there today

- **The move design** carries the conversation by default and has `--fresh` for a cold start from the handoff
  (room-handoff section 4). It has no `--runner`. Launch carries `harness` and `model` from the old card, so a
  successor today is always the same runner.
- **Runners:**
  - **claude** reports everything.
  - **codex** has the same twelve hooks, its gate works on `PreToolUse`, and it resumes by session id. Its
    hooks only run after the operator has approved them once in a codex session (other-runners "Trust"). The seeded
    row has `RulesSource` empty, so atrium imports no permission rules for it.
  - **opencode** is a plugin (`scripts/opencode/atrium.js`) with a gate that fails open, and it has no row on m1mini.
  - **gemini** has no row, no plugin and no measurements.
- **The capture** (`newContextCapture`) is one typed prompt asking the model to write `HANDOFF.<alias>.md`. It needs
  a working turn.
- **Limit readings.** A card's statusline reports its five-hour and weekly percent and reset time. The room keeps a
  row only when they change (`store/limits.go`). The usage tab shows them, and nothing acts on them.
- **Usage rows** are read from Claude transcripts only. A codex or opencode card's tokens are not counted
  (opencode doc section 4).

## 2. The check, for a runner switch

The room-handoff check (section 2 there) runs as is, plus these rows. One failure refuses the move, and nothing has
changed.

| # | Row | Refused when |
| --- | --- | --- |
| R1 | **The runner is enabled on the destination room** | no row, or `enabled = 0` |
| R2 | **Its login works** | the room's preflight for that runner fails (`POST /v1/preflight`) |
| R3 | **Its gate is in place** | codex: the atrium hook is not in `$CODEX_HOME/hooks.json`, or has not been approved once there. opencode: the factory permission config of opencode doc section 4 is missing. Then a dead daemon would leave the card ungated |
| R4 | **The card may use it** | the card's allowed runners do not include it (section 5). By default a judgment card may use only claude and codex |
| R5 | **The repo may go to it** | an OpenCode Go model on a private repo, while opencode doc question 2 says public only |
| R6 | **The model is named** | no `--model`, and the runner row has no default model |
| R7 | **A brief can be built** | no capture newer than the last commit, and no recap. The hub-built brief of 3.2 then has too little to go on. `--force` takes it anyway |

## 3. What the successor gets

### 3.1 The brief

The successor's first prompt is one paragraph. It points at `BRIEF.switch.md`, which the hub writes into the
worktree through `internal/safepath`, and which is never committed. It holds:

1. **Where you are.** You took over `<alias>` from Claude on room `<room>`. The worktree is `<path>` on branch
   `<branch>`. Answer this with `atrium_say` and act on nothing until you are told you are live, as in a move.
2. **The handoff.** The newest `HANDOFF.<alias>.md`, if its capture token is newer than the last commit. Otherwise the
   hub-built brief of 3.2, marked "built from data, not written by the card".
3. **The todo list.** The last todo list in the transcript, read by the room from the jsonl (the input of the last
   `TodoWrite` call). It is plain JSON, so no model is needed.
4. **The project's rules.** "Read `CLAUDE.md` in the repo root and in each folder you work in." Atrium does not write
   an `AGENTS.md` or `GEMINI.md` into the repo, because that would be a change to the repo.
5. **The memory.** The text of the card's project memory files (`~/.claude/projects/<encoded cwd>/memory/`), under a
   heading that says they were notes from the previous runner.
6. **What not to do.** The pause and its exceptions as they stand. Then, for a cheap runner, the limits of section 5:
   do not give verdicts, and do not land anything without a review.

The queued messages are not in the brief. They are delivered after the cut-over, as in a move (room-handoff 3.3
step 3).

### 3.2 The brief built from data, when the account cannot write one

The hub fills a template, with no model:
- the last capture, whatever its age, with its date;
- the card's `recap` and `recap_at`;
- the last `ask`, if one is open;
- the todo list of 3.1 item 3;
- `git log --oneline` on the branch since it left `claude/main`, and `git status --short`;
- the subjects of the last 10 messages the card sent and the last 10 it received, from the `say` table.

This is worse than a real handoff. It has no reasons and no "what I was about to do". That is why atrium captures
early (3.3).

### 3.3 The early capture

When a card's weekly or five-hour reading crosses **90%**, and the card has a worktree with commits not yet landed,
the room runs the capture prompt of `newContextCapture` without the clear that follows it in a new-context cycle.
This happens once per card per reset window.

- It costs one turn per card, about $0.40 on an Opus director at today's contexts (operator-focus section 4.2).
- It is skipped for a card that captured in the last hour.
- It is skipped for a card that is mid-turn. That card is captured at its next idle, if the limit still allows it.

## 4. Coming back

`atrium move <card> --back`, or the reverse decision in clint's list when the limit resets:

1. The check of section 2, against the original runner: its login works, and its limit reading is under 50% or past
   its reset.
2. Freeze the stand-in, and capture its handoff. It is on a working runner, so this capture is a real turn.
3. Park the stand-in. It stays done with `moved_to`, so its history stays one click away.
4. Resume the original Claude card. It is a parked card with its own transcript, reopened the ordinary way, with a
   first prompt that says:
   - "While you were out, `<runner>` worked as you on this branch";
   - read `HANDOFF.<alias>.<runner>.md`;
   - the commits since you left are `<range>`;
   - act on nothing until you are told you are live.
5. The cut-over as in a move. The alias goes back, `moved_to` points forward again (a chain A to B to A, which
   room-handoff section 5 already allows), and the queue is forwarded.

The Claude card's context then holds its own thread plus the stand-in's summary, which is better than either cold
start. When the stand-in ran for days, its handoff can be long. The resume then works like an ordinary new-context
wake.

## 5. Which cards may switch to which runner

A card carries `allowed_runners`, set by default from its role tag:

| Card | Default allowed runners | Why |
| --- | --- | --- |
| @review, any card tagged `role:review` | claude, codex | verdicts are judgment. Codex is another frontier model, so a verdict from it is a different model's verdict, and the verdict file says which runner wrote it |
| @rnd, and design workers | claude, codex | designs land only after @review, but they are judgment too |
| @runtime, @fabric, @ui, and build workers | claude, codex, opencode (public repos only) | their work is reviewed before it lands |
| a throwaway search or draft worker | any enabled runner | the opencode routing fits 2 and 3: Claude reads the result |

When a card is switched to a runner outside its default, the board shows the runner on the card face in the
attention color. clint can widen the list per card, and that is a numbered decision, not a flag a director sets.

## 6. Stages

Each stage is useful alone. All are held by the pause.

| stage | what | owner | size | acceptance |
| --- | --- | --- | --- | --- |
| S0 | **No build, now.** A director whose account runs out writes its handoff while it still can, and the orchestrator launches the stand-in by hand with the brief of 3.1 | every director | none | done when used once |
| S1 | `--runner` and `--model` on the move: R1 to R7, a forced `--fresh`, `BRIEF.switch.md` through safepath, the todo read from the jsonl, and the memory as text. Same room only | @runtime, after M1 | 2 days | a throwaway claude worker with an open todo and two queued says switches to codex on one room. The codex card reads the brief, names the todo items, gets the two says after the cut-over, commits on the same branch, and reports to the launcher. A switch with codex hooks unapproved is refused at R3 and nothing changes |
| S2 | `--back`: resume the original card with the stand-in's handoff and commit range | @runtime | 1 day | the worker of S1 comes back to claude. Its first reply names a commit the codex card made, and the alias, pins and `report_to` are the same as before S1 |
| S3 | The brief built from data (3.2), and the early capture at 90% (3.3) | @runtime | 1.5 days | with capture blocked (a test runner that refuses turns), a switch still builds a brief that has the recap, the todo, the commit list and the last says. A reading crossing 90% captures once, and a second crossing in the same window does not |
| S4 | Across rooms: S1 and S2 through the hub verb | @fabric, after M2 | 1 day | a worker on sg3 switches to codex on m1mini and back |
| S5 | The limit trigger: a reading at 95% with a reset more than 6 hours away files a numbered decision in clint's list naming the cards on that account and a suggested runner each. An answer runs the moves | @fabric, @ui, after operator-focus L1 | 1 day | a faked 95% reading files one decision. A `yes` moves the named cards, and `no` moves nothing |
| S6 | Usage rows for codex and opencode, so a stand-in's spend shows on the usage tab | @runtime | 1 day | a codex card's turns appear in `session_usage` with its model and a price |

Opencode as a target needs the opencode doc's stage for the factory permission config first. That is R3.

## 7. Questions for clint, in plain words

1. **What happens when your Claude limit runs out.** Atrium asks you, in your decision list, whether to move the
   affected cards to another AI tool, and does it only on your yes? Or should it switch on its own, by a rule you
   set once? **Suggested: ask each time, until you have seen it work twice.**
2. **Which tool stands in.** For cards that review and design, only codex (OpenAI's tool, a model as capable as
   Claude), and for the cards that build, codex or the cheaper OpenCode models on public repos? **Suggested: yes,
   codex first everywhere, OpenCode only after the bake-off and the terms answer.**
3. **Codex's one-time approval.** Codex runs atrium's safety checks only after you have approved them once in a codex
   session on each machine. Will you do that once on m1mini and sg3, so a switch is not refused? **Suggested: yes,
   after the pause, 2 minutes per machine.**
4. **The early save.** When a card is at 90% of a Claude limit, it writes its notes for a successor, which costs about
   40 cents a card. Then a switch has good notes even when the account is out. **Suggested: yes, only for cards with
   work not yet landed.**
