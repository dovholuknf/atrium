# Recognisers

A recogniser is a row that says what a URL means. Paste a pull request into the board and you get a launch
dialog that already knows the repository, the organisation, the host, the branch, the title and a first
instruction.

**Atrium learns nothing about GitHub, Bitbucket, Jira or any ticketing system.** A recogniser is a pattern and a
mapping, and whoever wrote the row did the understanding. That is the rule that lets this serve a system nobody
has thought of yet. These files are the rows a hub starts with, and they are rows like any you write. See `docs/runtime/scm-design.md`.

## The shape

```
  pattern      a regular expression with named groups
  rank         ordered, most specific first
  label        what to call this kind of thing, shown when it answers
  cwd          a template: where the work happens
  title        a template
  tags         a template, comma separated
  prompt       a template: what the agent is told first
  branch       a template, recorded on the card
  window       a template: which pile the card lands in
  theme        a template: the terminal palette
  kind         a template: what to record as the source
  fetch        OPTIONAL: an argv that prints more facts as JSON
```

Templates read `{host}`, `{org}`, `{repo}`, `{num}`, `{url}` and anything a `fetch` command adds. Whatever the
pattern captures is a variable by that name.

`host`, `org` and `repo` also go onto the card as themselves, which is how two forks of one repository stop
landing in the same pile.

## The three rules worth knowing before you write one

**Order is the dispatch.** A URL matches the first enabled row that wants it, lowest rank first.
`.../pull/5/files` and `.../pull/5` are the same pull request, and a generic "any repository" pattern will
happily swallow both, so the specific rows sit above the generic one. Keep a shrug at the bottom: a URL on a
host you know that matches nothing specific is still worth a dialog.

**A hole is left standing.** A template that reads `{branch}` when nothing supplied one produces a field that
still says `{branch}`, and the dialog says so. It is not blanked, because `feature/{branch}` blanked becomes
`feature/`, which is a directory somebody creates by accident.

**Atrium never makes the directory.** `cwd` is a path, and how that path comes into existence is somebody else's
command. Where it is not there, the dialog says `... is not here yet. make the worktree, then start it`, and
you go and make it with whatever makes worktrees here. Atrium does not know git and is not going to learn.

## `fetch`, and why it holds no credential

The pattern gives you an issue number. Turning that into a title needs an authenticated network call, and atrium
does not hold a credential for one. The built-in fetch asks the forge:

```
  fetch forge   fetch_args ["pr"]      title, headRefName, baseRefName, headRefOid, author
  fetch forge   fetch_args ["issue"]   title, body, author, state
```

It reads `{host}`, `{org}`, `{repo}` and `{num}` from the captures. **A room with a hub never runs gh or bb**: only
the hub talks to GitHub or Bitbucket, so the room asks the hub and the hub's own CLI login answers. A room with no
hub is its own hub and asks its local CLI. On a room with a hub, a row whose fetch is `gh`, `bb` or `glab` is
refused with a sentence that says to use `forge`.

Any other fetch is an argv you wrote, and atrium reads a JSON object off its stdout. Every key in that object
becomes a variable. There is nowhere in a recogniser to put a credential, the same as a source, and that is the
design rather than an omission.

**The captures win.** A fetched fact only fills a name the pattern left empty, and never overwrites one it
filled. A fetch reads whatever an issue tracker holds, and anybody can write into an issue tracker: one that
could redefine `repo` could move `cwd`, which would mean the contents of an issue chose the directory a runner
starts in.

**A failing fetch is never fatal and never switches the row off.** The reason goes on the row, and the URL still
resolves from its captures. Unlike a source, which is a timer nobody is watching, a recogniser runs because
somebody just pasted a link with the board in front of them, and a row that switched itself off would turn "gh
was not logged in" into "atrium is broken".

Other rules, the same as a source: one megabyte of output, and thirty seconds. A fetch answering one issue
number with more than that is reporting a repository.

## What is in here

| File | What it recognises |
| --- | --- |
| `github.json` | Pull requests, issues, and a bare repository as the shrug at the bottom. |
| `bitbucket.json` | Pull requests, which are the same thing spelled differently in the path. |
| `support.json` | Zendesk tickets and Discourse topics. Its rows default to the openziti/ziti worktree. |
| `load.ps1` | Loads a file of rows into atrium. A loop over `PUT /v1/recognisers/{id}`. |

Every example points at this operator's worktree layout, which is not yours. Pass `-Root` or edit the `cwd` of
each row afterwards.

**The hub owns the table.** A hub seeds its table once from these files, which are built into the binary
(`seed.go`), so a hub and every room it has recognise a pasted link with nothing loaded. A row you edit is never
overwritten, and a row you delete does not come back. A room with a hub reads the hub's rows. A room with no hub,
or one whose hub cannot be asked, keeps its own table. Against a hub, `load.ps1` writes the hub's table and `-Room`
is not needed. Against a single room with no hub it writes that room's. A 400 (a pattern that does not compile)
prints the hub's own sentence. See `docs/rnd/card-lifecycle-design.md`, section 2.

```powershell
./load.ps1 -Path ./github.json -Root D:/worktrees/github
./load.ps1 -Path ./support.json -Room sg4
atrium open https://github.com/openziti/ziti/pull/4211
```

## Writing your own

Write the pattern first and try it with nothing else filled in. `atrium open <url>` prints what resolved and
starts nothing, which is the loop: paste, look, adjust a template, paste again. The same box is in the edit
dialog under "try a url", and it names the row that answered, so "the generic row above swallowed my URL" is a
thing you find out rather than wonder about.

Add `fetch` last. A row with a good pattern and no fetch already fills in the directory, the tags and the
window, which is most of what makes pasting a link better than typing a path.

The most valuable row for a team is probably not a pull request at all. It is whatever your support tool's
ticket URL looks like, pointing at a repository somebody has to be asked about, with a prompt that says what to
read first.
