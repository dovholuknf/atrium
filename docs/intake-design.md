# Intake: starting a card from something that is not a directory

Ryan asked for atrium to integrate with source control and with ticketing, so that a card can be started from a
GitHub issue, a pull request review request, a Zendesk ticket or a Discourse topic, straight from that system's
API or UI. The repo owner's note was that this is "sort of similar to what I currently do with the gwt command".

That note is the whole design. `gwt` already does this, it has done it for a while, and the way it does it is more
interesting than the fact that it does it. This document reads that script, states what a card already carries,
and proposes the work in layers from cheapest to most ambitious. It also says which version of this would be a
mistake, because the obvious implementation makes atrium hold credentials for four external systems, and that
crosses a line the repo has drawn twice already.

Nothing here is built. Nothing here is a promise.

## What gwt already does

`D:/git/github/dovholuknf/dotfiles/powershell/onpath/git-worktree.ps1` is a 6229 line PowerShell script with one
large `switch` over a verb. Seven of its verbs are intake by any reasonable definition: each takes an identifier
from an external system and produces a git worktree with a claude session already running in it.

| Verb | Identifier | Directory | Branch |
| --- | --- | --- | --- |
| `issue <num>` | GitHub issue number or URL | `issue-<num>` | `issue-<num>` |
| `pr <num\|url>` | GitHub or Bitbucket PR | `pr-<n>` | the PR's head branch |
| `advisory <GHSA>` | GitHub security advisory | `advisory-<ghsa>` | `advisory-<ghsa>` |
| `ghsa <url>` | a temporary private fork holding the fix | `advisory-<ghsa>` | the fork's fix branch |
| `backport <pr> -Lts` | a merged PR plus a release line | the branch name | `backport.<vX.Y.x>.<head>` |
| `discourse <url\|id>` | Discourse topic id | `discourse-<topicid>` | `discourse-<topicid>` |
| `zendesk <url\|id>` | Zendesk ticket id | `zendesk-<ticketid>` | `zendesk-<ticketid>` |

A bare URL pasted as the first argument is routed to the right verb before the switch runs, at lines 1390 to 1450.
Pasting a GitHub issue URL, a Bitbucket pull request URL, a GHSA page, a Zendesk ticket or a Discourse thread all
work with no verb typed. That routing table is the closest thing anyone has to a specification of "the identifiers
this human actually starts work from", and it was written from use rather than from a survey.

### The part worth copying

The three integrations differ in how much they know about the system they name, and the differences are
deliberate.

**GitHub is read directly, through `gh`.** Seven call sites, all shelling out to the CLI, none touching
`api.github.com` itself:

```powershell
1882:  $j = & gh issue view $issueNum --repo "$($ctx.Org)/$($ctx.Repo)" --json title 2>$null | ConvertFrom-Json
1935:  $raw = (& gh api "repos/$($ctx.Org)/$($ctx.Repo)/security-advisories/$ghsa" 2>$null | Out-String).Trim()
```

The comment above the first one says `# Best-effort fetch the issue title from gh, for nicer prompts.` The title
is used for the seed prompt and for nothing else. The branch and the directory are named from the number, so a
failed fetch costs a nicer prompt and never costs a worktree.

**Zendesk is not read at all.** `gwt zendesk` makes zero network calls. It resolves a host, builds a URL, and puts
that URL into the prompt handed to the agent:

```powershell
2425:  $zendeskPrompt = "read this zendesk ticket ($ticketUrl), summarize it here and in ZENDESK-$ticketId.md
       file. list any attachments on the ticket and tell me whether there are any, then ask me which to
       download ... then let's figure out how you and i can make a plan to answer the customer."
```

Reading the ticket is the agent's job, through the Zendesk MCP tools it already has. The script's only
Zendesk-specific knowledge is a URL regex and `$env:ZENDESK_HOST`.

**Discourse is almost not read.** One `Invoke-WebRequest -Method Head` at line 2307, to follow a slug redirect to
a numeric topic id, and then the same pattern: build the URL, put it in the prompt, let the agent read the thread.

So the script already answers the credentials question, and it answers it three different ways depending on what
is cheap. Where a credentialed CLI is already installed and already logged in, use it and treat the result as
optional. Where it is not, hand the identifier to the agent and let the agent's own tools do the reading.

### The rest of the layout, since a proposal has to fit it

```
$GIT_ROOT\<host>\<org>\<repo>                    the main clone
$WORKTREE_ROOT\<host>\<org>\<repo>\<branch>      one worktree per branch
$WORKTREE_ROOT\sessions\<guid>.json              the session ledger, one file per session
$WORKTREE_ROOT\watch\state.log                   every hook transition, appended
```

The ledger entry is written by `claude-shell.ps1` before the tab spawns, and carries `Id`, `Pid`, `WorktreePath`,
`Branch`, `Repo`, `WindowName`, `PromptText`, `ClaudeSessionName`, and later `ClaudeSessionId`, `State`,
`LastStateChange`, `Saved`, `Label`, `RecapPath`. Mode B reads this. The daemon deliberately does not, and
`docs/architecture-v2.md` records why under "Abandoned".

**gwt has no atrium integration at all.** Zero occurrences of the word in the script, the registry, or the state
docs. The two systems track the same claude sessions through separate mechanisms that never meet: gwt through the
ledger it writes at spawn time, atrium through the claude hooks in `dotfiles/claude/settings.json`. That is the
gap this document is really about.

## What a card already carries

`internal/store/store.go`. A card has `Title`, `Why`, `Repo`, `Worktree`, `Runner`, `Hostname`, `PID`,
`Status`, `WireName`, `Overrides`, `Rank`, `ExternalID`, `ResumeID`, `Branch`, `WindowName`, `Gated`,
`AutoApprove`, `Tags`, `Pinned`, `Theme`, and since this was written `Note`, `Icon`, `WaitingReason`,
`Priority` and `PriorityAt`. Read the struct rather than this list, which is what a list of fields in prose
always becomes.

Four of those matter here.

- **`Why`** is free text, set by `SetWhy`, described in the code as "the answer to what was I even doing". An
  issue title and body is exactly what it is for.
- **`Tags`** are free text, lowercased and deduplicated by `NormalizeTags`. The comment at `store.go:100` already
  names the case this feature is about: "A card is also a support case, a tangent, a pull request, a lab, and none
  of that is in the path." A `zendesk` tag and a `12345` tag would group and filter with no new mechanism.
- **`Worktree`** is the directory. Everything downstream keys off it, including the folder permission rules.
- **`ExternalID`** was a column surviving from the abandoned ledger adoption, written by nothing, which is what
  made it free to take. Intake writes it now, and `atrium launch --external` sets it by hand.

`Launch` in `internal/daemon/launch.go` takes `{harness, cwd, title, why, resume, task_id}`. It creates the card
before the process so the wire name is reserved, applies `title` as an override and `why` as itself after the
runner starts, and records a `launched` event. `atrium launch` wraps that for scripts and prints the card id.

Two gaps were named here and both are closed: `atrium launch` takes `--tags` and `--prompt`.

One fact shaped everything below and it is **no longer true**, which matters more than any of the above.

It said: the daemon makes no outbound HTTP calls, the overlay code starts child processes and reads what they
printed, and atrium has never once dialled out to a third party. Sharing is embedded SDKs now rather than child
processes, and `internal/daemon/overlay_reserve.go` authenticates against the zrok REST API to reserve a name.
Only the setup step still shells out.

So the line this section drew has moved, and the line that actually holds is the one `CLAUDE.md` states:
**atrium may hold the name of a command that has a credential, and never the credential.** Dialling an overlay
this machine is already enrolled with, using a token that overlay's own tooling put on disk, is not the same
class of thing as atrium holding somebody's Zendesk key. Every proposal below still has to answer that rule
rather than the weaker "it never dials out" one.

## Layer 0: finish the hand-off that already exists

`docs/backlog.md` has "Working directories from a repo URL", which records the inversion: atrium does not learn
git worktree semantics, whatever already makes worktrees hands one over. `atrium launch --cwd` is that hand-off
and it works.

Intake is the same inversion applied one level up. Atrium does not learn what a Zendesk ticket is. `gwt zendesk`
already knows, and it already ends by starting a claude session in a directory it just made.

**What it does.** Add `--tags` and `--prompt` to `atrium launch` and to `POST /v1/launch`. Add `--external` to
record an identifier and a URL on the card. Then add a `-WithAtrium` switch to the gwt verbs, which makes them
call `atrium launch` instead of `_OpenClaudeShell`:

```powershell
atrium launch --cwd $wtPath --title "zendesk-$ticketId" --tags "zendesk,support,$ticketId" `
  --external "zendesk:$ticketId" --url "https://$zendeskHost/agent/tickets/$ticketId" `
  --why "$($firstLineOfPrompt)" --prompt $zendeskPrompt
```

Every card that arrives this way is supervised, gated, attachable in the browser and grouped by tag, and atrium
learns nothing about Zendesk.

**What it costs.** A day. Three flags, a `prompt` field appended to the harness args at launch, two columns
written rather than ignored, and a `Set-Content` in seven PowerShell verbs.

**What it depends on.** The launch path, which is done. The `external_id` column, which exists. Nothing new.

**The strongest argument against.** It does not answer what Ryan asked. He said "start an agent card directly from
that API or UI", and this requires a human at a PowerShell prompt typing `gwt zendesk <url> -WithAtrium`. It
improves the tool the owner already uses and adds no new entry point at all.

That argument is correct, and this layer is still the right first move, because every layer below needs the
launch surface to accept a prompt and a tag set before it can do anything useful. Layer 0 is on the path whatever
else gets built.

## Layer 1: an inbox the daemon owns and does not fill

**What it does.** Add `POST /v1/intake`, taking a normalized work item and nothing else:

```json
{
  "source": "github",
  "external_id": "openziti/ziti#4211",
  "url": "https://github.com/openziti/ziti/issues/4211",
  "title": "tunneler drops DNS on resume",
  "why": "assigned to me 2 hours ago. reporter says it started at 1.6.0.",
  "tags": ["issue", "openziti/ziti"],
  "suggested_cwd": "D:/worktrees/github/openziti/ziti/issue-4211",
  "prompt": "investigate openziti/ziti#4211 ..."
}
```

Atrium creates a card with no runner, no worktree it made itself, and a status that means "offered, not started".
The board shows it in its own column with a start button. Pressing start is the existing launch, with the item's
fields already filled in.

The important part is what atrium does not do: it does not know what `source` means. `github`, `zendesk`,
`discourse`, `ci`, `email` are all strings that get rendered as a badge and stored on the card. Whoever posts the
item did the reading.

**Why a new status rather than an existing one. This was wrong, and the correction is worth reading.**

The argument was: every current status describes a session. `running` and `needs-input` describe a process,
`done` and `dead` describe one that ended, `shelved` is a standing no in the permission chain and would refuse
requests from a card that has never made one. An offered item is a card with no process, which none of the six
mean. So call it `offered`.

Six of the seven. **`backlog` was skipped, and `backlog` is exactly this.** It has been in the status `CHECK`
since migration `0001`, it has a constant in `store.go`, and nothing has ever created a card in it. The board
says so in the comment above `COLUMNS`: "nothing ever creates a card in it, and an empty column on every screen
is a column you learn to skip past". An offered item is on the board and not started, which is what that word
means.

Missing it would have been a cosmetic mistake if the cost of a new status were small. It is not.

**Changing a status means changing a `CHECK` constraint, and SQLite cannot alter one in place.** The pattern for
that is in `0010` and `0014`: build the table again, copy, drop, rename. Both of those rebuild a CHILD table.
`task` is the PARENT of four `ON DELETE CASCADE` relationships, and with foreign keys on, `DROP TABLE` performs an
implicit delete that fires them. A migration written by following `0010` faithfully would have deleted every
event, every permission, every message and every launch spec in the database, and it would have looked exactly
like the two migrations it was copied from.

So the rule that came out of this, which is more useful than the entry it corrects: **before adding a status,
check whether one of the seven already means it, because the cheapest migration is the one that is not needed.**

The rest of the original entry stands. An offered card is prunable, because an item nobody started for a month is
not work, and it is never gated, because a card with no session has never made a request.

**What it costs.** Two days. A migration adding the status to the `CHECK` constraint and adding a `source` and
`url` column, an endpoint, a column on the board, and a start button that reuses the launch dialog.

**What it depends on.** Layer 0, for `prompt` and `tags` on launch. Migration `0022`, added at the end of the
slice as `schema.go` demands.

**The strongest argument against.** An inbox nobody fills is a column of nothing, and this layer deliberately
declines to fill it. It is half a feature by construction, and the half it declines to build is the half with all
the difficulty in it. There is also a real risk that it becomes a second to-do list next to the board, which is
the failure mode where a tool for organising work becomes work.

The counter is that this is the only layer that can be built once and serve every source forever, and that the
same endpoint serves a hand-written script, a poller, a CI job and a future forum peer without changing.

## Layer 2: a source is a command atrium runs on a timer

**What it does.** A `source` table, shaped like `harness`: an id, a label, an enabled flag, a command, arguments,
an interval, and a working directory. Atrium runs the command on the interval and reads its stdout as a JSON array
of the Layer 1 intake items. Anything already present by `external_id` is skipped.

```yaml
id:       assigned-issues
label:    Issues assigned to me
cmd:      pwsh
args:     ["-NoProfile", "-File", "D:/git/.../gwt-intake-github.ps1"]
interval: 15m
```

The script is three lines of `gh issue list --assignee @me --json number,title,url,body` piped through a shaping
step. `gh` holds the token, in the keyring it already uses. Atrium holds an argv and an interval.

This is exactly the `harness` pattern. A harness row says how to start a runner without atrium knowing what claude
is. A source row says how to find work without atrium knowing what GitHub is. The precedents are already in the
repo: `Prepare` on a harness runs a shell command and captures its environment, and the overlay code starts zrok
and reports what it printed.

**Rules this has to follow, taken from the existing resilience guarantees.**

- A source that fails is reported on the board and retried on the next tick. It never halts anything. Intake is
  not durable state, it is a suggestion.
- Output is bounded and parsed strictly. A source that returns 40MB or invalid JSON is disabled after three
  consecutive failures, with the reason on the row.
- The command is the operator's, run as the daemon's user, with the daemon's environment. Atrium is already a
  process launcher, so this widens nothing, but it does mean a source is as trusted as a harness and the settings
  screen has to say so.

**What it costs.** Three to four days. A table, a scheduler goroutine, a bounded runner with a timeout, a settings
screen, and failure reporting.

**What it depends on.** Layer 1. Nothing external.

**The strongest argument against.** Every source is now a script the operator maintains, and the first time one
returns bad JSON at 3am the board is wrong and nobody knows why. Polling also has no natural interval: fifteen
minutes is too slow for a PR review request and far too fast for a Discourse digest, so the interval becomes a
per-source guess that is wrong in both directions. And the whole thing is a cron daemon with a JSON contract,
which is a category of software the world has enough of.

The weaker but real counter-argument: the board already runs child processes and already has a settings screen
listing them, so the marginal complexity is lower here than it would be in most projects.

## Layer 3: mcp-gateway as the reader

**What it does.** `mcp-gateway` runs on this machine at `http://127.0.0.1:8088` and already exposes Zendesk
(`zendesk_get_ticket`, `zendesk_get_comments`, `zendesk_download_attachment`) and a large Discourse surface,
namespaced by backend id. Its own example configuration ships a GitHub backend. It holds the tokens, over an
OpenZiti or zrok transport, with per-backend tool filtering and a path policy.

If atrium wants a ticket's title without holding a Zendesk token, that server is sitting there with the answer.
Atrium becomes an MCP client over loopback, calls `zendesk_get_ticket`, and fills in the card.

**What it costs.** A week, plus a permanent dependency. Atrium would take on the MCP Go SDK as a client rather
than only as a server, an outbound HTTP client it has never had, a per-tool mapping from MCP result to intake
item, and a reconnect story for a backend that is down.

**What it depends on.** A running gateway with the right backends enabled. That is one more process that has to be
up for a feature to work, and `docs/overlays.md` already documents how much of the operator's difficulty is in
getting a second process configured.

**The strongest argument against.** It solves the wrong half. The hard problem in intake is deciding what counts
as work worth a card, and the gateway solves fetching, which was never hard. `gwt zendesk` demonstrates that: it
fetches nothing, hands the URL to the agent, and the resulting session reads the ticket through these same tools
about four seconds later. Atrium reading it first is a duplicate fetch that produces a nicer card title.

`docs/ai-platform-fit.md` ranks mcp-gateway second of four and calls it "a spike, not a commitment", for a
different reason (it has no caller identity, so gating at the gateway would collapse every agent into one card).
That objection does not apply to reading. This one does.

**Where the gateway genuinely is the right seam:** as a Layer 2 source. A source row whose command is a small
script that speaks to `127.0.0.1:8088` gets every gateway backend with no Go code in atrium at all, and the
credential stays with the gateway. That is Layer 2 with a better backend, and it needs nothing from Layer 3.

## The reverse direction: a card that links back

Three separable things get bundled under "the reverse direction", and they are not equally good.

**A link on the card. Build this.** `external_id` and a `url` column, rendered as a badge that opens the ticket.
No network, no credentials, no polling. It answers "what is this card about" for anyone who did not create it,
which after a week is everyone. This is part of Layer 1 and costs nothing extra.

**Ticket state on the board. Do not build this.** Showing that issue 4211 is now closed means polling GitHub for
every card with a GitHub external id, forever, which is Layer 2 pointed backwards and multiplies the poll volume
by the size of the board. The board's own status column is the operator's judgement about their attention. An
upstream ticket state would sit next to it saying something similar and different, and the first time they
disagree the operator has to work out which one is lying. The link already gets you the truth in one click.

**Posting the work back. Never atrium's job.** A comment on the ticket, a PR opened, a Discourse reply. The agent
in the session has the tools, the context and the credentials, and it is the only party that knows what it did.
Atrium's contribution to this is the message channel it already has: `POST /v1/tasks/{id}/message` reaches a
session through its next tool call or its Stop hook, so "post a summary back to the ticket" is a message, and
the agent's existing `discourse_create_post` or `zendesk` tool does the work. That path exists today.

There is one thing worth wanting here, and the backlog already names it. "An agent cannot say it finished" is the
largest hole in what atrium does, and it is the same hole in intake: a card raised from a ticket that reaches
`done` should be able to say so in a way that a script could read and turn into a comment. Whatever fixes that
fixes the useful part of the reverse direction as a side effect. Nothing about ticketing should be built on top
of a `done` state that only a human can set by hand.

## How this must not be built

The obvious implementation of Ryan's request is a `github.go`, a `zendesk.go` and a `jira.go` under
`internal/intake/`, each with a token in the settings table, each with an OAuth flow, each with a webhook
endpoint. Every one of those four things is a mistake here, for a reason already written down.

**Atrium must not hold a provider credential.** `CLAUDE.md` puts authentication out of scope, `docs/overlays.md`
records that atrium "never handles an identity" even for the overlay it drives, and `docs/backlog.md` lists
"holding provider credentials" among the things refused outright after reading Charon. A GitHub personal access
token in the SQLite database is a credential atrium holds, in a database with no encryption, reachable from a
board with no login, which is fine on loopback and stops being fine the moment a share is up. The rule that
follows: **atrium may hold the name of a command that has a credential, and never the credential.** `gh` has a
token. `zrok` has an environment. `mcp-gateway` has four backends' worth of secrets. Atrium has argv.

**Atrium must not accept an inbound webhook.** A webhook needs a publicly reachable address, and the only way
atrium gets one is the overlay it drives. Publishing an unauthenticated write endpoint on that overlay would
create exactly the thing `docs/backlog.md` warns about under "A share widens what loopback means": every request
presents as `127.0.0.1`, so no source-address check means anything. Verifying a webhook signature would mean
holding a shared secret per provider, which is the previous rule again. Intake pulls, or something already on this
machine pushes to loopback. It does not listen to the internet.

**Atrium must not become an OAuth client.** Token refresh, scope negotiation, consent screens and expiry are a
whole subsystem, and its failure mode is a board that quietly stops seeing work. `gh auth`, `zrok enable` and the
gateway's config each already solve this for their own service, and each is better at it than a first attempt
here would be.

**Atrium must not model a ticket.** The moment there is a `ticket` table with a status and an assignee, atrium is
a worse Jira with no permissions. A card is the unit. An external system contributes text, tags and a URL to a
card, and stops there.

The line that stays true through all of this is the one `docs/overlays.md` already uses for overlays: atrium keeps
the configuration, starts the process, and shows what it printed.

## Engineering and support are not the same shape

Everything above treats a source as a source. `github`, `zendesk` and `discourse` are strings that become a badge,
and that is the right call for the transport. It is the wrong call for the design, because the two kinds of work
behind those strings differ in a way that decides how much of a card can be filled in before a human looks at it.

The ask named both: bugs, pull requests and enhancement requests on one side, and Zendesk on the other. They are
not two instances of one thing.

### An engineering item names a directory. A support case names a person.

| | Engineering item | Support case |
| --- | --- | --- |
| What it identifies | a repo, usually a branch | a customer and a symptom |
| Working directory | derivable, and gwt already derives it | unknown, and often unknowable until somebody reads it |
| First action | do the work | find out what this is about |
| Ends when | merged, closed, released | the customer agrees it is over |
| Produces | a commit | a reply, and sometimes zero to several engineering items |
| Identifier | globally meaningful, `openziti/ziti#4211` | meaningful inside one instance, `zendesk:12345` |
| Text on the card | public | someone else's |

Five things follow from that table, and each of them changes something concrete.

**A support case can be offered and cannot be prepared.** `gwt pr` makes `pr-<n>` on the PR's head branch, so a
review request can arrive with a directory, a branch and a seed prompt already correct, and the start button is
the only thing left. `gwt zendesk` makes `zendesk-<id>` on a branch named after the ticket, which is a directory
in the right shape and, importantly, a directory in *some* repo chosen by whoever typed the command. Nothing in
the ticket said which repo. So a support item posted to `/v1/intake` legitimately has no `suggested_cwd`, and the
inbox has to be able to hold a card that cannot be started until a human answers one question. That is the
strongest argument for the `offered` status in layer 1, and it is stronger than the argument layer 1 makes for
itself: without it there is nowhere to put an item that is real work and not yet a session.

**The seed prompt differs in kind, not in wording.** An engineering prompt is imperative. A support prompt is
interrogative, and gwt already writes it that way: read the ticket, summarize it, list the attachments, ask which
to download, then work out a plan together. That is a triage prompt, and its output is a decision about what the
work actually is. Atrium stores a prompt as text and does not care, which is the correct amount of caring, but
the two default prompts a source ships with should not be written by someone who thinks they are the same
sentence with a different noun.

**One support case becomes several cards, and the link between them is a tag.** A ticket that turns out to be
two bugs in two repos produces a triage card and two fix cards. Neither Zendesk nor GitHub models that
relationship well, and atrium must not try: a `ticket` table is the thing this document already refuses. What it
has instead is free text tags, and `zendesk-12345` on all three cards is the entire relationship, filterable and
groupable today with no new mechanism. `store.go:100` already names this case in a comment: "a card is also a
support case, a tangent, a pull request, a lab, and none of that is in the path."

**Attachments are the reason support meets file transfer.** A support ticket carries logs, configuration and
support bundles, and they are frequently the whole content of the case. `mcp-gateway` already exposes
`zendesk_download_attachment`, and the agent in the session has that tool, so the download belongs to the agent
and lands in the working directory. `docs/file-transfer-design.md` covers the other direction, which is the one
with no answer today: getting a file the customer sent you by some route that is not the ticket into a session
running on a machine you are not sitting at.

**A support case carries someone else's data, and a card is not a safe place to put it.** This is the one that is
genuinely new and it does not apply to the engineering side at all.

An engineering item is public: an issue title, a PR description, a stack trace from a repository anybody can
read. Copying it into `why` costs nothing. A support case is a named customer describing their own network, and
the fields it would naturally fill are `title` and `why`, which are:

- stored in a SQLite database with no encryption at rest,
- rendered on a board with no login, by decision, in `CLAUDE.md`,
- and reachable from another machine the moment a zrok or OpenZiti share is up, per `docs/overlays.md`.

None of those three is a flaw. Together they mean the board is a fine place for "what was I doing" and a poor
place for a customer's hostnames.

So the rule for a support source, and it is a rule rather than a preference: **carry the identifier and the URL,
and as little prose as gets the job done.** `zendesk-12345` as a title, `support case, unread` as the why, the
URL as the link. The agent reads the ticket through its own tools four seconds later, in a session whose
transcript is subject to whatever the operator already decided about transcripts, and the customer's words never
enter atrium's database at all. This costs a less descriptive card and it is worth it, and it happens to be
exactly what `gwt zendesk` already does, for reasons that were probably about effort rather than about privacy.

The same rule read backwards is the argument against ever fetching ticket bodies daemon-side, which layer 3
already loses on other grounds. Layer 3 would put customer prose into atrium's own memory to make a nicer title.

### What the reverse direction means for each side

`The reverse direction` above splits three ways and says posting back belongs to the agent. That stands, with one
distinction between the two sides worth stating.

An engineering result is self describing and self reviewing. A pull request is the artifact and the diff is the
argument, so an agent opening one is proposing something a human will read before it lands.

A support reply has no such gate. It goes to a customer, and the first person who reads it is the customer. So a
support card's output is a **draft**, and the send is a human act. Atrium does not need to enforce that, because
atrium never posts anything, but the seed prompt a support source ships with should say it, and the difference
should be written down before somebody wires a card action called "reply to the customer" that does.

### What this does not change

The layers stay as they are and in the same order. Layer 0 is still first and still needed by everything. The
`offered` status in layer 1 gets a better argument than it had. Layer 2 gains a note that engineering sources and
support sources want very different intervals, which is the polling-interval objection already recorded, made
sharper: a review request is stale in an hour, and a support queue somebody is paid to watch does not want a
second watcher at all.

And the refusals in "How this must not be built" all hold harder on the support side than the engineering side.
A Zendesk token in the database is worse than a GitHub token in the database, for the same reason the prose rule
exists.

## What else is in this category

Ryan named source control and ticketing. The gwt script and the daemon between them suggest several more, and
Layer 1 makes every one of them a script rather than a feature. Listed with what they would actually be worth.

- **CI failures.** A failed workflow run on a branch you own is the clearest possible intake item: it has a cause,
  a log, a repo and a directory that probably already exists. `gh run list --json` is the whole source. gwt
  explicitly refuses to handle an Actions URL today (line 1441, it falls through to the "cannot infer a branch"
  path), which marks this as a known gap rather than a new idea. Highest value of anything on this list.
- **PR review requests.** Named by Ryan, and worth separating from issues because the directory rule is different:
  `gwt pr` already makes `pr-<n>` on the PR's head branch, so the worktree is deterministic and the card can be
  fully prepared. `gh search prs --review-requested=@me`.
- **Security advisories and backports.** gwt has `advisory`, `ghsa` and `backport` verbs, which means the owner
  does this often enough to have automated it three ways. A source that raises a card per open advisory, or per
  merged PR not yet on an LTS line, is a small script over `gh api` and lands on the exact worktree gwt would
  have made.
- **A TODO scan.** Cheapest possible source: `rg -n "TODO|FIXME"` over a repo, one card per cluster. Also the one
  most likely to produce a hundred cards nobody wanted, so it needs a threshold before it needs anything else.
  Worth building first purely as the proof that a source can be a shell one-liner.
- **Unanswered Discourse topics.** The gateway exposes `discourse_filter_topics` and `discourse_search`, and gwt
  already has the verb for working one. "Topics in these categories with no staff reply in 48 hours" is a query,
  not an integration.
- **Alerting.** Anything that already pages the operator could raise a card instead of, or as well as, a
  notification. This one is a trap, because an alert is urgent and a card is not, and putting a page on a board
  that is checked hourly is a way to miss a page.
- **Calendar and email.** Named for completeness. Both are plausible sources and both are worse than the others,
  because neither has a repo, a branch or a directory, so the resulting card has no worktree and no runner and
  is a note. Atrium already refuses to be a to-do list, and a calendar source is the fastest route to becoming
  one. If either is ever wanted, the useful shape is narrow: a meeting with a repo named in the invite, or an
  email from a specific address with a ticket id in the subject.
- **`gwt sessions audit`, pointed at the board.** The one source that needs no external system at all. gwt already
  classifies worktrees into `REMOVABLE`, `REVIEW` and `KEEP`, and already checks whether a `pr-<n>` worktree's PR
  merged. `REVIEW` is a list of work that was started and not finished, which is precisely a card. This is a
  handful of lines and it is the only item here that a person could finish in an afternoon.

## Open questions

- **Which of Layer 2 and Layer 3 is actually wanted, or neither.** Layer 1 plus a hand-written script covers the
  real cases and can be tried in an hour. Building the poller before trying that would be guessing.
- **Whether an `offered` card should be a card at all**, or a separate table that becomes a card on start. A card
  with no session breaks the assumption that a card describes a runner, and several queries filter on statuses by
  name. A separate table means a second thing to render and a second thing to prune.
- **What a source does when the directory does not exist.** `suggested_cwd` names a worktree gwt has not made yet.
  Either atrium refuses to start until a human makes it, or the start button runs a configured command to create
  it, which is `gwt new` wearing a different hat and re-opens the question the backlog closed under "Working
  directories from a repo URL".
- **Deduplication across restarts and across machines.** `external_id` is unique per source at best, and once
  there is a forum, two leaves polling the same GitHub account raise the same card twice. The `wire_name`
  qualification that `atrium name` introduced is the existing answer to a near-identical problem and is probably
  the shape here too.
- **Whether the seed prompt belongs on the card.** gwt stores `PromptText` in the ledger entry and passes it as a
  single argv element, deliberately never interpolated into a command line. If atrium takes a `prompt`, it
  inherits that requirement exactly, and `expandTemplate` in `launch.go` already has a comment about what goes
  wrong when a command is joined into one string.
