Docs: docs/runtime/card-home-design.md sections 7 and 10.2

## Test plan

## @LETTER@. Homes on the board

### @LETTER@1. The card shows its home

1. Launch a card with `home` set, open its terminal and its detail dialog.
2. Wait for the room's next size walk (ten minutes at most).

**Expected:** the path with a copy button and a size chip. The age shows only once the measurement is over an hour old.
On the loopback board with one room an open-folder button shows beside the path.

### @LETTER@2. The room tile and the Disk panel

1. Open the Rooms page, then room settings, then Disk.
2. Set `card_root` to a drive with under 10 percent or 20 GB free.

**Expected:** the tile shows the homes' total and the drive's free space, orange only in the second case. The panel lists
homes by size, folders that are not atrium's, and cards working outside the root.

### @LETTER@3. Removing a home

1. Keep a home by culling a card with an uncommitted file, then press remove in the Disk panel and confirm.
2. Try the same on a card whose runner is still up.

**Expected:** the first removes the folder and says how much it freed. The second has no button, and a request for it is
refused.

### @LETTER@4. New home from the launch form

1. Open new card, tick new home, give a repo and branch, launch.

**Expected:** the directory box is disabled, the card starts in a home with `work/` a worktree on that branch. Ticking
throwaway unticks new home.
