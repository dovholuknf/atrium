# r-hooks-all-in-atrium: every hook is an atrium hook

The goal is that no session runs a PowerShell or bash hook script. Each one becomes an `atrium hook --event ...` call. This is done in five phases, and phase 1, the guard, is the one built here.

## Phase 1: the guard

`atrium hook --event pre-tool-use` replaces `~/.claude/hooks/pre-tool-use-hook.ps1`. The code is in `internal/guard`, and the wiring is in `internal/cli/hook_guard.go`.

- The rules are data, kept in `internal/guard/rules.json` and embedded in the binary. They are the old hook's rules in the old order. Each rule has an id, a check, the tools it applies to, a decision (deny or allow), a reason, and params. The first rule that matches answers.
- Bash commands are parsed with mvdan.cc/sh. PowerShell commands go through a tokenizer. Both produce the same model: commands, their words, and how they are chained. The model also tracks the directory each command runs in (`cd`, `Set-Location`, `pushd`, `git -C`), unwraps wrappers (`env`, `timeout`, `xargs`), and parses inner scripts (`bash -c`, `pwsh -Command` and `-EncodedCommand`, `cmd /c`, `eval`, `iex`, heredocs).
- The hub rule takes the room's agent address from the location file the daemon writes, and each remote URL must start with `<agent>/git/`.
- The guard is OFF by default. `ATRIUM_GUARD=on` (or 1, true, yes) turns it on, and anything else leaves it off. `settings.json`, the dotfiles and the board's hook installer were not changed. Switching a machine over is clint's call.
- `ATRIUM_GUARD_RULES=<path>` replaces the built-in rules with a file.

Every test case in the dotfiles `test-git-guard.ps1` is ported in `internal/guard/gitguard_test.go`. For the misfires, the tests check the right answer, not the old one.

### Failure posture

Other atrium hooks fail open. The guard is a deny hook, so it fails closed wherever it can:

- **Built-in rules.** They are compiled in, and `Load` is strict: an unknown field, an unknown check, a bad regexp, a missing reason or tools list, or a duplicate id fails the whole set. A test loads the built-in set, so a build with broken rules does not pass. If one ships anyway, every Bash, PowerShell, Write, Edit, NotebookEdit, Task and Agent call is denied with a message that says to set `ATRIUM_GUARD=off`.
- **Broken override file.** If the file is missing, does not parse, or fails a check, the same tools are denied, and the reason names the file and the error. The exception is a Write or Edit of that file itself, so the file can still be fixed. Read, Grep and Glob still work.
- **Panic.** If a check panics, the call is denied and the reason names the rule. The CLI also recovers any panic and denies.
- **Git fails.** A failed or timed-out git call (3 s each) means the guard cannot tell. Branch verbs and hub operations are denied in that case.
- **Fails open.** These are outside atrium's control. If the hook passes Claude Code's own timeout, Claude Code runs the tool anyway. If the payload is empty or does not parse, the guard prints nothing, because there is no tool call it can judge.
- **Kill switch.** `ATRIUM_GUARD=off`, or leaving it unset, turns the guard off. It does not depend on `ATRIUM_PERM_GATE`, so turning off the permission gate does not turn off the guard, and turning off the guard does not turn off the gate.

The guard never exits non-zero. It always reports through the PreToolUse JSON on stdout.

### Misfires fixed

- The cwd rules followed the session cwd. They now use the directory the command itself changes to (`cd` and `git -C` are followed).
- `2>&1` was read as a branch name.
- `git -C` was refused. It is now treated as a directory change.
- `;` inside a quoted argument (such as a sed expression) was refused. Now only real statement separators count.
- `>` inside quotes, `2>&1`, `>&2` and `> /dev/null` were refused as redirects. Now only real redirects to a file count.
- A phrase was blocked even inside a search. Phrases are now ignored in the arguments of grep, rg, Select-String, findstr, git grep and git log.
- Only the first git command of a chain was checked. Now every git command in the chain is checked.

### Changes from the old behavior

- The Bash tool's `;` and `> file` rules are KEPT for real separators and real file redirects. The brief's "refuses > redirection" was read as meaning the misfires, not the tee rule itself. If the tee rule should go too, it is one entry in rules.json.
- In the Bash tool, an unquoted backslash path is read the way bash reads it: `cd C:\x` becomes `C:x`. The ported Bash cases use forward slashes for this reason.
- `gh api` must be `-X GET` wherever it appears in a command, not only at the start. Docker env prefixes are checked in any position.
- A phrase is checked in words and heredoc bodies, not anywhere in the raw text. Comments are no longer checked.
- The current-branch verbs are the same as before: commit, add, rebase, reset, restore and clean. merge, cherry-pick and revert are not gated. Adding them is suggested.

### Known limits

The guard reads command text, so it cannot see:

- `Start-Process` and other ways of starting a program it does not parse
- a binary that has been renamed or copied, such as `g.exe` standing in for git
- a script file run by path, whose contents are never read

The old hook could not see these either.

## What stays personal

The rules split into two groups.

**Atrium's rules** stay compiled in: the git rules (alias, global options, unknown subcommands, the hub remote, plain checkout, branch names, current-branch), the co-author phrase, and only-atrium-subagents. They protect atrium's own model of rooms and branches.

**Personal rules** are one operator's taste, not atrium's policy: no-find, no-perl, no-python, the cmake preset and vcpkg rules, the em-dash and `!important` content rules, `gh api` GET-only, tee instead of `>`, and compound-cd. These belong in a personal rules file that dotfiles provide through `ATRIUM_GUARD_RULES`.

Today the override replaces the built-in set rather than adding to it. So for now the personal file has to carry atrium's rules too, copied from rules.json. A layered mode (built-in rules, then personal rules) is the natural next step, once the split is agreed.

## Phases 2 to 5

2. **Ledger.** `log-subagent.ps1` and `set-session-state.ps1` become atrium hook events. Atrium already sees subagent-start, subagent-end and the session state, so the scripts' log is one more consumer of events atrium already handles.
3. **Tab title.** `set-tab-title.ps1` becomes an event that sets the terminal title from the card's name and state, which atrium knows better than the script does.
4. **Context lines.** `filler-guard.ps1`, `snapshot-layout.ps1` and `clint rw` become events that add context to a prompt (UserPromptSubmit and SessionStart additionalContext), with their text kept as data like the guard's rules.
5. **Bootstrap.** `session-bootstrap.ps1` becomes the SessionStart event. When this phase is done, `settings.json` holds only atrium commands, and the installer can write all of them.
