TestARunnerAndAShellOnOneCardHaveTheirOwnRuns no longer leaks its runner on a loaded Windows runner: it ends the lone runner with a plain process kill, not the taskkill tree walk (q-ci-reds-14).
