# r-new-opencode-bubbles: chat bubbles for an opencode card, the same as claude gets

Status: HELD (pause). Owned by @runtime (reader) with @ui (only if the phone view needs a change). Filed by the
orchestrator 2026-10-01, from clint: "needs the same bubble treatment that Claude gets. Possible?"

Screenshot: `D:\git\github\dovholuknf\atrium\.atrium\incoming\20261001-181352-Screenshot_20261001_181339_Brave.jpg`
on sg4. The phone card view (`/m/alias/opencode`) shows an opencode card as its raw terminal screen.

## Why it happens

`repliesPage` in `internal/daemon/replies.go` reads a transcript only when `d.usage.isClaude(t.Runner)`. Every other
runner falls back to `screenReply`, `source: "screen"`, which is the screen rows as one reply.

## What exists to read

opencode keeps every session on disk, and `opencode export <session id>` prints it as JSON: `messages[]`, each with
`info.role`, `info.time.created`, `info.finish`, and `parts[]` where `type == "text"` holds the text. The card's
`resume_id` is the opencode session id (the plugin records it at session start, for example
`ses_f06a7011dffeuZx11lJZVjRBnv`). Verified by hand on sg4 on 2026-10-01.

## Wanted

- An opencode card's replies and prompts come from its session, `source: "transcript"`, so the phone and the board
  draw bubbles exactly as for claude. Assistant text only, no tool calls or reasoning, the same rules as the claude
  reader (bounded, each reply cut at `replyTextMax`, paging with `before`).
- Read opencode's storage directly if its layout is stable enough, otherwise run `opencode export` bounded in time
  and output. Say which and why. A failure falls back to the screen, as today.
- A test with a recorded export.
- The reader is per runner, so codex and gemini can follow the same shape later.

## @runtime director, 2026-10-04

Landed already under the id without `new-` (r-exit-guard ca014c1e, r-opencode-bubbles 82a63fd3). Nothing left.
