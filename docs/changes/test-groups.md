# test-groups

The integration suite was one 11 to 25 minute `go test -tags integration ./...` and one panic aborted a package.
It now runs in groups.

- **Script:** `scripts/test-groups.ps1` (PowerShell 7, Windows first, pwsh on Linux and macOS).
  `-List` shows each group, its packages, `-run` pattern and last time. `-Group a,b` runs groups. `-All` runs them all
  and prints a table. `-Failed` re-runs the tests that failed last time. `-Check` fails when a test is in no group or
  in two. Each group writes `build.claude/test-groups/<group>.txt` and prints, for each failing test, its name,
  `file:line` and first error line.
- **Groups:** 24, none over 2m25s. link is split in six, daemon in seven, gitsync in three, and the small packages
  are one group. Splitting is by test file inside the script, so no test file changed and there are no new build tags.
- **Why file patterns:** the test names are sentences (`TestACardWithAPid...`), so a name prefix cannot tell them
  apart. The script reads which tests a file defines and builds the `-run` regex.
- **Docs:** `docs/testing.md` has the groups, their times, how to run one test, and the known noise.
- **Recorded run:** `-All` on a Windows dev machine, 3 groups failed (4 tests), the rest passed. The failures are
  timing under load, see `REPORT.md`.
