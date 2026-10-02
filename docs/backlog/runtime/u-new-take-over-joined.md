# u-new-take-over-joined: take over a joined session

Status: not started, 2026-10-02. Raised by the board (u-joined-click).

A session joined from the operator's own terminal is live but atrium holds no terminal for it, so the board cannot attach, exit or kill it. The board's click dialog on a joined row shows "take it over" grey for this.

**Needed.** One room call that ends the joined session where it runs (by its pid, if the join recorded one, or by asking its hooks to stop) and, once it has exited, resumes the same conversation under atrium on the same card, so it becomes attachable. It must refuse while the session is still running: resuming a live conversation runs it twice (the resume-busy guard already refuses that).

**Why.** The operator who clicks a joined row wants to work on it from the board; today the only way is to quit it in its terminal and resume by hand.

**Board half.** Enable the button in `joinedClick` (`js/terminal-list.js`) and call the new route.
