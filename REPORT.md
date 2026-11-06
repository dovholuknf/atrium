# paste-preamble-kind

The kind-aware preamble was already on main: commit 8662b5664 "Paste preamble names the file kind" is an ancestor of
claude/main (3022c5116). Cherry-picking a9e5dc10a onto it is empty, so I aborted it. The only gap was wiring.

## What changed
- `scripts/test-buckets.json`: `scripts/test-paste-kind.js` added to the docs-files bucket `tests` (owner of notes-files.js).

## Already on main
- `internal/api/web/js/notes-files.js:2067` PASTE_WORDING, `pasteKindOfFile`, `pasteKindOf`, `preambleForFiles`.
- `internal/api/web/js/terminal.js:2060` uses `preambleForFiles`.
- Wording: image "check out the image here: ", text (md, txt, log, json, yaml, csv, toml) "read the file here: ",
  pdf "read the pdf here: ", archive "unpack and look inside the archive here: ", other "take a look at the file here: ",
  mixed batch "take a look at these files: ".
- A custom `paste_preamble` wins for every kind, and "none" gives no preamble (the test pins both).
- Test: `node scripts/test-paste-kind.js` passes.

## Checks
`bash scripts/test-touched.sh` all green (selftest, check-test-buckets). No chromium suites were skipped.

## How clint verifies
With `paste_preamble` unset, drag onto a terminal on the board, and expect typed:
- .png: `check out the image here: <path> `
- .md: `read the file here: <path> `
- .pdf: `read the pdf here: <path> `
