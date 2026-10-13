# comms-audit: what r-agent-comms-one-protocol already shipped

Audited on claude/comms-audit (at hub/claude/main c6578820). SHAs are the first commit that added the symbol named,
from `git log --reverse -S`. Nothing was changed but this file.

## The four points of r-agent-comms-one-protocol

| Point | Verdict | SHAs | What is missing, and where it would change |
|-------|---------|------|--------------------------------------------|
| 1. "done <sha>", and it does not close a card the launcher wants kept | present | 1f9f03ed atrium_done and launch text; 4c267edc artifact and hub-wide sha check; e23ca803 KeepOpenTag / InvestigationTag (staysOpen, exitonreport.go) | The keep-open rule is a launcher-side tag, not something the worker is told. That is acceptable: atrium, not the worker, decides. |
| 2. "blocked, need X", to the launcher first and never to clint unless the launcher is clint | partial | 1f9f03ed atrium_blocked, 50-word limit (CheckBlockedReason in finish.go) | atrium_blocked reaches the launcher by construction, but no text says "launcher first, never clint". Add it to LaunchEndingLine (internal/daemon/launch.go), link.reportLine (internal/link/control_mcp.go) and leanSystemPrompt (internal/daemon/lean.go). |
| 3. A launcher's "stop" is a valid end, and atrium does not nudge or notify after it | partial | 1b5a2f56 PromptFYI: a launcher's fyi creates no debt (promptKind in fyi.go); d55e1c87 expectedEnd: an exit the launcher asked for sends no ended notice (store/ledger.go) | Only a "stop" sent with kind=fyi is exempt, and only the atrium_say description says so. A "stop" sent as the default kind=needs still makes the worker owe a report, so silentStop (a2a.go) tells the launcher it stopped silently. Change store.promptOwes, or add the sentence to the launch text. |
| 4. A worker never asks where to push | absent | none | No launch text, system prompt or tool description tells a worker how its work gets back. Nothing in launch.go mentions pushing. The text goes in the same three places as point 2. |

## The three folded-in items

| Item | Verdict | SHAs | What is missing, and where it would change |
|------|---------|------|--------------------------------------------|
| r-report-is-one-line | partial | 1f9f03ed atrium_blocked reason is 1 to 50 words, refused not cut; 4c267edc sha or artifact only, and a say repeating an end is dropped (endonce.go) | Only the two ending tools are enforced. atrium_report `summary` and atrium_say text have no one-line limit (checkPeerText bounds size, not lines), so a worker can still send a paragraph. Add a limit in reportHandler (internal/link/control_mcp.go) or daemon.validReport (finish.go). |
| r-expected-end-notices | done | d55e1c87 no ended notice for an end atrium caused or a worker already reported; 4c267edc endSayDropped; 1b5a2f56 fyi owes nothing; 1bcf830e exit-on-report | None found. An exit asked for, atrium_done and atrium_blocked (ReportedSince check in silentStop) all end quietly. The one hole is the plain-needs "stop" under point 3. |
| r-worker-knows-commit-is-visible | partial | 98cf7eab atrium_git_push (daemon.GitPush); 4c267edc atrium_done accepts a commit in the worker's own directory or on any hub branch | atrium_done already works without a push, but nothing tells the worker so. GitPush (internal/daemon/hubremote.go) has no "nothing to push" answer when the remote is another room's forwarder on the same machine, and gitsync.PushToHub refuses instead. No text tells a worker not to offer a push to origin. |

## Smallest next change

Add one short paragraph to the launch ending, in the three places that carry it: LaunchEndingLine (launch.go),
link.reportLine (control_mcp.go, a test keeps the two equal) and leanSystemPrompt (lean.go). It would say: report to
your launcher, never to the operator, unless your launcher is the operator; your commit is read from your worktree,
so never ask where to push; a launcher's "stop" is a valid end, answer it with atrium_done or nothing. That is text
only, and it closes point 4, finishes point 2, and covers the instruction half of point 3 and of
r-worker-knows-commit-is-visible. Points 3 (plain-needs stop) and r-report-is-one-line still need code after it.
