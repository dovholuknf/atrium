# Spike: the paste preamble names the file type

Backlog item `s-paste-preamble-by-type`. Exploration only, no code changed.

## The problem

Dragging a `.md` onto a terminal types `check out the image here: <path> `. The wording is one constant,
`DefaultPastePreamble` (`internal/api/files.go:76`), used for every file. For a markdown file, a PDF, a zip or a log
the sentence is wrong, and the agent is told it is looking at a picture.

## Why it matters

- The preamble is the only instruction the agent gets with the path. A wrong noun can send it looking for pixels in a
  text file, or make it describe a "screenshot" that is a spec.
- It is typed into the line and not submitted, so the operator sees the wrong sentence and has to edit it by hand each
  time.
- The default was written for the screenshot case (clipboard paste), and drop and picker reuse it unchanged.

## Every place the preamble is chosen

There is exactly one place that prepends it, plus one that omits it.

| Path | Where | Preamble |
| --- | --- | --- |
| Clipboard paste of a file | `pasteFilesIntoTerm` (`js/terminal.js:1985`) builds `File`s named `pasted.<subtype>` and calls `uploadIntoTerm` | yes |
| Drop on the terminal | `wireTerminalDrops` (`js/terminal.js:2078`) calls `uploadIntoTerm(files)` | yes |
| Paste box with files | `js/terminal.js:1962` calls `uploadIntoTerm` | yes |
| Desktop picker | same `uploadIntoTerm` | yes |
| Phone with composer up | `uploadIntoTerm` hands off at `tcomposeAttach` (`js/terminal.js:2024`), `m/js/compose.js` uploads and inserts bare paths via `tcomposeInsert` | no preamble at all |

The one choice site is `uploadIntoTerm`, `js/terminal.js:2054`: `preambleOf(await pasteSettings())`, then
`sendInput(pre + paths + " ", ...)`. `preambleOf` (`js/notes-files.js:2044`) maps the stored setting to text: unset gives
the daemon default (`paste_preamble_default`, `internal/api/settings.go:132`), the sentinel `none` gives empty, anything
else is used as written. The settings box is `s-pastepre` (`notes-files.js:1036`, saved at `:2075`).

The server does not choose. `uploadFiles` (`files.go:166`) returns `{"paths": [...]}` only, and the daemon never
prepends text. The preamble is a browser-side concern, the same for every file in a batch, applied once to the joined
paths.

## Is the file type known at the choice point?

Yes, in the browser, before the upload.

- Each `File` has `name` (extension) and `type` (browser MIME, derived from the extension for drops and exact for
  clipboard blobs). `uploadIntoTerm` already holds the `File` list when it builds the sentence.
- Clipboard pastes are named `pasted.<mime subtype>`, so `pasted.png` and the MIME agree. Odd subtypes
  (`pasted.svg+xml`) are possible, and a MIME of `""` happens for unknown extensions on a drop.
- Server side, the type is knowable too (`filepath.Ext(name)`, or `http.DetectContentType` on the saved bytes), but the
  response carries only paths and is used verbatim by the phone composer, so the browser is the cheaper place.
- A sniff is not needed. Extension first, MIME second, `other` as the fallback. A mislabeled file gets a slightly wrong
  noun, which is no worse than today.
- A batch can mix kinds. Needs a rule (below).

## Proposed wording per kind

Same shape as the default (instruction ending in colon and space, path follows).

| Kind | Rule | Wording |
| --- | --- | --- |
| image | `image/*` or png jpg jpeg gif webp bmp svg | `check out the image here: ` (today's text, unchanged) |
| markdown / text | `text/*`, md txt log json yaml yml csv toml and similar | `read the file here: ` |
| pdf | `application/pdf` or `.pdf` | `read the pdf here: ` |
| archive | zip tar gz tgz 7z rar | `unpack and look inside the archive here: ` |
| other | everything else | `take a look at the file here: ` |

Mixed batch: if every file is the same kind use that kind's wording. Otherwise use the plural neutral wording
`take a look at these files: `. Multiple files of one kind could use the plural (`images here: `), which is optional
polish and not needed for the first cut.

## How the custom preamble setting fits

Today the setting is one string. Options:

1. Custom string wins for every kind (keep the meaning exactly as it is today). Unset gives per-kind defaults, `none`
   gives nothing. This is the smallest change and breaks nobody. Downside: an operator who customised for screenshots
   (`look at this screenshot: `) gets that sentence on a PDF.
2. Custom string per kind. Five fields in settings. More UI and more storage for a thing few people touch.
3. Custom string with a `{kind}` token, for example `check out the {kind} here: `, filled with `image`, `file`,
   `pdf`, `archive`. One field, and the default itself becomes `check out the {kind} here: `. Existing custom strings
   have no token and behave as in option 1.

Mechanics for any option: `preambleOf` keeps its name and behaviour. The kind logic goes in a new function the caller
uses to pick the default (or substitute the token). `paste_preamble_default` would either stay as the image sentence
and a new `paste_preamble_defaults` map is added beside it, or the table lives in JS only. Keep the stored sentinel
`none` semantics unchanged. The settings export list (`internal/daemon/export.go:147`) needs no change.

## Recommendation

Do option 1, with the table living in the browser next to `preambleOf`:

- Add `pasteKindOf(files)` and a small kind to wording map in `js/notes-files.js` beside `preambleOf`. In
  `uploadIntoTerm` use the custom string when one is set, else the wording for the kind of the batch.
- Leave `DefaultPastePreamble` and `paste_preamble_default` alone (the image sentence). Show the per-kind defaults as
  placeholder or help text in the settings box so the operator knows an empty box is not "check out the image".
- Fix the phone composer separately: it inserts bare paths with no preamble, so that gap predates this and could use the
  same helper in `m/js/compose.js`.
- Tests: a JS check or a small node test for the kind function (extension and MIME cases, empty MIME, mixed batch),
  because nothing on the Go side changes.

If clint wants one setting that stays useful for a custom wording across kinds, upgrade to option 3 (`{kind}` token)
later. It is backwards compatible with option 1.

Size: one new function, one table, a two line change in `uploadIntoTerm`, one settings hint. No daemon or schema change.
