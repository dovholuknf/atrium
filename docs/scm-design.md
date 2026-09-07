# Source control and ticketing: work that arrives from somewhere else

Two things, related only by the word "integration", and separating them is the first decision in this document.

**Inbound.** You are looking at a pull request, an issue, a ticket. You want a card, in the right directory,
with the right context already in it. Today atrium can only be pushed work by a timer.

**Outbound.** Atrium's own configuration lives in one SQLite file in a home directory. Runners, fixtures,
sources, actions and standing permission rules are all things worth reviewing, diffing and keeping in a
repository, and none of them can leave. The constraint that shapes the whole answer is "any SCM": not GitHub,
not an integration, files on disk.

They share nothing but a document. They are here together because they were asked for together.

---

## Part one: turning a URL into a card

### What exists, and why it is the other half

`docs/intake-design.md` covers **sources**: commands atrium runs on a timer, whose output becomes items in an
inbox. That is work being PUSHED at you, filtered, deduplicated, waiting for you to look.

This is the pull. You have the thing in front of you and you want to act on it now. The two are not variations
of one feature: a source is a standing subscription and this is a single verb.

### The prior art is already on this machine

`git-worktree.ps1` is six thousand lines and the first two hundred of the URL handler are the design. Paste any
URL at `gwt` and it recognises the shape:

| Shape | What it does |
| --- | --- |
| `<host>/<org>/<repo>/pull/<n>` | worktree on the PR's branch |
| `bitbucket.org/<org>/<repo>/pull-requests/<n>` | same, different spelling |
| `<host>/<org>/<repo>/issues/<n>` | branch named for the issue, prompt pointing at it |
| `<host>/<org>/<repo>/security/advisories/GHSA-...` | reads the advisory, finds GitHub's temporary private fork |
| `<base>-ghsa-xxxx-xxxx-xxxx/tree/<branch>` | the fix branch on that fork, as a worktree on the real clone |
| `*.zendesk.com/.../tickets/<n>` | a support ticket, with the repo asked for |
| `<host>/<org>/<repo>` | clone it if missing, open it |
| anything deeper on a known git host | cannot infer a branch, so it asks |

Four properties are worth stealing exactly, and one is worth refusing.

**Steal: it is an ordered list, most specific first.** `.../pull/5/files` and `.../pull/5` are the same pull
request, and the specific pattern strips the tail. The generic repo pattern sits at the bottom so it cannot
swallow the specific ones.

**Steal: the last entry is a shrug, not an error.** A URL on a known host that matches nothing falls through to
"I cannot work out a branch, tell me what to call it". Refusing would be correct and useless.

**Steal: named captures feed the flow.** `host`, `org`, `repo`, `num`. The flow does not re-parse the URL.

**Steal: the host is a first-class field.** `bitbucket.org` and `github.com` differ in more than the word in the
path, and gwt threads `RemoteHost` through everything rather than assuming GitHub.

**Refuse: doing the work in the recogniser.** gwt's advisory branch fetches the advisory body, clones a private
fork, and adds a remote, all inside the dispatch. That is why it is six thousand lines. Atrium already has the
separation: a recogniser produces FACTS, and something else decides what to do with them.

### The shape

**A recogniser is a row, not code.** This is the "make it customizable so I can tweak it" requirement, and it is
also how atrium already does everything else: a runner is a row, a source is a row, an action is a row. Nothing
about GitHub is special-cased in the binary.

```
  pattern    a regular expression with named groups
  label      what to call this kind of thing, shown on the card
  title      a template over the captures
  tags       a template, comma separated
  cwd        a template: where the work happens
  prompt     a template: what the agent is told
  fetch      OPTIONAL: an argv that prints more facts as JSON
  prepare    OPTIONAL: an argv run when `cwd` does not exist, then `cwd` is
             checked once more. PER RECOGNISER, with a daemon-wide default
             used when the row leaves it empty.
  rank       ordered, most specific first
```

**`prepare` is per recogniser and not only daemon-wide,** because the flows differ in the one way that matters:
a pull request checks out an existing branch, an issue creates a new one, and an advisory clones a fork nobody
has locally. One command cannot be all three. The daemon-wide default exists because most rows will want the
same thing, and repeating it in eight rows is eight places to fix a typo.

It runs ONCE and then `cwd` is checked again. Not a loop, not a retry: a `prepare` that ran and did not produce
the directory has failed, and the failure to report is "I ran this and the directory still is not there",
naming the command, which is a sentence somebody can act on.

Templates read `{host}`, `{org}`, `{repo}`, `{num}` and anything a `fetch` command adds.

**`fetch` is the whole extensibility story, and it holds no credential.** The pattern gives you an issue number.
Turning that into a title needs a network call and an authenticated one. Atrium does not do it: it runs an argv
the operator wrote, exactly as `sources` already does, and reads JSON from its stdout.

```
  gh issue view {num} --repo {org}/{repo} --json title,body,labels
  bb pr show {num}
```

`gh` already has a token in the keyring it already uses. This is the rule from `CLAUDE.md`: atrium may hold the
NAME of a command that has a credential, and never somebody else's credential. It is the same rule sources are
built on, the same bounded-output handling, and the same "three failures switches it off".

**What it produces is a card, and a card is where it stops.** The recogniser fills in a directory, a title, tags
and a prompt. It does NOT start a runner. Starting work is a decision, and it is the decision the launch dialog
already exists to take: this fills the dialog in.

### Where the directory comes from

The hardest part, and the part where atrium should do LESS than gwt does.

gwt owns a worktree layout: `$GIT_ROOT` for clones, `$WORKTREE_ROOT` for worktrees, a naming convention, and it
will clone a repo it has never seen. Atrium has no business owning any of that. It would be a second, worse
implementation of a tool that already works and is already on the PATH.

So: **`cwd` is a template, and if the directory it names does not exist, atrium runs the row's `prepare` argv
once and looks again.** For this operator that argv is `gwt pr {url}` or `gwt new {branch}`. For somebody else
it is a shell script. Atrium's contribution is knowing which card it belongs to, not knowing how to lay out a
checkout.

The recogniser's job is to produce a PATH. How that path comes into existence is somebody else's command, named
in a row, and the whole of atrium's worktree knowledge is one string.

### Where a URL gets pasted

Three places, all of which are one function:

- **The launch dialog's directory field.** Paste a URL, the form fills in.
- **`atrium open <url>`,** so a browser can be made to hand URLs to it.
- **The inbox.** An intake item already carries a link; recognising it turns "here is a thing" into "here is a
  card ready to start".

### What this deliberately does not do

- **No webhooks, and no listener.** That is a service with an address and a secret, and atrium is loopback with
  no auth. A source on a timer already answers "tell me when something changes" and it dials out.
- **No writing back.** Atrium does not comment on issues, move cards on a project board, or close tickets. The
  agent in the terminal can do all of those with the tools it already has, and an atrium that writes to your
  issue tracker is holding a credential.
- **No repository model.** Atrium does not learn what a repo is, keep a list of them, or clone. A path and a
  command that produces it.

---

## Part two: settings that can live in a repository

### The problem

Everything atrium knows that is not a card is configuration somebody wrote: five runner definitions, the
fixtures that come up in the morning, the sources that fill the inbox, the actions, and the standing permission
rules. That last one is the sharp end. A permission rule is a policy decision about what an agent may do without
asking, and it lives in a SQLite blob with no history, no diff, and no way to tell what it looked like last
week.

### The answer is files, because the requirement is "any SCM"

Not a GitHub integration. Not a sync service. A directory of files that atrium can write and read, which git,
Mercurial, Subversion, Fossil, a Dropbox folder or a USB stick all handle identically because none of them is
being asked to do anything clever.

```
  atrium.d/
    runners.yaml        harness rows
    fixtures.yaml       what starts with the daemon
    sources.yaml        commands on a timer
    actions.yaml        named prompts
    rules.yaml          standing permission rules
    recognisers.yaml    part one, above
    settings.yaml       the daemon-wide settings that are not machine-specific
```

One file per table, because the unit somebody wants to review is a table. A single file would make every diff a
diff of everything.

### What must NOT be exported, and this is the important list

- **Cards.** They are state, not configuration. A card is a live thing with a status and a runner.
- **The event log, permissions granted, messages, recaps.** History of this machine.
- **Anything machine-specific.** The browse roots, the shell command, absolute paths that only exist here.
  Exporting these is how a config repository becomes unshareable on the second machine.
- **Anything that resembles a credential.** There should be none, by design, but the exporter checks rather than
  asserting: a source's argv is written by a human and a human can paste a token into one.

That last point wants stating plainly: **a permission rule and a source argv are the two places where something
sensitive could plausibly end up**, and both are exported, so the export needs a look-before-you-commit pass in
the same spirit as the `safe-to-push` habit.

### The list above is not enough on its own, and this is the part that decides it

Naming whole tables as exportable sounds like a policy and is not one. Every table named above contains fields
that are true only of this machine: a runner's `cwd` is `D:\worktrees\...`, a fixture names a directory, a
source's argv may begin with an absolute path to a script in a home directory. An exporter that faithfully
dumped those tables would produce a file that is correct, reviewable, and useless on the second machine, while
satisfying every rule stated so far.

So the policy is **per field**, and every field is one of three things.

| | Meaning | Example |
| --- | --- | --- |
| **exported** | true anywhere, safe to read | a runner's label, its arguments, a rule's pattern and verdict |
| **local** | true of this machine only, written as a placeholder | a fixture's directory, a runner's `cwd`, the browse roots, the shell command |
| **flagged** | exported, and named in the export as needing an eye first | any argv: `cmd`, a source's command, `fetch`, `prepare` |

**`local` fields are exported as a NAME, not as a value.** A fixture that starts in `D:\worktrees\atrium` is
written as `cwd: {worktree_root}/atrium`, and the importing machine resolves it. A field with no sensible
placeholder is written as an empty value and a comment saying what it was for, so the importer shows it as
something to fill in rather than silently starting a runner in the wrong place.

**`flagged` is a real category and not a hedge.** An argv is the one field in this whole design written by a
human in free text, and it is the field a token gets pasted into by somebody in a hurry. It cannot be dropped,
because a source with no command is not a source. It cannot be exported quietly, because the failure is
publishing a credential. So it is exported with a line in the export's output naming every one of them, which
is the same posture `/safe-to-push` takes and for the same reason.

An exporter with no such table has to invent one, and it will invent it per field as it goes, which is how half
the fields end up in the wrong category.

### The absolute path rule, stated once

**No absolute path leaves this machine.** Not in a runner, not in a fixture, not in a source, not in a
recogniser's `cwd` template, not in the settings file. Anything that is one is either a placeholder or omitted.

This is a stronger rule than "do not export machine-specific values" and it is stronger on purpose: it can be
checked mechanically, which "machine-specific" cannot.

### The direction that is hard is import

Export is a dump. Import has to answer what happens to what is already there, and the wrong answer is "replace
everything", because that silently deletes a runner somebody added on this machine.

**Import is a diff and a confirmation, never a write.** Show what would be added, what would change, and what
exists here and not in the file. Apply on a press. This is the shape `docs/test-plan.md` section I already
describes for importing permission rules from Claude Code, and that is the precedent to follow rather than a
second mechanism.

**Identity across machines is by name, not by id.** The ids are ULIDs generated here. A runner called `claude`
in the file is the runner called `claude` on this machine.

### YAML, with a reservation

Chosen because a human edits these files and reviews their diffs, and JSON's lack of comments is a real cost for
a policy file. The reservation is that atrium has no YAML dependency today and adding one for this is a real
cost too. If that turns out to be the deciding factor, JSON with a documented formatting convention is a
perfectly good second choice and nothing else in this design changes.

---

## What to build first

1. **The exporter.** It has no dependencies on anything else here, it is the smaller half, and it turns the
   permission rules into something reviewable, which is the piece with the sharpest edge today.
2. **The importer's diff view.** Without it the exporter is a backup rather than a workflow.
3. **The recogniser table and its templates,** with no `fetch` and no `prepare`: paste a PR URL, get a filled-in
   launch dialog pointed at a directory you already have.
4. **`fetch`,** which makes the card's title and tags real.
5. **`prepare`,** which is the one that makes it work for a repository not yet checked out, and which is really
   just "call gwt".

Each of those is useful alone, and each one shipped without the next is not a half-finished feature.

---

## What was built, and the two places it differs from the above

One through four are built. Five is not, and is `docs/backlog.md` item 2.

The table is `recogniser` in the store, `GET`/`PUT`/`DELETE /v1/recognisers` on the API, `POST /v1/recognise`
for the verb, `atrium open <url>` on the command line, and a pane under runners on the board. The three places a
URL gets pasted are all there: the launch dialog's link box, the CLI, and an offered card in the inbox whose
link is put in that box ready to press. `scripts/recognisers` holds working rows and a README.

**A failing `fetch` does not switch the row off,** which the `fetch` section above says it should, by analogy
with a source. The analogy is wrong in one respect and it is the deciding one. A source is a timer nobody is
watching, so a broken one retrying forever is a process spawned every fifteen minutes to produce an error into
an empty room, and switching it off is a kindness. A recogniser is a verb somebody just typed with the board in
front of them. Switching the row off would mean the NEXT paste silently matches nothing while they watch, which
turns "gh was not logged in" into "atrium is broken". So the reason goes on the row where the settings screen
shows it, the count says how long it has been going on, and the URL still resolves from its captures. Losing the
fetched title is a worse card. Losing the card is worse than that.

**The captures win over the fetched facts,** which the section above does not say either way. A fact only fills
a name the pattern left empty and never overwrites one it filled. The reason is that a `fetch` reads whatever an
issue tracker holds, and anybody can write into an issue tracker: a fetch that could redefine `repo` could move
`cwd`, which would mean the contents of an issue chose the directory a runner starts in.

One thing the design leaves implicit is worth stating, because it is the behaviour somebody will meet first. A
template reading `{branch}` when nothing supplied one leaves the field saying `{branch}` and reports it. Blanking
it would turn `feature/{branch}` into `feature/`, which is a directory somebody creates by accident and then
wonders about, and failing the whole resolution would throw away the five fields that did work because the sixth
did not.
