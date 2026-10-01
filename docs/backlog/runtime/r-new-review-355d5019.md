# Review of 355d5019 (@runtime: /replies carries the card's prompts)

Reviewed by @review, 2026-09-30, from `git diff claude/main...355d5019` (`internal/daemon/replies.go`). Room side.
`go vet ./internal/daemon/` passes, and `go test -run 'Repl|Prompt|Output' ./internal/daemon/` passes. The shapes
were checked against this session's own transcript with jq (read only).

## What holds

- **Nothing new is exposed.** A prompt is a user line in the card's own transcript: what the operator or a peer
  said to it. The same words are already on the card's terminal, which the same board attaches to on the same
  listener, and they are bounded like replies (last n, 16 KB each).
- **Injected context stays out.** In the live transcript, no user turn carries more than one text block. So a
  `<system-reminder>`, CLAUDE.md or hook output is not joined onto the operator's words, because none is stored
  inside the user message. Task notifications carry `origin.kind: task-notification` and are skipped by the origin
  rule, and anything else starting `<` is dropped. A tool result ends the line, and sidechains and `isMeta` are
  skipped.
- **The kinds read correctly.** `[atrium] ... says:` and the `[atrium] new context:` wake are `peer`, and `/clear`
  comes out as the command `clear`. The rest is the operator.
- **Caching is unchanged.** Prompts ride the replies cache on size and mtime, and output_at's incremental read
  ignores them.

## Findings

### Low

1. **Several text blocks would be joined without a check on each block.** The `<` test looks only at the start of
   the joined text. If a future Claude Code stores an injected block after the operator's text in the same user
   message, it is shown as the operator's words. Today's transcripts do not have that shape. Could `promptOf` drop
   any text block that starts with `<`, rather than testing only the joined text?

### Nit

- A prompt the operator really typed that starts with `<`, such as a pasted HTML line, is dropped, and one that
  starts with `[atrium] ` is marked `peer`. Both are rare.

ROOM DEPLOY OK 355d5019

## Re-read of 7aa8dfe4, 2026-09-30

The low is closed. Each text block that starts with `<` is dropped on its own, before the blocks are joined, and
the test carries an injected `<system-reminder>` block.

ROOM DEPLOY OK 7aa8dfe4
