# Operator focus: one numbered list of decisions, shorter reports, and what the tokens buy

Status: design by @rnd, 2026-10-02. Nothing built. Asked by clint through the orchestrator, 2026-10-02. In short: he
keeps losing track of things, about 50 of his decisions were skipped or forgotten, the directors' reports to him are
too long, and he wants one list answered by number. clint is not quoted here because this repo is public. His words
are in the factory log, which is held until he decides where it lives.

Inputs, all read 2026-10-02:
- `docs/rnd/factory-refactor.md` revision 2 (held), item 3 and section 2;
- `docs/backlog/rnd/rnd-new-clint-inbox.md` (held);
- `docs/rnd/held-message-escalation-design.md`;
- `docs/rnd/usage-tab-design.md` section 1 (the counting rule);
- `docs/rnd/lean-context-cycle-design.md`;
- the factory log's second entry (held), used for counts only;
- m1mini's `~/.atrium/atrium.db`, tables `say`, `session_usage` and `task`, opened read-only;
- the price table at `internal/daemon/keepalive.go:109` and `internal/daemon/usage.go:70`.

**A writing rule for every question to clint.** Plain words, and no internal names. Say what a thing does in one
sentence before naming it. Recap the item when asking. Give one suggested answer.

## 0. The answer

- **One numbered list of every open decision, kept on the hub.**
  - Each decision gets a number that never changes and is never reused.
  - The board and the phone show the same list.
  - clint answers several at once by number, for example `12 yes, 14 no, 15 b, 20 later`.
  - The hub hands each answer to the card that asked. No model is in between.
  - This is the hub inbox already filed as `rnd-new-clint-inbox`, plus the numbers, batched answers, safe defaults,
    idea capture and a daily summary page (section 2).
- **The smallest first step needs no build, and half of it already exists.** The orchestrator keeps
  `notes/DECISIONS-OPEN.md` on sg4 (51 items, outside the public repo). From today, every director:
  - refers to a decision by that number;
  - files a new question as a new number instead of re-asking in a report.
  - clint answers by number in batches (section 5, stage L0).
- **Reports in two parts** (section 3).
  - A "for clint" part goes to clint: at most five lines, saying what changed for him and which numbered decisions are
    his.
  - A "for the record" part stays in a file and is never sent.
  - Says to the orchestrator average 1,207 characters today. The "for clint" part targets 400.
- **What the tokens buy** (section 4). On m1mini, 44 hours of work cost about **$167** at atrium's prices.
  - Two thirds of that is cache reads, the cost of re-reading each card's context at every turn.
  - **Half of it ($85) was turns started by a message from another card.** The cost of a message is the turn it
    wakes, and that turn's cost depends on who reads it. A message to @review cost $0.78 on average, and one to @ui
    $0.07.
  - The cheapest saving is fewer wake-ups of the cards with the largest contexts.

## 1. What is there today, and where things get lost

### 1.1 Where decisions live now

Open decisions are spread over six places, and nothing ties them together:
- the "Questions for clint" section of each design doc;
- the orchestrator's conversation, which held the list of 50 (groups A to H) until it was written to
  `notes/DECISIONS-OPEN.md` on sg4;
- the per-day rnd queue files on sg4;
- a card's own question (`ask`, `ask_at`, `ask_peer` on the task row, shown by `atrium_peers` through
  `internal/daemon/peers.go:168`). It holds one question per card, with no number and no history;
- director reports to the orchestrator, which re-numbers questions per message;
- clint's replies, typed into whichever card he is looking at.

### 1.2 Where things get lost

Counted from the factory log's two entries (held). These are things the setup does to clint, not his fault:

1. **Answering the newest and skipping the older.** clint decides in bursts from the board and the phone. A question
   that has scrolled out of view is never answered. The same question was asked up to three times with no answer.
   *Fix: a list that keeps its order and shows the age of each item.*
2. **Several asks in one message.** One message started four threads. Only some came back as work, and nothing
   marked the others as taken. *Fix: capture each as its own row (section 2.5).*
3. **Answers with no number.** A "yes, do it" while several questions are open is matched to one by a guess. *Fix:
   answers by number, and an answer with no number gets one question back: which one?*
4. **The only copy lives in a model's context.** The list of 50 lived only in the orchestrator's conversation. A
   context clear or a lost card would have lost it. *Fix: hub rows, and meanwhile a file.*
5. **Internal names.** Names such as "landing op" reached him unexplained. *Fix: the writing rule above, applied by
   each director (section 3).*
6. **Interruptions that cost more than they give.** He asked to turn off the pop-out notifier and to move what it does
   into the bell. *Fix: one daily batch plus a rate-limited "urgent" (section 2.6).*
7. **A message arriving mid-redraw** may have cut off the end of a reply on his screen. This is unproven, and it goes
   to @ui as a bug to reproduce, not to this design.

## 2. (a) The decision list

### 2.1 A row

The hub inbox row of `rnd-new-clint-inbox`, with these fields added:

| field | what it holds |
| --- | --- |
| `number` | stable per hub, shown as `#123`, never reused |
| `group` | a short area such as "PR review" or "machines", suggested by the asker |
| `recap` | one sentence on what the item is, for a reader who has forgotten it |
| `question` | the question, in plain words |
| `options` | `yes`/`no`, or lettered choices `a`, `b`, `c` |
| `suggested` | one option, with one line of why |
| `blocks` | what waits on it: a card, a stage, or nothing |
| `effect` | what the suggested answer changes, from a closed list: `design-choice`, `stage-order`, `setting-default`, `wording`, or `other` (section 2.3) |
| `default_at`, `default_ok` | an optional deadline, allowed only under section 2.3 |
| `state` | open, answered, defaulted, withdrawn, later |
| `answer`, `answered_at`, `via` | what clint said, when, and from where (board, phone, card) |

The asker is identified by its room certificate and card id, and rate-limited, as the inbox design says. A director
may withdraw its own row. Only clint answers one.

### 2.2 Answering by number, in batches

- On the board and the phone, the list has one text box. clint types `12 y, 14 n, 15 b, 20 later, 31 ask rnd why`.
- The hub's parser is plain code, not a model. It reads a number, then `y`/`yes`, `n`/`no`, a letter, `later`, or free
  text.
- Each part becomes that row's answer and is handed to the asking card, through the relay as today.
- Anything the parser cannot read (no number, an unknown number, a letter the row does not offer) is shown back on the
  spot as one line, and nothing is sent.
- **Only the list's box parses.** Text typed into a card's input goes to that card, unparsed, whatever it starts
  with. Otherwise "3 files are wrong, fix them" would answer #3. The one exception is the explicit form `#12 y` at
  the start of a card's input. The board then shows "answer #12: yes?" and sends it only on a confirm, and an
  unconfirmed one goes to the card as typed.
- Free text goes to the asker as information, never as an instruction to act.

**An answer is information, not authorization**, exactly as in the inbox design. A yes on a row that would publish,
delete, deploy or spend does not count as approval. That action still meets its own gate when it runs.

### 2.3 A default on a deadline, only where it is safe

A row may say "if clint has not answered by Friday, take the suggested answer". The rules for when that is safe:
- the suggested answer changes only atrium's own internal work;
- it can be undone;
- it publishes nothing, deletes nothing, deploys nothing, spends nothing, and contacts no one outside;
- it is not about another person, another organization's repo, an account, or a term of service.

The hub cannot read free text, so it cannot check these rules itself. What it enforces is the typed `effect` field.
`default_ok` is accepted only on a row whose `effect` is one of `design-choice`, `stage-order`, `setting-default` or
`wording`. A row with `effect: other`, or with no effect, is refused a default at filing. No effect on the list
publishes, deletes, deploys, spends or contacts anyone.

The asker picks the effect, so the label can be wrong. Two checks back it up:
1. @review checks the effect when it reads the design that filed the row.
2. A defaulted answer is still only information. A gated action still meets its own gate.

The deadline is at least two days, and the row shows "defaults to *b* on Fri" from the moment it is filed. A
defaulted row stays on the list, marked defaulted, until clint confirms it or answers otherwise. A later answer
replaces the default, and the asker is told.

### 2.4 Focus limits

- **"Now" shows at most 10 rows.** The hub orders them by what they block first, then by age. The rest are under
  "Later", collapsed, with a count.
- **Each director has at most 3 rows in "Now".** A fourth goes to "Later" unless it blocks a running stage.
- **`later` is an answer.** It moves the row out of "Now" for a week, then it comes back once. A second `later` keeps
  it out until a director refers to it again.
- **Old rows get a warning.** A row open for 7 days gets a "still needed?" mark for its asker, which then withdraws it
  or keeps it.

### 2.5 Capturing ideas

- clint types `idea: ...` in the list's box, or `/idea ...` at the start of a card's input, which the board confirms
  before it leaves the card, as for `#12 y` in 2.2. A plain "idea: ..." typed to a card goes to the card. It becomes a
  row of kind `idea`, with no question.
- The hub suggests an owner from the departments' keywords and the paths named, as in refactor section 2.3. The owner
  then files it as a backlog item (and the idea row closes, with a link) or answers "dropped, because ...".
- A message with several asks is the same problem as several ideas. The director that receives it files one row per
  ask it is not acting on at once, so nothing is silently dropped.

### 2.6 Notifications

- **One batch a day.** At a fixed time clint picks, the bell gets one item: "8 decisions open, 3 new, 1 defaults
  tomorrow", which opens the list.
- **Urgent.** A director may mark a row urgent only when it blocks a running stage. Urgent rows go to the bell at once,
  at most one per director a day. A second urgent row from the same director that day is filed as an ordinary "Now"
  row at the top of the list, and it goes in the next batch. The board shows it as "urgent, held: daily limit", and
  the director is told once that the limit was reached, so it does not re-send.
- This replaces the pop-out notifier for decisions. The bell already exists, so this fits clint's ask to move that
  function into it.

### 2.7 The daily "where things stand" page

The hub builds it at the batch time from data it already has, with no model:
- **Landed since yesterday:** commit subjects on `claude/landing` and `claude/main`, grouped by department, with the
  review verdict commits counted, not listed.
- **Waiting on clint:** the "Now" rows, by group, with their age.
- **Paused:** what the pause covers, and its exceptions (refactor item 4).
- **Running:** cards per room, with any card idle more than a day.
- **Spend:** yesterday's dollars per room and per department, from `session_usage`, using the counting rule of the
  usage tab.

It is one phone screen, a link from the daily bell item. Writing it with a model would cost a turn a day and could
get
numbers wrong, so it is a template filled from rows.

### 2.8 How this ties to `rnd-new-clint-inbox`

Same rows, same route, same guard. The inbox design holds unchanged. This design adds:
- the fields of 2.1;
- the batch parser of 2.2;
- the defaults of 2.3;
- the limits and the bell batch of 2.4 and 2.6;
- the idea kind of 2.5;
- the page of 2.7.

The two should be built as one item. The inbox backlog file gets a pointer to this doc.

### 2.9 Interview mode: the decision list's sibling

Added 2026-10-02 evening. clint, after the hub-forge interview, said the process worked well and asked for a better
screen for it. An interview is a run of questions about one design, asked one at a time, where each answer can change
the next question. The decision list holds separate questions, each answered on its own. Both share the hub rows,
the board and the phone.

**The screen.** On the board and the phone, an interview shows **one question at a time, as a card**:
- **the scenario as bullets**, phrased as "this happens, then this happens, then what?" (`docs/rnd/interviewer-brief.md`
  section 3);
- **the options as buttons**, each with one line on what it would do, and **the default highlighted** with its why;
- **a free-text box**, always there, sent with a button choice or alone;
- **progress**, as "3 of about 8". The count can grow, because a later question can depend on an answer;
- **back**: any earlier answer can be opened and changed. A changed answer is sent to the interviewer as "Q2
  changed". The interviewer may then withdraw or rewrite the questions after it, and the screen marks those;
- **"I don't understand"** and **"I already said this"**: two small buttons that send the interviewer a rephrase
  request, or the point where he said it. Each is counted in the answers file, so a bad brief shows up;
- **the answers file building beside it**: his exact words, and the interviewer's "taken as" lines marked as the
  interviewer's. On the phone, this is a second tab.

**The interviewer drives it through a tool, not terminal text.**
- `atrium_interview {start}`: the topic, the doc it feeds, and the planned count.
- `atrium_interview {ask}`: the scenario bullets, the question, the options, the default and its why, and which
  earlier answers it depends on.
- `atrium_interview {taken_as}`: the interviewer's reading of an answer, recorded beside his words.
- `atrium_interview {done}`: the "open, for the designer to default" list.

Each answer reaches the interviewer card as one message, which is its one turn. The interviewer may queue up to two
questions ahead, marked "may change". The screen shows the next one at once, so clint is not waiting on a model
turn between easy questions. A changed answer withdraws the queued questions that depend on it, at once and before
the interviewer's turn. The screen shows "rewriting" in their place, so a stale next question never shows.

**Where it lives.** An interview is a hub row with its questions and answers. The answers file is written from the
row at each answer, outside every repo (`~/.atrium/interviews/<id>.md` on the hub, the directory `0700` and each file `0600`), because it quotes clint.
It follows the factory log's home once he decides it. A design doc cites the answers file by path, and paraphrases
it.

**It ties into the decision list.** Starting an interview from held questions ("interview me on #63 to #67") turns
those rows into the interview's first questions. Each answer marks its row answered, `via: interview <id>`, so the
list stays the one list. An interview is started only when clint asks for one, by his rule that no questions are
sent until he asks.

## 3. (b) Shorter director reports

### 3.1 The two parts

**For clint**, sent: at most five lines, and plain words under the writing rule.
1. What changed that he would notice, in one line.
2. Which numbered decisions are his, by number only. A new question is filed as a new row, not written out here.
3. Anything blocked, and on whom.

A sha, a review id or a file path appears only when he would click it.

**For the record**, kept and not sent: everything else. That covers commit ranges, review ids, measurements,
reasoning and alternatives.

### 3.2 Where the record lives

The repo is public, and records quote people and name customers' repos. So:
- **Now:** the record goes in a file on the director's own room, outside any repo, as
  `~/.atrium/records/<dept>/<yyyy-mm-dd>.md`. The directory is created `0700` and each file `0600`. The "for clint"
  part names the file.
- **Later:** once `docs/rnd/hub-documents-design.md` stage 1 is built, records are hub documents, readable from any
  room and from the board.
- **The factory log** gets whichever answer clint gives for it. Reports and log entries should live in the same
  place.

### 3.3 An example

Today's opencode report, the one sent at this doc's start, ran to about 1,700 characters. In the two-part form, the
part for clint is:

> The plan for using your cheaper OpenCode models is written and reviewed. In short, they only do work that Claude
> then checks. Four decisions are yours, numbered on the list. Nothing is waiting on anyone else.

That is about 250 characters. The terms, the bake-off, the plans and the review ids go in the record.

### 3.4 What it saves

- 92 says to the orchestrator from m1mini since 2026-10-01 18:33 came to 111k characters, about 28k tokens of
  output, averaging 1,207.
- At 400 characters, that is about 37k characters. The output saved is small, about $0.50 at Opus output prices.
- **The real saving is in the reader's turns.** Each say wakes the orchestrator on a context past 100k tokens, and the
  orchestrator then rewrites the say for clint. With the "for clint" part going straight to the hub list (stage L1),
  both the wake and the rewrite go away. That is the 4 million context tokens a day in refactor section 2.2.

## 4. (c) The token and efficacy re-evaluation

### 4.1 Method

usage-3 (card `01a0f86f`, "token usage evaluation, 09-30 17:15 to now") has shown "running" since 2026-10-01 17:07
and published nothing. Its card stores no brief, so its method is not on m1mini.

This section therefore uses a method laid out step by step, so it can be re-run:
1. Read `session_usage` rows from 2026-09-30 21:23 to 2026-10-02 17:11 (the m1mini history).
2. Price each turn at atrium's table:
   - Opus 5.5: input $4, 5-minute write $5, 1-hour write $8, cache read $0.20, output $20 per million;
   - Sonnet 5.5: $2, $2.50, $4, $0.20, $10.
   The `cost` column is zero on every row in this window, so the price is worked out in the query.
3. Split by the row's `cause` (`say`, `operator`, `subagent`, `resume`, `keepalive`), by card and by model.
4. For efficacy, divide by the non-merge commits that landed on `claude/landing` in the window: 418, of which 164
   are review or verdict commits.

If usage-3 was meant to use another method, the orchestrator can forward its brief, and this section will be re-run
on it.

### 4.2 What it shows (m1mini only)

**Total: $167.12.** Cache reads are $111.74 of it (67%).

| what started the turn | turns | cache read | cost | share |
| --- | --- | --- | --- | --- |
| a message from another card (`say`) | 203 | 304M | $84.53 | 51% |
| someone typing into the card (`operator`) | 101 | 212M | $61.58 | 37% |
| a subagent finishing | 36 | 33M | $14.36 | 9% |
| a resume | 7 | 6M | $5.55 | 3% |
| keep-alive | 9 | 3M | $0.67 | 0.4% |

| card | model | turns | cost | per turn | average context |
| --- | --- | --- | --- | --- | --- |
| @review | Opus 5.5 | 61 | $41.39 | $0.68 | 336k |
| u-m-card (a worker) | Sonnet 5.5 | 28 | $27.34 | $0.98 | 248k |
| @rnd | Opus 5.5 | 61 | $26.42 | $0.43 | 204k |
| @fabric | Sonnet 5.5 | 68 | $13.25 | $0.20 | 202k |
| @ui | Sonnet 5.5 | 86 | $11.28 | $0.13 | 87k |
| @runtime | Sonnet 5.5 | 26 | $3.08 | $0.12 | 88k |

Turns started by a message, by the card that read it:

| card | message turns | cost | per turn |
| --- | --- | --- | --- |
| @review | 39 | $30.27 | $0.78 |
| u-m-card | 26 | $22.15 | $0.85 |
| @fabric | 43 | $8.51 | $0.20 |
| @rnd | 23 | $7.78 | $0.34 |
| @ui | 36 | $2.41 | $0.07 |
| @runtime | 18 | $1.30 | $0.07 |

**Efficacy.** 254 work commits landed for $167, or **about $0.66 a work commit** on m1mini. Each work commit also
carried about 0.65 review commits. This leaves out sg4 (the orchestrator) and sg3, so it is a floor, not the whole
cost.

### 4.3 What it means

1. **Who a message wakes matters more than how long it is.** The same short message costs $0.07 when @ui reads it
   and $0.78 when @review does, because @review re-reads 336k tokens of context at each turn.
2. **@review is the biggest single cost, at a quarter of the total**, and almost three quarters of that is turns
   started by messages: one per commit or range sent to it, and one per follow-up.
3. **A long-lived worker is expensive.** u-m-card ran 28 turns at an average context of 248k, nearly $1 a turn on
   Sonnet. A worker should end when its item lands, or cycle its context.
4. **Keep-alive is cheap** ($0.67). Its design holds.

### 4.4 The levers, ranked by dollars saved in this window

| # | Lever | Saved in this window, estimated | Build |
| --- | --- | --- | --- |
| 1 | **@review cycles its context when idle or after a batch**, not after every verdict. A cold start re-reads 40k to 80k, and a cycle loses what one review teaches the next. So each cycle starts by reading the standing `docs/backlog/review/REVIEWER-NOTES.md` (started by @review, 86b95a63). At about 100k instead of 336k, a turn costs well under half | somewhat under $25 of $41, less the cold starts | none: one line in @review's queue, under `docs/rnd/lean-context-cycle-design.md` |
| 2 | **Batch what goes to @review.** One message per ready range, not one per commit plus a nudge. 39 wakes to about 15 | about $18, before lever 1 | none: a habit for every director |
| 3 | **Workers end or cycle at 150k context** | about $15 on u-m-card alone | the lean cycle's existing stage |
| 4 | **News is sent as `fyi`, which a receiver that holds its notices keeps instead of waking** | a few dollars here, more on sg4 | none: a habit, already in the `atrium_say` description |
| 5 | **The hub list replaces relays through the orchestrator** | sg4 side, about 4M context tokens a day (refactor 2.2), not measured here | stages L1 to L3 |

Levers 1 and 2 overlap, so together they save somewhat under $30, not $43. They need no build, so they are allowed under the
pause.

### 4.5 What the full re-evaluation still needs

sg4's transcripts and `session_usage` hold the orchestrator's cost, and m1mini cannot read them. Once the pause ends,
a short worker on sg4 should run the method of 4.1 over the same window and add:
- the orchestrator's turns by cause;
- the cost of each relay, start to finish: the director's say, the orchestrator's turn, and its rewrite for clint;
- sg3's rows, if any.

It publishes a table in this section's shape and exits. It is not launched now, because no worker may start during
the pause.

## 5. Stages

Each stage is useful on its own. Only L0 is allowed during the pause.

| stage | what | owner | size | acceptance |
| --- | --- | --- | --- | --- |
| L0 | **No build.** `notes/DECISIONS-OPEN.md` on sg4 is the list. Directors cite its numbers and add new rows there, through the orchestrator, instead of re-asking. Reports take the two-part form of section 3, with the record in `~/.atrium/records/`. @review cycles when idle or after a batch, reading its standing notes file, and directors batch what they send it | the orchestrator, every director | today | clint's next batch of answers cites numbers. Over one day, the "for clint" parts average under 400 characters. @review's message turns average under $0.30 |
| L1 | Hub rows with the fields of 2.1, stable numbers, the batch parser of 2.2, and delivery to the asking card. Imports `DECISIONS-OPEN.md` once, keeping its numbers | @fabric | 2 days | a batch `12 y, 14 n, 15 b` sets three rows and reaches three cards. An unknown number sends nothing and says why |
| L2 | The list on the board and the phone: "Now" and "Later", the one box, and the focus limits of 2.4 | @ui | 2 days | 14 open rows from 4 directors show at most 10 in "Now" and at most 3 per director. `#12 y` typed into a card asks "answer #12: yes?" and sends only on the confirm. `12 y` typed into a card goes to the card |
| L3 | A tool a director files a row with (`atrium_ask`), replacing a say to the orchestrator for a question | @runtime | 1 day | a director files a row, clint answers on the phone, and the director's next tool call shows the answer, with no orchestrator turn |
| L4 | Defaults on a deadline (2.3), and the daily bell batch and urgent limit (2.6) | @fabric, @ui | 2 days | a `default_ok` row with `effect: other` or no effect is refused at filing. A safe row defaults at its time, shows as defaulted, and is replaced by a later answer |
| L5 | The daily page (2.7) and idea capture (2.5) | @fabric, @ui | 2 days | the page renders on a phone with today's landings and spend matching `session_usage`. `/idea x` from a card's input becomes an idea row with a suggested owner after the confirm, and `idea: x` typed to a card reaches the card unparsed. `3 files are wrong` typed to a card answers nothing |
| L6 | Interview mode (2.9): the hub's interview rows, `atrium_interview` (start, ask, taken_as, done), the one-question card on the board and the phone with buttons, the default, free text, progress, back, the two small buttons and the answers file beside it, and decision-list rows turned into an interview | @fabric (rows), @runtime (the tool), @ui (the card) | 3 days | an interviewer card asks 3 questions through the tool. clint answers 2 on the phone and changes the first one on the board. The interviewer gets "Q1 changed" and rewrites Q3, and the screen marks it rewritten. The answers file on the hub holds his words and the "taken as" lines, and nothing is written into a repo |
| T1 | The sg4 re-evaluation of 4.5 | a short worker on sg4 | half a day | the table of 4.2 with the orchestrator's rows added, then the card exits |

L1 to L3 are the build of `rnd-new-clint-inbox`, and that backlog file points here.

## 6. Questions for clint, in plain words

1. **Where the list lives for now.** The open-decisions file the orchestrator keeps on sg4, outside the public repo,
   is the one list until the hub version is built. Directors refer to its numbers and add to it. **Suggested: yes.**
2. **Answers that take effect by themselves.** If you have not answered in two days, may a director's suggested
   answer take effect by itself? This would apply only when the answer changes atrium's own internal work, can be
   undone, and publishes, deletes, deploys, spends or contacts nothing. The item would say so from the start, and you
   can overrule it later. **Suggested: yes, two days.**
3. **One reminder a day.** One bell item a day with the open list and one daily summary page, plus at most one
   interruption per director a day, only for something that blocks running work? **Suggested: yes, at 8:00 your
   time.**
4. **Shorter reports.** Directors send you at most five lines (what changed, which numbers are yours, what is
   blocked), and keep the details in a file you can open? **Suggested: yes.**
5. **The rest of the cost check.** After the pause, a short helper on sg4 measures what the relays through the
   orchestrator cost, which this machine cannot see. **Suggested: yes, after the pause.**
6. **The interview screen.** When you ask to be interviewed on a design, the board and the phone show one question at
   a time, with the choices as buttons, the suggested one highlighted, a box for your own words, and your answers
   collecting beside it. You can go back and change one. **Suggested: yes.**
7. **Where interview answers are kept.** Your answers are quoted word for word, so they are kept on the hub machine,
   outside the public repo, wherever you decide the factory log lives. **Suggested: yes, the same place.**
