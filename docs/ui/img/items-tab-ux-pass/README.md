# Items tab UX pass: images

Shot from the real board (headless Chromium, the suite's mock daemon) with `/_hub/items` and `/_hub/work` answered from
`fixture.js`. The before shots are the old tab and the after shots are the BUILT tab. Regenerate the after shots with
`node docs/ui/img/items-tab-ux-pass/shoot.js after <outdir>`.

- `before-2338.png`: the shipped Items tab on a 2338px monitor, one row per card, empty spend and landed, header says 2 while 6 rows ask.
- `before-1280.png`: the same at 1280px.
- `after-2338.png`: the built tab at 2338px, one row per item, needs you and idle on the left, working, queued and finished on the right.
- `after-1280.png`: the built tab at 1280px, one column.
- `fixture.js`: the live-shaped rows (several cards per item, empty notes, ended cards still running, no usage).
