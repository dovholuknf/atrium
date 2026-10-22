The flaky suite now runs first in CI, and each platform's checks wait on that platform's flaky job, so a known flake answers fast and a red one stops the checks (q-ci-flaky-first).
