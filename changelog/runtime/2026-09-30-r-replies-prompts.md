- **A card's replies say what was said to it.** `GET /v1/tasks/{id}/replies` adds `prompts`, the last n user turns
  from the transcript, oldest first, each `{at, text, truncated, kind}`. `kind` is `operator` (typed at the terminal,
  the desktop board or the phone), `peer` (atrium's `[atrium] ` messages) or `command` (a slash command). Tool results,
  notifications, meta lines and subagent prompts are left out. `replies` is unchanged. For /m, which showed only what
  was sent from that phone. Room side, needs a room deploy. (with @ui)
