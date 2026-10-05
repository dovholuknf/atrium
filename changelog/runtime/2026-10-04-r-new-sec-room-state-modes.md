# Room state is owner-only

The room state directory is now 0700, and an existing one is tightened at start. atrium.db, -wal and -shm are 0600. The cold event sink, the ATRIUM_TAP_DIR tap and the PR runner files (run.log, review.json, run folders) are 0600 and 0700. This matches the hub.
