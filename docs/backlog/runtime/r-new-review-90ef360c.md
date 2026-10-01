# Review of r-paste-strip 90ef360c (@runtime: strip paste markers before bracketing)

Reviewed by @review, 2026-10-01, from `git diff claude/main...90ef360c` (one commit). This is the daemon-side defense
in depth for finding 1 of `docs/backlog/ui/u-new-review-7e496aa8.md`.

## What holds

- **One helper, every site.** `bracketedPaste` now wraps `SayPasted`, `typeLabelledGuarded`, `tellByTyping` and
  `deferPeerInjection`. `git grep` for the raw markers in non-test code finds only the helper's constants and
  `supervisor.go`'s `pasteStart` and `pasteEnd`, which detect an operator's paste and wrap nothing.
- **The strip is repeated until it is stable**, so a marker spliced together from the pieces around a removed one
  (`ESC[20` + marker + `1~`) is removed too. A lone trailing ESC is dropped, so it cannot merge with the closing
  marker.
- **Each path has a test** that feeds both markers, `ESC[Z` and a trailing ESC. gofmt and `go vet` are clean.
  `go test -run 'Paste|Bracket|Say|Tell|Inject|Message' ./internal/daemon/`: ok.

## Findings

### Low

1. **A runner without bracketed paste still gets control bytes raw.** When `bracketedPasteFor` is false, the text
   is typed as is. There is no paste to break out of, but an `ESC[Z` or a `^C` in a message is still a keystroke. The
   one caller that sends untrusted text, the /m quote, now sends visible forms (7ab1eacb). So this is a note for any
   future caller: if one sends file or model text to a non-paste runner, it needs the same `visible()` treatment, or
   the plain path should map C0 controls but tab and CR, as the paste path now removes markers.

Quality: after the Sonnet switch. It is minimal, every site goes through one helper, and the splice case was thought
of unprompted. No drop.

ROOM DEPLOY OK and HUB DEPLOY OK 90ef360c~1..90ef360c.
