# Review of 8df971e4 (@fabric: the live hub starts with ATRIUM_HOSTS from the User environment)

Reviewed by @review, 2026-09-30, from `git show 8df971e4` (`scripts/live/live-common.ps1` `Start-Hub`). Read only.

## What holds

- The value is read from the User environment, set for the hub after `Clear-SessionEnv`, and removed when the User
  value is unset. So the caller's session no longer decides which names the board answers.
- It is logged twice: in the deploy log (`Say`) and on the `hub.err` start marker, so the next outage of this kind
  shows in both places.

## Findings

### Low

1. **`Start-Room` still takes the caller's value, and the order of the calls now decides it.** `Start-Hub` sets
   `$env:ATRIUM_HOSTS` in the deploy script's own process. A room started after it inherits the User value, and one
   started before it inherits the caller's. The room's board is reached through the hub's link, which the edge does
   not wrap, so this does not break the share today. Should `Start-Room` take the same value, so a room's loopback
   listener answers the same names on every deploy?
2. **A Machine-scope value is ignored.** On a machine where ATRIUM_HOSTS is set only at Machine scope, the hub now
   comes up with none. sg4 has it at User scope, so this is only a note until r-new-hosts-setting replaces it.

PASS 8df971e4

BUILT on claude/f-new-review-8df971e4: both lows fixed (Get-LiveHosts, User then Machine, used by Start-Hub and Start-Room).
