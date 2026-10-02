# atrium blog posts

Status: captured by @rnd for the blog expedition, 2026-10-02. Each post has a stub in `docs/blog/<slug>.md`, with its
angle, story beats, the screenshots or demos it needs, and its sources. The feature inventory the posts draw on is
`docs/blog/inventory.md`.

**Count: 47 posts in 10 series.** Status: 41 idea, 6 outline. None is drafted yet.

## Start with these five

Chosen because each is a story only atrium has, and reaches beyond atrium's own users. Posts 2, 4 and 5 lean on
off-repo figures, so their stubs list what to check before drafting:

1. [The interview that deleted a design](the-interview-that-deleted-a-design.md): A reviewed, approved design was thrown out at question 4 of an interview, when it turned out the human pictured something else.
2. [What a factory of agents costs: $1,202 in 41 hours](what-a-factory-of-agents-costs.md): Directors against flat workers against a human driving one model, and how much of the money went on relaying messages.
3. [The Enter key that approved a tool call](the-enter-key-that-approved-a-tool-call.md): Typing a message into a session wrote text and then Enter; with a permission dialog on screen, Enter picked the highlighted option.
4. [The org ran out of work at 02:40](the-org-ran-out-of-work-at-0240.md): Four hours, about thirty items and ten thousand lines landed, and then every director sat idle on questions only one person could answer.
5. [Remote rooms did not add capacity](remote-rooms-did-not-add-capacity.md): The plan was more machines, more workers; one remote room ran a stale build, a trust dialog ate two workers, and the one finished branch could not get home.

## Every post, by series

Columns: title, hook, audience, status, and the features and docs it rests on.

### The daemon and the halt (7)

What a supervisor of agents does when its own state breaks, restarts or reboots.

| post | hook | audience | status | rests on |
| --- | --- | --- | --- | --- |
| [From chat window to kanban: atrium's first version answered the wrong question](from-chat-window-to-kanban.md) | v1 was a TUI broker whose hub forgot everything by rule; v2 had to reverse that rule to become useful. | people building agent tooling | idea | v1 Mode A broker, v2 SQLite task board |
| [Stop taking work when the database fails, and when that rule halted every room](halt-on-storage-failure.md) | atrium halts when its store fails, which is right, until a unique-name clash counted as a store failure and stopped every agent. | people building daemons and supervisors | idea | the halt, the health event, constraint errors never halt |
| [The restart that never came: a room is never idle for ten seconds](the-restart-that-never-came.md) | A HIGH fix waited seven hours because the safe-restart rule needed a quiet room, and a room running ten agents is never quiet. | people running agents unattended | idea | restart_atrium, room deploy hold, hub restart gate, rolling restart design |
| [A zombie card resumed another room's conversation](a-zombie-in-another-rooms-conversation.md) | The orchestrator moved to a new room; on the next restart its old card came back on the same conversation, and messaged its living self. | people building session supervisors | idea | restart reopens what was open, asked-exit is sticky |
| ["ConPTY has no reattach", until a spike showed it does](conpty-has-no-reattach-until-it-did.md) | A limit written into four docs as fact turned out to be false, and a pty host now outlives the daemon. | Windows and terminal engineers | idea | pty host (off by default), rolling restart stage 0 |
| [When your tests talk to production: test runs posting cards to the live board](when-your-tests-talk-to-production.md) | Tests read the machine's shared address file and reported to the live room, twice, until a test guard sealed them off. | anyone whose tests run on the same box as the real thing | idea | internal/testguard, ATRIUM_LOCATION hand-down |
| [Starting at boot with nobody logged in: why not a Windows service, and a $2,500 Linux room](starting-at-boot-with-nobody-logged-in.md) | A service runs in session 0 and supervises nothing you can attach to; a Mac LaunchAgent waits for a desktop login; Linux just works. | people running agent machines at home | idea | logon task autostart, room autostart, room machine spec |

### The permission chain and auto mode (3)

Every tool call an agent makes passes one gate, and what went wrong around it.

| post | hook | audience | status | rests on |
| --- | --- | --- | --- | --- |
| [The Enter key that approved a tool call](the-enter-key-that-approved-a-tool-call.md) | Typing a message into a session wrote text and then Enter; with a permission dialog on screen, Enter picked the highlighted option. | anyone automating input into agent terminals | outline | dialog guard, typing gate, Notification hook |
| [Auto mode for the next hour, and who said yes](auto-mode-for-the-next-hour.md) | Approve everything for an hour, except what a never rule forbids, and keep a record of whether you, a rule, or auto said yes. | people who run Claude Code with permissions | idea | auto mode, standing rules, decision log, hub-held board-wide auto |
| [A permission gate in the binary, and why it fails open](a-permission-gate-that-fails-open.md) | From a PowerShell script in the operator's dotfiles to `atrium hook --event permission`, with a 24-hour timeout and a deliberate fail-open. | people writing Claude Code hooks | idea | permission gate, machine-wide gating, codex as a second target |

### Cards, not agents (6)

Why the unit on the board is a card (a task with a live session), and what that shape cost and bought.

| post | hook | audience | status | rests on |
| --- | --- | --- | --- | --- |
| [The board filled with ghosts](the-board-filled-with-ghosts.md) | Adopting every session a ledger had ever seen made hundreds of cards waiting on a human who could not answer them; now a narrow 'adopt' is wanted again. | people building dashboards over agent sessions | outline | SessionStart/SessionEnd hooks, the abandoned ledger adoption, the wanted `atrium adopt` |
| [Cards, not agents](cards-not-agents.md) | The unit on atrium's board is a card: a piece of work with a live session, a history, an owner and a state, not an agent persona. | people designing agent UIs | idea | attention columns, seen tracking, archive, inbox, card URLs, handles |
| [Removing the dollar figure, then pricing everything](removing-the-dollar-figure-then-pricing-everything.md) | The board stopped showing cost the day before a cost study became the factory's main design input. | people tracking LLM spend | idea | usage tab, usage backfill, factory-shape cost study |
| [Keeping a prompt cache warm on purpose](keeping-a-cache-warm-on-purpose.md) | Coming back to a big session after lunch cost a noticeable share of a week's limit, so atrium refreshes idle caches with forked resumes and stops at break-even. | heavy Claude Code users | idea | cache keep-alive, idle parking |
| [The growler: built as a priority, switched off two days later](the-growler.md) | An alert that stayed until you acted on it, and why the operator asked for its job to move into the bell instead. | people designing notifications | idea | persistent growler, bell, reminders ladder |
| [Running an agent org from a phone](an-agent-org-on-a-phone.md) | Most decisions now come from a phone, often as one word, so the phone page had to make one word enough. | mobile and agent-UI people | idea | the /m page, phone terminal, replies API, changes API |

### Supervision and terminals (4)

Owning other programs' terminals on Windows, macOS and Linux, and every way that broke.

| post | hook | audience | status | rests on |
| --- | --- | --- | --- | --- |
| [Twice fixed, twice reverted: rebuilding scrollback through a screen model](twice-fixed-twice-reverted.md) | Colour codes stacked six deep in one cell, a grid that grew instead of scrolling, and conhost losing ten lines on a height flip. | terminal emulator people | idea | screen-model replay, height hold, repaint loss report |
| [Two characters nobody typed](two-characters-nobody-typed.md) | Clicking into a terminal sent a focus report, the typing gate counted it as typing, and messages waited on an empty line for nine minutes. | terminal and automation people | idea | typing gate |
| [Agents that clear their own memory](agents-that-clear-their-own-memory.md) | Directors ran at three or four times their context threshold until atrium learned to capture a handoff, clear, and wake them itself. | people running long-lived agents | idea | new-context cycle, context ceiling, autocompact |
| [Docs that still say the old rule](docs-that-still-say-the-old-rule.md) | The README promises peer messages are never typed and ptys never outlive the daemon; both changed. | maintainers of fast-moving projects | idea | peer bus typed delivery, pty host |

### Rooms, the hub and federation (5)

One board over many machines, and the hub that was meant to hold nothing.

| post | hook | audience | status | rests on |
| --- | --- | --- | --- | --- |
| [The hub that was supposed to hold nothing](the-hub-that-was-supposed-to-hold-nothing.md) | From 'federate in the client' to a hub that owns integration branches, documents and a git store, one decision at a time. | distributed-systems people | idea | hub/room split, decisions 11 and 19, hub documents, hub git store |
| [Two ways a hub knocked its rooms off](two-ways-a-hub-knocked-its-rooms-off.md) | One slow room stalled every attach because the hub waited for all rooms to answer; later a frame-order race kept rooms off for minutes. | people building hub-and-spoke systems | idea | hub/room link, attach proxy |
| [Remote rooms did not add capacity](remote-rooms-did-not-add-capacity.md) | The plan was more machines, more workers; one remote room ran a stale build, a trust dialog ate two workers, and the one finished branch could not get home. | people scaling agents across machines | outline | rooms, launch caps, room build check, git sync |
| [What it takes to make a machine contribute](what-it-takes-to-make-a-machine-contribute.md) | Antivirus at 103%, a byte-order mark that failed provisioning with exit 6, and Cygwin git first on the path. | people provisioning dev machines for agents | idea | provision-room, room requirements, room-defender |
| [When the Claude account runs out](when-the-claude-account-runs-out.md) | Moving a live card to codex or opencode: the conversation can never carry, and an exhausted account cannot write its own handoff. | people mixing coding agents | idea | runner switch design, room handoff design |

### The software company: directors and workers (10)

An org chart of AI agents run by one human, what it cost, and where it jammed.

| post | hook | audience | status | rests on |
| --- | --- | --- | --- | --- |
| [What a factory of agents costs: $1,202 in 41 hours](what-a-factory-of-agents-costs.md) | Directors against flat workers against a human driving one model, and how much of the money went on relaying messages. | engineering leads weighing agent orgs | outline | factory-shape study, usage tab, operator-focus token re-eval |
| [An org chart of AI directors](an-org-chart-of-ai-directors.md) | Five resident directors, workers per item, one merger, a reviewer that gates every commit, and one human. | people designing multi-agent systems | idea | aliases, control MCP, worker tool set, report-to, merged cull |
| [The org ran out of work at 02:40](the-org-ran-out-of-work-at-0240.md) | Four hours, about thirty items and ten thousand lines landed, and then every director sat idle on questions only one person could answer. | anyone handing work to agents overnight | outline | decision list, held questions, the pause, factory evaluation |
| [Who a message wakes costs more than what it says](who-a-message-wakes.md) | The same short message cost about eleven times more to the reviewer than to a director, because of what each re-reads per turn. | people building agent messaging | idea | atrium_say fyi and needs kinds, held notices |
| [Removing the middle manager](removing-the-middle-manager.md) | The orchestrator rewrote dozens of reports a day for the human; the plan moves that relay into plain code on the hub. | people building agent orgs | idea | factory refactor, clint inbox, decision list |
| [The pause: an AI company that can only write plans](the-pause.md) | The human stopped every build and deploy; the org kept going, and produced eight designs in a day, all reviewed. | engineering leads | idea | the pause, named exceptions, freeze and budget design |
| [What the review HOLDs caught](what-the-review-holds-caught.md) | A reviewer agent that holds about one change in five, mostly with a proof: forgeable facts, a stale picture, the human quoted in a public repo. | people adding review agents | idea | Atrium-Verdict trailers, deploy-ready, the review director |
| [Working notes leaked into a public branch](working-notes-in-a-public-repo.md) | A director's handoff and plan files were tracked, and a merge carried them into the public history. | people running agents in public repos | idea | gitignore for working notes, merge-check guard, held commits |
| [A grade of D+ for a cheaper model](a-grade-of-d-plus.md) | A cheaper model's audit cited files well but got the severities wrong, which became the rule: cheap models only where a stronger one checks. | people routing work to cheaper models | idea | opencode runner, opencode token routing design |
| [The persona pack that lived for a day](the-persona-pack-that-lived-for-a-day.md) | Six stages of specialist agents with memory were built and reverted the same day; personas came back two weeks later as standing reviewers. | people building agent personas | idea | persona pack (reverted), standing reviewers design |

### The one human (3)

Deciding, interviewing and approving from a phone, as the only person in the org.

| post | hook | audience | status | rests on |
| --- | --- | --- | --- | --- |
| [The interview that deleted a design](the-interview-that-deleted-a-design.md) | A reviewed, approved design was thrown out at question 4 of an interview, when it turned out the human pictured something else. | people writing designs for others | outline | hub forge rev 1 and rev 2, interviewer brief, interview mode |
| [How to interview a human about a design](how-to-interview-a-human-about-a-design.md) | Scenario first with real machine names, 'this happens, then this, then what?', one default each, and never re-ask. | anyone who asks people questions for a living | idea | interviewer brief, interview mode |
| [Fifty forgotten decisions](fifty-forgotten-decisions.md) | Counting the decisions an operator of an AI org skipped, and why the fix is a numbered list parsed by plain code, not a smarter model. | people working with many agents at once | idea | decision list, typed effect, safe defaults, one reminder a day |

### Overlays and zrok (2)

Reaching a board and its rooms over OpenZiti and zrok without atrium holding an identity.

| post | hook | audience | status | rests on |
| --- | --- | --- | --- | --- |
| [The zrok outage that was not ours](the-zrok-outage-that-was-not-ours.md) | Every share request answered 500 with an empty body, and the work could not be proven, so the repo got an upstream-ready report. | zrok and OpenZiti users | idea | zrok board share, reserved names |
| [Serving a board on an overlay without holding an identity](a-board-on-an-overlay-with-no-identity.md) | atrium serves its board straight on a zrok share or an OpenZiti service, and a public share is refused without a login. | OpenZiti and zrok users | idea | native overlay listeners, published-board login, room identity inside the overlay |

### Intake and PR review (3)

How work gets in, and a PR review that atrium runs but never posts.

| post | hook | audience | status | rests on |
| --- | --- | --- | --- | --- |
| [A PR review that never posts](a-pr-review-that-never-posts.md) | From an hour and fifty minutes and $7 to a target of ten minutes and $2, by forking one primed session for every reviewer. | people using agents for code review | idea | pulls tab, PR review runner, walk drawer |
| [Before you contribute: the policy gate](before-you-contribute.md) | A day of work was done before anyone checked whether the upstream accepts AI contributions or needs a CLA. | people contributing to open source with agents | idea | outside-repo contribution gate, change lifecycle stage 0 |
| [A chat message becomes work: the OpenWiki factory test](a-chat-message-becomes-work.md) | A colleague's chat message turned into a spike, a build and a factory test, and showed thirty-one gaps. | people building agent intake | idea | inbox, sources, intake design, OpenWiki spike |

### The hub forge and the change lifecycle (4)

Git between rooms, verdicts as trailers, and the stages a change passes before clint signs it.

| post | hook | audience | status | rests on |
| --- | --- | --- | --- | --- |
| [Code moves by fetch, never by push](code-moves-by-fetch.md) | Agents may not touch remotes, so the hub collects rooms' branches and serves main read-only, and a reviewer's verdict is a git trailer. | people moving code between agent machines | idea | hub git sync, deploy-ready, verdict trailers |
| ["Tested at abc1234, 3 commits ago"](tested-at-abc1234-three-commits-ago.md) | Every fact about a branch is pinned to a sha the room stamps, so a session stops answering 'yes, it's tested' from memory. | people who ask agents 'is it ready?' | idea | change record design |
| [Eight stages and a signature](eight-stages-and-a-signature.md) | A change is walked through only when the human approved it hunk by hunk on a screen, and the agent cannot push early because the walls are credentials, not instructions. | people letting agents prepare PRs | idea | change lifecycle design, hub forge |
| [The hub as a forge, without a mirror](the-hub-as-a-forge.md) | The hub owns main and takes pushes with plain git rules; everything else passes through live to the room that has it, and fails when that room is off. | self-hosted git people | idea | hub git store, repos tab, hub forge rev 2 |

## How these were found

- Two read-only digs of the repo: the shipped half from README, FEATURES, the changelogs, the docs and the git log,
  and the designed and wanted half from `docs/rnd` and `docs/backlog`. Their merged result is `inventory.md`.
- The orchestrator's factory status and evaluations, and the factory log. These are kept off the repo because they
  quote clint, and are used here as paraphrased sources only.
- The recap index on sg4 has no atrium rows, so nothing from it is used here.

## Left out on purpose

- Security work: the audits and their fixes are not blog material in this pass.
- Posts about the other repos the factory works in. The recap index covers them, and they would need their own pass.

## Questions for clint, held until he asks

1. **Where the posts go.** A post is drafted from its stub. Should it go on the atrium docs site, on a personal or
   company blog, or both? **Suggested: the docs site's blog, because it is the same repo and is already public.**
2. **Naming the machines and agents.** The stubs use real names: sg4, m1mini, @review and the rest. Should the posts
   keep them? **Suggested: yes, because they make the stories concrete.**
3. **Real costs.** Several posts quote real dollar and token figures. Should they stay in? **Suggested: yes, as
   measured, with the date.**
4. **The working notes still in history.** A director's handoff and plan files were merged into the public branch
   and later removed from the tree, but the commits are still in history. Should history be rewritten to drop them,
   or left as it is? Until then, no post names those commits or says what the files held. **Suggested: your call.
   A rewrite changes every later sha on claude/main.**
