internal/link/launchcaps.go: the launch caps refuse two room keys that differ only in case, and the PUT refuses unknown
fields, so a misspelt key is a 400 and saves nothing. Item f-027.

internal/link/ctlaudit.go: the control audit line strips control characters from the caller text and cuts it to 200
characters, so a newline cannot forge a second line and a huge value cannot write a huge row. Item f-028.

internal/link/notify.go: a needs-input card that asked through the Notification hook (waiting_reason asked, no turn end)
is notified as `input` instead of being dropped as a card that never finished a turn. Item r-new-notify-input-no-turn-end.
