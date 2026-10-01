# Review of 1036e180 (@ui: /m composer upload) and de951162 (@ui: needs-you keeps oldest wait first)

Reviewed by @review, 2026-09-30, from `git show 1036e180` and `git diff ad9794ab de951162`, read only. Hub side.
Board and headless checks are @ui's, and I ran none. 872b8e7c (sticking to the newest message) is not in this
checkout, so it is not reviewed here.

## What holds

- **What the upload sends.** One multipart POST per batch, holding only `file` parts, to `/v1/tasks/{id}/files`,
  which computes its own destination and takes no path (daemon rule 8). The only extra header is `X-Atrium-Room`,
  read from the board's own room choice. XHR is used only for progress, and an error answer's text is shown as the
  chip's word through `textContent`.
- **Where the returned paths go.** Into the textarea as plain text (`place`), or appended to the message text for a
  send that waited on them, skipping a path the text already holds. They are never put into HTML. The /m thread
  draws the sent text through `U.esc`.
- **A send during uploads.** It waits on each held file's `batch` promise. A held file whose chip was taken off is
  `gone` and is left out, and a failed one fails the send with its name. On failure the text, the settled chips and
  the paths of files that did land all come back, so the 869cfa7f nit is closed.
- **The terminal's compact bar** still waits for its files before it sends, as before. The pickers are drawn only
  on the phone page.
- **de951162.** The needs list skips `sortRows` and stays oldest wait first, with a visible note saying so. The
  ad9794ab low is closed.

## Findings

### Nit

1. The `accept` list names office types (`.doc` and the rest) that the runner cannot read, and they are uploaded
   anyway. That is harmless, since the upload is bounded by the endpoint, but a docx path in a prompt helps no agent.

HUB DEPLOY OK 1036e180 de951162

## 872b8e7c, 2026-09-30

Read `git show 872b8e7c`. Following stops only on a scroll within 1.5 s of a wheel, touch, pointer or key event on
the thread, and reaching the end always sticks again. A ResizeObserver on the thread and its parts, and a captured
`load` (images), go to the end while following. There is no markup and no data path, so nothing to escape.

HUB DEPLOY OK 872b8e7c

## Re-read of 1b32263e + d3a16dd3 (@ui)

`git diff 1b32263e~1 d3a16dd3 -- internal/api/web`, read only. 1b32263e drops `.doc .docx .xls .xlsx .ppt .pptx` from
the composer's accept list, which closes the nit above. d3a16dd3 raises `REPLIES_N` from 3 to 10. The room clamps
`n` to `repliesMax` (10, `internal/daemon/replies.go`), so the request asks for nothing the room does not already
bound. No findings.

HUB DEPLOY OK 1b32263e d3a16dd3
