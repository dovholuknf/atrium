# Lane collapse

The board folded a project group by name across the whole board, so shutting `alpha` in one column shut it in every
column (`img/lane-collapse-before.png`). It is now folded per lane and group, with "collapse all" and "expand all" in
the board's header strip (`img/lane-collapse-after.png`). The stack's "pinned" and "the rest" sections could not fold
and now do, with the same chevron and header as the other groups (`img/stack-collapse-before.png`,
`img/stack-collapse-after.png`).

State is in `atrium.folded`, as every fold is. Board groups are `proj:<lane>|<name>` and the pinned group is
`pin:<lane>`, where the lane is the column id, or the status inside the finished column. The stack's halves are
`sect:pinned:<scope>` and `sect:rest:<scope>`, where scope is the group's name or `all`. An old board-wide key
(`proj:<name>`, `proj:pinned`) is spread over every lane the first time the list is read, so nothing you folded opens.

## Test plan

- Board, grouped by tag with two lanes holding the same group: fold it in one lane. The other lane's copy stays open.
  Do the same with the pinned group.
- "collapse all" shuts every group in every lane, "expand all" opens them, and both survive a reload.
- Stack with pinned and unpinned cards: fold "pinned", then "the rest". Each folds alone and both stay folded after a
  reload. The board's folds are untouched.
- Put `["proj:alpha","proj:pinned"]` in `atrium.folded` and reload: both are shut in every lane and open lane by lane.
- `HEADLESS_UNITS=laneFold,foldStill,bootClean node scripts/test-board-headless.js`
