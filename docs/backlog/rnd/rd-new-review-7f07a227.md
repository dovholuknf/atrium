# Review: OpenCode token routing design (7f07a227, m1mini, 2026-10-02): HOLD

`docs/rnd/opencode-token-routing.md`, clint's "token min using opencode". Reviewed alone (the held factory-log and
refactor commits are out of scope here). Checked against opencode.ai/docs/plugins, fetched today.

The position is right and well argued. Cheap models go only where a Claude step checks, judgment and security stay
on Claude, a graded bake-off comes first, and the terms question goes to clint before any unattended run. The use of
the Kimi grading is fair: 0 of 27 at the stated severity, 6 at real defects, 1 confirmed and already documented.
It holds on one security gap in K3, which repeats the r-pr-run HIGH.

## High: the opencode second opinion would run the PR's own plugins

The plugin docs (today): "`.opencode/plugins/`: project-level plugins. … Files in these directories are automatically
loaded at startup", beside `~/.config/opencode/plugins/`. Section 3 item 2 and K3 run `opencode run` on the bundle.
If that runs in the PR's checkout, or in any directory under it, a PR that ships `.opencode/plugins/x.js` runs its
code as the daemon's user. A plugin is code, not a setting, and `edit: deny, bash: deny` does not stop it. This is
exactly the class closed for claude in r-pr-run (forks in `<run>/work`, no project setting source).

Fix in the design:
- the second opinion runs with cwd `<run>/work`, never under `src/`, and reads the bundle as data;
- no project config or plugins from the PR: no `opencode.json` and no `.opencode/` found in or above the cwd;
- a K3 acceptance proof, like r-pr-run's: a `src/.opencode/plugins/marker.js` that touches a file, and the marker
  must not appear.

Also say whether opencode reads `opencode.json` from parent directories, and if it does, keep the run folder outside
any repo.

## The two checks you asked for

**Section 4, `permission.asked`: true as listed, but they are events, not hooks.** The docs list `permission.asked`
and `permission.replied` under "Permission Events", next to `session.idle` and the rest. A plugin observes them
through its `event` handler. They are not a hook that returns a decision the way `tool.execute.before` takes its
output. So "a newer opencode may have closed that gap" holds only if a plugin can answer the ask, through the server
SDK's permission-reply call (the session permission endpoint), which is **unverified**. Reword it: "the docs list
permission events. Answering one needs the SDK's reply call, which K2 checks against the installed binary." Also
worth K2's check: the docs name the plugin directories `plugins/`, plural, while the atrium plugin's header says to
install into `~/.config/opencode/plugin/`. Confirm the singular is still loaded, or fix the install line.

**Section 3, the cost arithmetic: right, with two omissions.**
- **Second opinion:** 60k at $0.95/Mtok is $0.057, about $0.06. Right.
- **Verifier per item:** a 60k cache read at Sonnet's $0.20/Mtok is $0.012, plus 2k output at $10/Mtok is $0.02,
  so $0.032, about $0.03. Twenty items are $0.64. Right.
- **Omitted, once per task:** writing the 60k cache in the first place is 60k at the 1h write rate of $4/Mtok, so
  $0.24. A 20-item verify is then about $0.88, not $0.65.
- **Omitted, per item:** each fork's own uncached prompt (the item and the instruction, 1 to 3k at $2/Mtok, under
  $0.01), which is negligible.
- **Unclear:** "K2.7 Code's $60 of Zen value is about 15 million cached-read-heavy input tokens plus output". $60
  buys 63M fresh input, or 315M cached reads, or 15M output. Show the mix the 15M assumes, or drop the figure. The
  monthly headroom point stands without it.

## Smaller

- The audit's citations were "accurate" in section 0. Say "mostly accurate": one location was wrong (C9 named
  hub_setting, and it is the room's table) and one route verb was wrong (H9's PUT is a POST).
- Section 4's permission config: say whether opencode's `edit` permission takes path patterns. "edit limited to the
  worktree" depends on it. Mark it **unverified** until K2 checks.
- Q2, public repos only: right. Add that a PR bundle from an outside contributor is public too, but a private
  repo's PR is not.

## Verdict

HOLD on the K3 High: the second opinion outside the checkout, no project plugins or config, and a marker proof in
K3's acceptance. Fold in the `permission.asked` rewording and the plugin-directory check, the cache-write line in
section 3, and "mostly accurate". A re-read covers sections 3, 4 and 5. doc-ok on OK. It can land alone by
cherry-pick.

Quality: careful and honest about what is measured and what is not. It is grounded in the one graded run, and the
terms question is the right blocker to raise. The miss is that opencode's plugins load from the project, the same
trust boundary the r-pr-run review found for claude's settings.
