The daemon's tests no longer fail on Windows at cleanup with `atrium.db` still open. Every test helper that makes a
daemon now cancels its Run and waits for it to return, closes the store, and waits until the database files can be
deleted before the temp dir is removed. Item t-daemon-tests-hang (CI on main, windows).
