## What changed

The held messages notice on a terminal (`js/heldline.js`) now has a hover menu, "Let it through". It is the operator
overriding the gate for once. The board reads `GET /v1/tasks/{id}/typing` (now with `exact`), copies the line, and
`POST /v1/tasks/{id}/let-through {line}` makes the room do, under the input lock: control-c to clear, every waiting
message typed and submitted oldest first, then the draft typed back with ESC CR for newlines and no Enter.

- Exact only. The line must be what the board copied and the typing model must be sure of it, else 409 and nothing is
  written. The board then only copies and toasts "your text is on the clipboard".
- Mid-turn is allowed (Claude Code queues the message). A dialog, a new-context cycle or a deploy hold refuses.
- Keys pressed during it wait on the input lock and reach the terminal after the draft. The model is the draft then
  those keys.
- Room side: `internal/daemon/letthrough.go`. A room deploy is needed.

## Test plan

## @LETTER@. Let it through

### @LETTER@1. Line, messages, restore

1. In a supervised claude terminal, type `fix the` then shift-enter then `thing`, without Enter.
2. From another session, `atrium tell` this one two messages. The notice shows "2 messages waiting".
3. Hover the notice and choose "Let it through".

**Expected:** both messages are delivered, the draft is back on the prompt on two lines and not sent, and the
clipboard holds the draft.

### @LETTER@2. A line atrium cannot read

1. Type a few characters, then press the left arrow so atrium is unsure. Queue a message and choose "Let it through".

**Expected:** a toast says "your text is on the clipboard". The line and the message are untouched.
