Daemon tests no longer fail when a slow disk makes the database close take longer than 30 s: their databases skip fsync (q-ci-restartrunner-shutdown).
