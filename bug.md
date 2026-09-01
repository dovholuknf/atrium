# atrium permission UI bugs

Captured 2026-09-01 from a live session against the permissions UI on localhost:7778. Source was four
screenshots of the WAITING ON YOU view and the OS-level notification. Two pending Bash requests were on
screen, asked 19:53:48 and 20:07:24.

repo: dovholuknf/atrium

## Desktop notification has no approve or deny action

experienced: the browser notification reads "permission needed / Bash: sed -n '23,62p' internal/state/state.go /
localhost:7778" and carries a single Close button. Acting on the request means leaving the notification and going
back to the tab.

expected: the notification offers the decision inline, at minimum approve and deny, so a request can be answered
without switching context. Close alone should dismiss without deciding.

## Two text boxes render per request card

experienced: each pending request draws a textarea holding the command and a second text box labelled "stop
asking about", one above the other, plus the rule chip below. Both are editable, and it is not obvious which one
the approve buttons act on.

expected: one text box. The command is not something the user edits, so it should be static text, leaving "stop
asking about" as the only editable field.

## Growl toasts never auto-dismiss

experienced: "permission needed" toasts stack in the bottom right and stay there. Two were still visible after
their requests had been on screen for over thirteen minutes. Each has to be closed by hand with its x.

expected: a toast auto-dismisses after a few seconds, and a toast whose request has already been decided or
superseded disappears on its own rather than waiting for a manual close.

## Help tooltip is positioned outside the container and clips its own text

experienced: the `?` tooltip in the RULES header opens anchored so its right edge runs past the container. Lines
are cut mid-sentence, for example "Standing answers. When a request match" and "prefix - plain text, matched
against the ST". A horizontal scrollbar appears at the bottom of the page while the tooltip is open.

expected: the tooltip is positioned inside the viewport, wraps its text so no line is cut, and does not change the
page's scroll width. Opening it should never introduce a horizontal scrollbar.
