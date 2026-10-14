# Held chip wears the envelope

`termHeldChip` (`js/terminal-list.js`) drew `!` or `! N` for a message held behind a turn, a line or a dialog. It now draws
the envelope and the count, same glyph as the quiet queued chip, and keeps the `chip held` class so it keeps its colour.
The tooltip is unchanged. `!` stays for needs-you marks.

Images: `img/held-chip-envelope/{before,after}-terms-row-{1280,390}.png`, shot by `scripts/shoot-held-chip-envelope.js`.

## Test plan

- Headless `sayWhen`: the held chip text is `✉︎` and `✉︎ 2`, class `chip held`.
- By hand: hold a peer message behind a typed line. The chip is an envelope, not `!`.
