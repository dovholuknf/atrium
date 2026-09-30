# u-new-machine-health-gear: a machine health section in the gear, with "profile this machine"

Status: parked (clint, 2026-09-30). @ui owns the board half, @runtime the room half. Designed by @rnd:
`docs/rnd/machine-health-design.md`.

## Why

On 2026-09-30 clint found three Windows problems by looking at a process monitor himself: Defender at 103% CPU
scanning builds, headless chrome drawing WebGL on the CPU through SwiftShader, and test binaries in `%TEMP%`.
Each had a fix nobody would have found from the board. clint wants atrium to notice this kind of thing and offer
"do you want me to run an agent to profile it", from the gear.

## Wanted

- **A "machine" section in the gear**, per room: the advisories that room has raised, each with what was seen,
  the numbers, the fix as a ready-made command with the room user's own paths, and a link to the doc. Dismiss per
  room. The first advisory is r-new-defender-advice. Others follow the same shape (SwiftShader in headless runs,
  an indexer on the worktree root, a disk nearly full, a reserved port range).
- **"Profile this machine" button.** Launches a read-only profiling agent card on that room with a fixed brief:
  sample the top CPU, memory and disk processes for N minutes, attribute each to a card or a tool where it can
  (process tree back to a runner's pid), and write a report with fixes it proposes and never applies. The same
  button sits on an advisory, with the brief narrowed to that advisory.
- **Nothing runs elevated or changes a setting.** Advisories and the profiler only read and recommend. Applying a
  fix is a human action, as in `docs/user-guide.md` Pattern 13.

## Related

- r-new-defender-advice (runtime): the first detector.
- f-new-defender-at-provision (fabric): applying the known fix at room bring-up.
- u-queue item from 2026-09-30: the headless suite skips xterm's WebGL addon.
