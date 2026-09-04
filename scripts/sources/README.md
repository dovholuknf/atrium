# Sources

A source is a command atrium runs on a timer. Its stdout is a JSON array of intake items, and atrium turns each
one into a card with no runner, waiting in the inbox until you press start.

**Atrium holds an argv and an interval. It never holds a credential.** `gh` has a token in the keyring it
already uses, `mcp-gateway` has four backends' worth of secrets, `zrok` has an environment. There is nowhere in
a source row to put one, and that is the design rather than an omission. See `docs/intake-design.md`.

## The shape

```json
{
  "source":        "github",
  "external_id":   "openziti/ziti#4211",
  "url":           "https://github.com/openziti/ziti/issues/4211",
  "title":         "tunneler drops DNS on resume",
  "why":           "assigned to me",
  "tags":          ["issue", "openziti/ziti"],
  "suggested_cwd": "D:/worktrees/github/openziti/ziti/issue-4211",
  "prompt":        "investigate 4211 ...",
  "runner":        "claude"
}
```

`source` and `external_id` are required and everything else is optional. Together those two are the
deduplication key, which is why neither may be empty: the same number is an issue in one tracker and a ticket in
another, and a source with no key raises the same work again on every tick.

`source` is lowercased and both are trimmed, so two scripts spelling it `github` and `GitHub` raise one card.

Print one object or an array of them. An empty stdout means nothing to report, which is a normal answer and not
a failure.

## Rules atrium applies

- **One megabyte of output.** A source with more than that to say is reporting a repository, not a work queue.
- **Two minutes.** A source is a network call wearing a shell script, and the thing network calls do is hang.
- **Three consecutive failures switches it off,** with the reason still on the row. Turning it back on is you
  saying you fixed it, and clears the count.
- **A whole run fails or none of it lands.** One unkeyable item in a batch of forty is a source to fix, not a
  partial import to reconcile. The same batch arrives again next tick once it is fixed.
- **Thirty seconds is the floor on an interval.** A source is a child process, and one every second is a fork
  bomb with a settings screen.

A source runs as the daemon's user with the daemon's environment. It is exactly as trusted as a harness, which
is to say completely, and the settings screen says so.

## Choosing an interval

There is no good default, and fifteen minutes is a starting point rather than an answer. A review request is
stale in an hour. A CI failure is worth knowing about in five minutes. A support queue somebody is paid to watch
does not want a second watcher at all, and pointing a source at one is how a board becomes a worse version of a
tool that already works.

## What is in here

| Script | What it reports |
| --- | --- |
| `github-assigned.ps1` | Open issues assigned to you, through `gh`. |
| `zendesk-open.ps1` | Open support tickets, by identifier only. Read the header before using it. |

## Writing your own

The cheapest useful one is a `TODO` scan, and it is worth writing first purely as proof that a source can be a
shell one-liner:

```powershell
rg -n "TODO\(me\)" --json | ... | ConvertTo-Json -AsArray
```

The highest value one is CI failures. A failed workflow run on a branch you own has a cause, a log, a repo and a
directory that probably already exists, which is the most complete intake item there is. `gh run list --json` is
the whole source.
