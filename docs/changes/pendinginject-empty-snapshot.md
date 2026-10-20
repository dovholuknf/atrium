# The held-message retry and an empty snapshot

`pendingInjector.try` snapshots the held entries, drops peer text while a card is in a new-context cycle, and reads the
hold again a few lines later. When the cycle ended between the two reads the snapshot was empty, nothing was typed, and
the still-blocked note indexed `entries[0]` of that empty snapshot. It now uses `ht.entries[0]`, read under the lock.

## Test plan

## @LETTER@. The still-blocked note

### @LETTER@1. The hold lifts mid-attempt

1. Run `go test -tags integration -run TestStillBlockedNoteUsesTheEntryHeldUnderTheLock ./internal/daemon`.

**Expected:** it passes. With `entries[0]` back in the still-blocked note it panics with index out of range.
