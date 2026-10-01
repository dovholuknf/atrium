# r-deploy-ready: the hub says when a deploy is ready, and one click does it

Step one of "the hub deploys itself" (docs/rnd/factory-shape.md), built to @review's five required changes. Nothing
deploys automatically and no timer deploys.

- `internal/deployready` reads git and answers `ready`, `blocked`, `current` or `unknown`. A verdict is the trailer
  `Atrium-Verdict: hub-ok|room-ok|hold <base>..<tip>` (or one sha) on a review commit. It covers commits by
  `git patch-id --stable`, so a rebased landing keeps its verdict and an altered patch loses it. A range is
  `--first-parent`. The newest trailer wins, and a conditional OK is an OK.
- Code needing a verdict: `internal/**`, `cmd/**`, `scripts/**`, `go.mod`, `go.sum`. Not `*.md`, images, or
  `scripts/test-board-headless.js`. Room-side Go (anything outside `internal/link` and `internal/hubstore`) also needs
  `room-ok`. A merge counts only for its own remerge-diff. A verdict on a commit that touches non-review files is
  ignored and noted.
- `GET /_hub/deploy-ready`: the report, with `line` for the button, `blocking` (sha, short, subject, why, detail),
  `notes`, `script`, and `deploy` (idle, running, finished with exit). `?fresh=1` skips the 10 second reuse.
- `POST /_hub/deploy-ready/deploy {"tip": "<full sha>"}`: loopback only. 409 when not ready, when the tip moved, or
  when one is running. 202 starts `scripts/live/deploy-ready.ps1` detached.
- SSE: a `deploy-ready` event, a delta, on every board. Re-fetch the GET. Sent when the answer changes (checked on the
  hub's minute tick, only while a board watches) and when a deploy starts or ends.
- Settings: `deploy_binary` (default: the hub's own executable) and `deploy_script`.
- Not built here: the green package gate on the tip, the 15 minute floor between deploys, the deploy hold check, the
  hosts-setting check, and the automatic step two.
