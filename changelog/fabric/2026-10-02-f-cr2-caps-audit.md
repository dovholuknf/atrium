internal/link/launchcaps.go: the launch caps refuse two room keys that differ only in case, and the PUT refuses unknown
fields, so a misspelt key is a 400 and saves nothing. Item f-027.

internal/link/ctlaudit.go: the control audit line strips control characters from the caller text and cuts it to 200
characters, so a newline cannot forge a second line and a huge value cannot write a huge row. Item f-028. The agent and room taken from the headers get the same strip and bound, and the strip also removes the
line and paragraph separators and the bidi controls.

internal/link/launchcaps.go: a stored launch_caps value that fails its check, such as one saved with two room keys that
differ only in case, is logged once, because every room then gets the default cap. Room keys are folded the way `For`
folds them.
