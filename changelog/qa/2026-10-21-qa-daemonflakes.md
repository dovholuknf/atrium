Two daemon integration tests now wait on conditions under one deadline instead of fixed sleeps, so a loaded machine no longer fails them (qa-daemonflakes).
