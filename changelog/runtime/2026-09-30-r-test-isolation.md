- **A daemon tells its runners where it is, and a test can no longer reach the live room.** A daemon that keeps its
  own address file (a throwaway room, a test) now passes it to every runner and shell as `ATRIUM_LOCATION`. Before,
  their hooks read the machine's shared file and reported to whichever room owned it, which is how a test's runner put
  cards named `002` and `atrium-reopen-*` on the live board. Test daemons also stop every runner they started, and the
  shell tests run with a sealed home and no daemon address. Room side for the location hand-down.
- **A constraint never halts a room.** SQLite refusing a write on a constraint (a foreign key naming a card that is not
  there, a duplicate, a CHECK) is returned to the caller on every table, told apart by its code. Storage that failed
  is still a halt. (r-new-review-f4466ea0)
