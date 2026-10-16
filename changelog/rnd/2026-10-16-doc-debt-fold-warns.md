- `scripts/fold-changes.ps1` now warns, and never fails, for a `docs/changes/*.md` file with no `Docs: d-<n>` or
  `Docs: none, <reason>` line above `## Test plan`. The warning names the file, and the exit code and the folded output
  are unchanged. `scripts/test-fold-changes.ps1` covers it. m-doc-debt-from-features, phase 2.
