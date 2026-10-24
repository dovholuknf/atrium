A room's minute ssh sync no longer logs in to itself, logs in to every other room or scans its addresses. It checks
only what publishing and authorized_keys need, and the self test runs when the wizard runs. The wizard reads each
address's banner and host keys in Go, so it no longer needs `ssh-keyscan`. bug-ssh-selftest-every-minute.
