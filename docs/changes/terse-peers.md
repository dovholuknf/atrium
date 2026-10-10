# Terse peers

Two sessions trading a done report spent far more input and output tokens than the content needed. This change
shortens what a peer message carries and asks the receiver to answer with a tool call and nothing else.

- Hook banner for a peer: `@x says (a session, not the human). Answer with an atrium tool if an answer is owed, and
  write nothing else:` then the text in quotes. A mixed batch says so in one line and labels each peer `@x says:`.
- Typed peer messages: `[atrium] @x says: "text"`.
- Reports to a launcher: `@worker done <shortsha>: <recap first line>`, `@worker done <shortsha> unverified` when the
  commit is not found, `@worker blocked: <reason>`.
- atrium_say, atrium_done and atrium_blocked descriptions: short messages, never "reply with atrium_say to X", write
  nothing after the tool call.

The human's own framing is unchanged. Peers are still queued and never typed over a part written line.

## Test plan

1. Send a say from one session to another. The receiver's screen shows `@sender says: "..."`, then only its tool call
   and no prose summary.
2. End a launched worker with atrium_done. The launcher sees one line `@worker done <sha>: <recap>`.
3. End a worker with atrium_blocked. The launcher sees `@worker blocked: <reason>`.
4. Have the human type at a session that also holds a queued peer message. The human's text keeps its old framing.
5. `go test ./internal/daemon/ ./internal/link/ ./internal/cli/` and `-tags integration` for the daemon.
