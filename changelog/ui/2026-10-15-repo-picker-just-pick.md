- The launch dialog's repo list is one list. Room checkouts and hub repos are merged by host/org/repo, each row is the
  repo name only, and the "on the hub" badge and the "clone from hub" button are gone.
- Picking a row fills the working directory. A repo the room lacks is cloned from the hub as part of the pick. A repo
  the hub lacks, typed in the filter as host/org/repo or a URL, is cloned onto the hub from the forge first and then
  onto the room. A clone failure or a forge refusal shows inline in the dialog (repo-picker-just-pick).
