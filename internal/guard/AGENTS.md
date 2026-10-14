# internal/guard

The PreToolUse guard: which commands, edits and subagents a session may run. It replaced a PowerShell hook script in
dotfiles. It runs as `atrium hook --event pre-tool-use`, wired in `internal/cli/hook_guard.go`. The design and the
failure posture are `docs/changes/r-hooks-all-in-atrium.md`. Read that before changing what fails which way.

## The one thing that is different here

**Every other atrium hook fails open. The guard fails CLOSED.** It is a deny hook, and a guard that waves a command
through because it broke on it is not a guard. So a broken guard denies:

- `Load` is strict. An unknown field, an unknown check, a bad regexp, a missing reason or tools list, or a duplicate
  id fails the WHOLE set. If the built-in set fails, every Bash, PowerShell, Write, Edit, NotebookEdit, Task and
  Agent call is denied with a reason that says to set `ATRIUM_GUARD=off`.
- A broken `ATRIUM_GUARD_RULES` file denies the same tools, except a Write or Edit of that file itself, so it can
  still be fixed. Read, Grep and Glob stay usable.
- A check that panics denies the call and names the rule. `guardHook` also recovers any panic and denies.

A typo in `rules.json` therefore stops every session on every machine that has the guard on. A test loads the
built-in set, so that build does not pass. Run it.

What fails open is decided by Claude Code, not here: a hook that passes Claude Code's own timeout, and a payload that
is empty or does not parse. The guard never exits non-zero. It always answers through the PreToolUse JSON on stdout.

## The reply check is the opposite again

`reply-pattern` rules name the tool `reply` and run from the Stop hook (`atrium turn`, `internal/cli/turn_reply.go`)
against `last_assistant_message`. **That path FAILS OPEN.** A Stop that blocks keeps a session working, so a guard
that failed closed there is a session that never stops. A rule set that does not load, a panic and a payload with no
message all end the turn as before, with one line on stderr. It blocks at most once a turn (`stop_hook_active` lets
the answer go), and it corrects rather than prevents, since the reply is written before the Stop fires. A rule naming
`reply` and a tool call in one set fails the load, and `reply` rules never answer a tool call.

## Invariants

1. **OFF unless `ATRIUM_GUARD` says on.** Only `on`, `1`, `true` or `yes` turn it on. Switching a machine over is the
   operator's call, made in `settings.json`, never something an upgrade does. It is independent of
   `ATRIUM_PERM_GATE`.
2. **The rules are data, the checks are code.** A rule in `rules.json` names a check from the `checks` table in
   `checks.go`, and carries its tools, its params, its decision and the sentence the model reads. A new check is Go
   code AND a table entry. A rule naming a check that is not in the table fails the load.
3. **Order matters. The first rule that matches answers.** `decision` is `deny` (the default) or `allow`. An allow
   answers without the permission prompt and stops the rules after it.
4. **`ATRIUM_GUARD_RULES` REPLACES the built-in set, it does not add to it.** A personal rules file has to carry
   atrium's own rules too, copied from `rules.json`.
5. **A rule sees parsed commands, never raw text.** Bash goes through `mvdan.cc/sh`, the PowerShell tool through the
   tokenizer in `pwsh.go`, and both produce the same `Script` model. A word inside quotes is an argument, `2>&1` is
   a redirection and never a branch name, and `;` inside a sed expression is part of an argument. A bash script that
   does not parse is read by the PowerShell tokenizer instead, so a typo is still checked rather than waved through.
6. **The directory is the command's, not the session's.** `cd`, `Set-Location`, `pushd` and `git -C` are followed.
   An empty `Cmd.Dir` means the guard cannot tell.
7. **Every git command in a chain is checked**, with aliases resolved in the directory it runs in. The old hook
   checked only the first.
8. **Git asks are bounded.** Each git call has a three second timeout (`gitTimeout`). Per the doc, a git call that
   fails means the guard cannot tell, and branch verbs and hub operations are denied.
9. **The hub remote comes from the room.** `Env.Agent()` is this room's agent address, read from the location file
   the daemon writes (`recordedAgentAddr`). A hub remote URL must start with `<agent>/git/`.
10. **Everything the guard asks of the machine goes through `Env`**, so a test can answer it. Do not call `os` or
    `exec` from a check.

## Known limits

The guard reads command text. It cannot see `Start-Process` or other ways of starting a program it does not parse, a
renamed copy of a binary, or the contents of a script file run by path.

## Files

| File | What it holds |
| --- | --- |
| `guard.go` | `Rule`, `Load` (strict), `Builtin`, `Evaluate` (first match wins, a panic denies) |
| `rules.json` | the built-in rules, embedded. Edit it and run the tests |
| `checks.go` | the `checks` table and every check a rule may name |
| `git.go` | the git policy: every git command read once into a `gitCall`, aliases resolved, the branch in a dir |
| `shell.go` | the shared model (`Script`, `Cmd`, `Word`, `Redir`), wrappers, inner scripts, the directory tracker |
| `bash.go` | the Bash tool's commands through `mvdan.cc/sh`, falling back to the tokenizer |
| `pwsh.go` | the PowerShell tool's tokenizer. Not a parser: enough to tell commands from arguments |
| `env.go` | `Env` and `OSEnv`, the machine behind an interface |
| `*_integration_test.go` | behind the `integration` build tag. Real git, kept off the operator's git config by `internal/testguard` |

## Tests

`go test ./internal/guard/` loads the built-in rules and runs the cases. The git cases in
`gitguard_integration_test.go` run real git and need `-tags integration`.
