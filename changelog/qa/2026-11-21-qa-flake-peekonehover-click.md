# peekOneHover: the expand click waits for the popover's real state

macOS CI board-2 failed `peekOneHover` at `p.click(".peek.on .qa-exp")` with a 5000ms timeout, and the retry failed too.

The button is `hidden` until `qaClamp` measures the question, and the usage reply redraws the popover body and measures
again. The test now waits for the body to leave `loading` and the button to show before it clicks. If it never shows,
the failure prints the measured state instead of a bare click timeout. No timeout was widened.

The cause was not reproduced locally, so this is a guard on the one state the click needs, not a proven fix.
