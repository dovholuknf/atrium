# CI as one script per platform: the interview

Source: INTERVIEW.md on hub branch claude/interview-ci-scripts (0c42d132). Ten questions, answered by the operator.
The overriding aim, in their words: it has to run locally exactly like CI, so a CI failure can be reproduced.

## Decisions

1. **Script shape (Q1).** bash everywhere: `bash scripts/ci.sh`, Git Bash on Windows. The land script calls it. The
   operator's complaint stands: the script has to explain why CI fails, not just fail.
2. **One script, all three platforms (Q2).** The workflow becomes a matrix job on the three major platforms (linux,
   macos, windows) that runs the same script. Local runs are identical to CI, which is the point. Landing is
   therefore strict: a red local run blocks it.
3. **Flaky tests fail (Q3).** Option A. No rerun, no quarantine list. A test that fails under load and passes alone is
   a bug to fix.
4. **macOS (Q4).** CI runs and, when it is red, we notice and fix it. Red is to be prevented where possible, but
   diagnosing and fixing is the method. Pull requests instead of pushing straight to main are named as possibly the
   better approach.
5. **Getting onto main (Q5).** Through a PR, so CI runs all platforms on it; then option B: push to `land/<ts>`, wait
   for the linux/macos/windows matrix on that sha, fast-forward main to it, branch protection requires the three
   checks.
6. **What ci.sh runs (Q6).** The recommendation, accepted: headless board tests (playwright) on all three platforms;
   the test-*.ps1 scripts on Windows; `-race` as a unit-only pass on linux and macos; screenshots written to
   build.claude/ only; ci.sh fails when git status is dirty after the run.
7. **Screenshots (Q7, Q8).** Dropped for now. No baselines, no hashes, no accept switch, no pixel gate. The earlier
   wish for a way to regenerate and "accept" screenshots is shelved with them.
8. **Toolchain (Q9).** Option A. The workflow keeps setup-go and setup-node. The script checks go equals go.mod and
   node major is 22 and stops on a mismatch, runs `npm ci` and the playwright install itself, and treats missing node
   as a failure.
9. **Failure report (Q10).** The recommendation, "let's see how the summary goes": ci.sh writes
   `build.claude/ci/summary.md` everywhere (header, steps with times, failed tests with their tail, slowest 10, a
   paste-able reproduce line using `--only <step>`); the workflow copies it to `$GITHUB_STEP_SUMMARY`; the land script
   prints each red job's summary; agents read it through atrium_ci.

## Open points

- **Alone/under-load label (Q10 vs Q3).** The recommended summary labels failures alone vs under load. Q3 forbids the
  rerun that would produce the label, so the label has nothing to say. Left out.
- **Land script.** Q2, Q5 and Q10 change the land script (run ci.sh before pushing, `land/<ts>` and the fast-forward,
  print red summaries). Not specified beyond that; out of scope for the CI script build.
- **PRs and branch protection (Q4, Q5).** Q4 floats PRs and Q5 says yes. Branch protection requiring three checks is a
  GitHub setting, not code.
- **Docker on Linux.** Q2's recommendation (also run `scripts/ci-linux.sh` in docker before landing) was answered with
  "run on all three platforms" and not with a yes or no to docker. ci-linux.sh is kept unchanged.
- **macOS being required (Q4).** Whether macOS is a required check from day one was not settled; the matrix includes it.
- **Public or private repo (Q4).** Asked, not answered. It decides runner minutes for macOS.
- **Toolchain mismatch on a laptop.** Q9 says stop on mismatch. Whether a developer may override is not said.
- **Node 22 vs the laptop.** The operator's laptop had node 18 (and this room has node 24), so the strict check fails
  there until they install 22.
- **Which test-*.ps1 on Windows (Q6).** "The test-*.ps1 scripts" was not narrowed. test-groups.ps1 is a group runner
  for the Go suite, not a test, and is left out.
- **-race on Windows.** Not asked. Needs cgo, left out as before.
