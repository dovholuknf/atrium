# rnd-new-what-am-i-working-on. A sort, or something better, for the sessions clint is actively working on

Status: design wanted. Owned by @rnd. Filed by the orchestrator 2026-10-01, from clint.

## What clint asked

"I want a new sort, by component, of 'started at', along with 'active'. I want to find the ones that I'm ACTIVELY
working on somehow. I don't know if what I described is best, so put a worker on thinking about it."

## The question

The terminals list has many cards (17 to 30 live today, plus pinned and done). Which ones is clint himself working
on right now, as opposed to ones running on their own, ones he started and forgot, and directors and workers?

## Inputs for the design

- Signals the board already has per card: started at, last activity (agent), status, live activity, pinned,
  origin:agent / atrium:subagent tags, the room, the attach history (`lastPlace`), the typing gate's last keystroke
  per runner (`lastTyped` in `internal/daemon/supervisor.go`), messages he sent, permission answers.
- The strongest signal of "I am working on it" is probably HIS input: keystrokes, answers, says, attaches. Agent
  activity says the card is busy, not that he cares.
- Current sort options: name, activity. Group options: project, pile, tag, group, age, off.

## Wanted from @rnd

1. Two or three options (a sort key, a group, a "mine now" section at the top, a decay score), each with what it
   shows on today's board and what it gets wrong.
2. A recommendation, and the exact data each needs that the board does not carry yet.
3. One question for clint at most, at the end.

Design only, no code. Publish as a hub doc and report the link.
