# Review: u-switch-latency closeout 5982b166, and the hover no-row low 5eea3847

Two commits, both on landing `ce6022fd`:

- `5982b166` on claude/u-switch-closeout changes the latency item and `scripts/measure-term-switch.js`.
- `5eea3847` on claude/ui-director adds `docs/backlog/ui/u-new-measure-hover-no-row.md`.

Verdict: **OK.**

- **The sg4 pool result.** Attach 9, made with 8 cards kept on one remote room, shows c>o 30 -> 33 ms. That supports
  "no per-room cap". The first-byte rise is put down to a 0 KB replay, which is plausible, since a 0 KB replay sends
  no first byte to time.
- **"Two frames" is a software-GL artifact.** The argument follows the renderer: with the DOM renderer two frames
  take ~25 ms, and with SwiftShader WebGL they take 60-240 ms, in the same headless build. That is enough to stop
  without a GPU trace. clint's report is paraphrased, not quoted.
- **The script.** The no-baseline line now gives the right reason in both cases. `node --check` passes.
- **The hover no-row low.** It is filed as unexplained, with three candidate causes and the two ways it can resolve.
  That is the right size for a Low.

A nit: the hover item says "instant for him". Use "clint says switching is instant", as the latency item does.

Atrium-Verdict: room-ok ce6022fd..5982b166
Atrium-Verdict: hub-ok ce6022fd..5982b166
Atrium-Verdict: doc-ok ce6022fd..5eea3847
