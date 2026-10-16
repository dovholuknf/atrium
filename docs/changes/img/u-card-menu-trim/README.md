# u-card-menu-trim: the board card menu, before and after

Before is claude/main's menu code, after is this branch. Both were taken by the restartMenu unit's cards.

| card | before | after |
| --- | --- | --- |
| idle | `before-idle.png` | `after-idle.png` |
| parked | `before-park.png` | `after-park.png` |
| paused | `before-paused.png` | `after-paused.png` |
| idle, after "more..." is clicked | no such row on main | `after-more.png` |

Labels are unique in every menu: on a parked or paused card the conversation picker reads "resume another conversation",
next to the plain "resume". The restartMenu unit fails on a repeated label.

The terminal and row menus (termMenu in js/terminal-list.js) keep their "new context" label. Only the board card menu
reads "handoff / clear context".

`resumeSpinner` fails its first try when run in the shards and passes on retry, on claude/main as well as here.
