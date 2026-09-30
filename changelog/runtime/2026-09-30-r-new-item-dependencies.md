- **Work items can wait on other work, and the hub keeps the list.** `atrium_deps add` records that an item waits on
  another item, on `live:<item>`, `room:<name>`, `sha:<commit>`, or on free text. The hub meets a gate only by
  reading for itself: an item has landed when claude/main holds its `changelog/<dept>/<date>-<id>.md`. A gate met
  stays met. A loop is refused at write time, naming it. There is no clear for an agent: a human clears a gate on
  the board, with a reason, and the clear is audited. (r-new-item-dependencies)
- **`atrium_launch` refuses a worker whose title starts with a blocked item**, naming what it waits on. There is no
  override. `atrium_deps ready <dept>` lists the department's backlog items that have not landed and have no open
  gate. The agent that added a gate is told once, by a say from `atrium-hub`, when its item's last gate clears.
  Hub side only, so it goes live with a hub deploy and needs no room restart. (r-new-item-dependencies)
