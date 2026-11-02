---
title: How a card gets code and hands it back
description: The path code takes between a card, its room, the hub and GitHub, and the one git rule a card lives by.
---

# How a card gets code and hands it back

A card is an agent working in a room. It needs code it does not have, and it needs somewhere to put what it wrote.
It never talks to GitHub for either. This page follows the path and explains the one rule that keeps it that way.

## The picture

```text
  card ---> its room's forwarder ---> the hub's git store ---> the forge (GitHub)
  git       <agent>/git/              a bare copy of each        only the hub
  fetch hub                           repo: branches rooms       talks to forges
  push hub                            pushed, plus main
```

- **The card** runs ordinary git against a remote named `hub`.
- **The room's forwarder** is a small git endpoint on the room's agent address. It carries the card's request to the
  hub, and a call with no card token is refused. A card cannot reach the hub's store any other way.
- **The hub's store** holds a bare copy of each repository: the branches rooms have pushed, plus `main`. It is the one
  place every room can read from, which is why a branch pushed from one room can be fetched from another.
- **The forge** is GitHub. Only the hub talks to it. **A card never touches a forge**, so no card needs a token for
  one, and a card in a room with no GitHub login can still get a branch from it.

Work in progress stays in its room. A card on another room cannot fetch a commit that was never pushed. The card that
holds it pushes it to the hub first, and then everyone can reach it.

## The lookup: where does a branch live?

A card does not guess where a branch is. It asks the hub with `atrium_git_url`, which is `GET /_hub/git/url` for
anything that is not a card:

```text
atrium_git_url { repo: "owner/repo", branch: "fix/login" }
```

`repo` may be `<owner>/<repo>`, `<host>/<owner>/<repo>`, a bare name or a forge URL. The answer is `found`,
`not found` or `offline`, and a found branch lists the places it can be fetched from, each with its sha:

| Source | Meaning |
| --- | --- |
| `hub` | The hub's store has it. The URL is on this room's own forwarder, and the answer says how to fetch it. |
| `room` | A room has commits there that were not pushed. Work in progress is passed through, not handed out. |
| the forge | Nobody holds the branch, so the hub got it from GitHub (next section). It is marked as a forge copy. |

A card is told to fetch only what it can: a room's unpushed work answers with a note asking the card that holds it to
push, and no URL. The operator, asking from the board, is given the room's URL as well. Leave `branch` out and the
answer lists every branch the hub holds and every branch an attached room would serve. A miss offers up to five
closest names. A room that is not attached is listed as offline, and a branch pushed to the hub is still answered
while its room is away.

`main` and the integration branch are answered as read-only: a card can fetch them to base work on, and a push to
either is refused.

## The forge fetch: when nobody holds the branch

When neither the hub's store nor an attached room has the branch asked for, the hub asks the forge. If the forge has
it, the hub fetches it into its own store under `refs/forge/<branch>` and answers with that.

- The branch is served as `refs/heads/<branch>`, unless a room pushed a branch of that name. A room's push always
  wins, and the forge copy never overwrites it.
- `git fetch hub forge/<branch>` always gets the forge copy. The hub fetches it again on every ask, so a colleague's
  new commits arrive.
- A repository the hub never held is created on the first ask.
- A private repository, with no `gh` login on the hub, answers **"the hub has no credential"**. The login is the
  hub's to make, since only the hub talks to forges.
- **Ask the lookup first.** A bare `git fetch hub <branch>` cannot start the first fetch, because git only reads what
  the hub already holds. The lookup is what makes the hub go to the forge.

So the card's sequence is: call `atrium_git_url`, then run the `git fetch` the answer gives.

## The hook's rule

Atrium does not ship the hook. It is the recommended guard, a `PreToolUse` hook in your own Claude Code setup, and it
is what keeps cards on this path. It refuses every git command that could reach a remote, and allows exactly these:

```bash
git fetch hub
git push hub <branch>
git push -u hub <branch>
```

The remote may be named `atrium-hub` instead of `hub`. Nothing else passes:

- **No force**, no `+` refspec, no `:` refspec, and no extra flags.
- **One leading directory change** is allowed, `cd`, `sl`, `Set-Location` or `pushd`, followed by a newline or `&&`.
  This lets a card fetch a sibling repository. The remote is then checked in **that** directory.
- **The remote must be this room's forwarder.** Every url and pushurl of the remote must start with
  `<agent>/git/`, where `<agent>` is the `agent` address in the room's `daemon.json`. The file is found through
  `ATRIUM_LOCATION`, else `%LOCALAPPDATA%\atrium\daemon.json`. The default agent address is `http://127.0.0.1:7777`.
- A clone whose `hub` points at **another room's** forwarder is refused, and so is a room with no `daemon.json`.

A remote that points at GitHub, a colleague's machine or a different room is not a `hub` as far as the hook is
concerned, whatever it is named.

## Adding a `hub` remote to a clone

A clone has no `hub` remote until someone adds one. Add it with the forwarder address of the room the card runs in:

```bash
git remote add hub http://127.0.0.1:7777/git/hub/github/owner/repo.git
```

The address starts with `<agent>/git/`, so `http://127.0.0.1:7777/git/` for a default room. The lookup prints the
exact URL for a repository, and `git remote get-url hub` shows what a clone has now. If the room's agent listens
somewhere else, use the `agent` address from its `daemon.json`, or the hook will refuse the remote.

Then a card can `git fetch hub` and `git push -u hub <branch>` from that clone. A clone shared by cards in two rooms
needs a remote per room, and each card must use its own.

## Landing on claude/main

A card cannot push `claude/main`, and a push to it is refused. It pushes its branch with `git push hub <branch>` and
runs `atrium merge` (or `atrium_merge`). The hub merges the branch onto `claude/main`, runs the tests and answers
merged with the sha, red with the failing tests, a conflict, or refused with the reason. `atrium merge --list` shows
the queue. Landing `claude/main` on `main` is the operator's `atrium merge land` or **Land now** on the Terminals tab,
and the hub never pushes to the forge.

## What you see when git is refused

A card that runs `git push origin main`, `git pull` or `git fetch origin` is stopped before git starts, and sees this
text:

> claude never pushes, pulls, or fetches -- no git command may reach a remote. Hand it to the user. The one exception
> is atrium's hub remote: exactly 'git push hub &lt;branch&gt;', 'git push -u hub &lt;branch&gt;' or 'git fetch hub', optionally
> after one 'cd &lt;dir&gt;' (newline or &&), and only when that remote points at this room's atrium forwarder.

The reason is the rule above. A card that could reach any remote could push to the wrong place, force over a
colleague's branch, or carry a credential it was never meant to hold. Handing the work to the hub means the hub's
store sees every change first, and the person at the board decides what reaches the forge.

When it happens to you:

| The card tried | What to do |
| --- | --- |
| `git push origin <branch>` | Use `git push hub <branch>`. |
| `git pull` or `git fetch origin` | Call `atrium_git_url`, then fetch from the hub. |
| `git push --force hub <branch>` | Not allowed. Push a new branch, or ask the operator. |
| `git push hub +<branch>` or `hub a:b` | A refspec is not allowed. Name the branch alone. |
| `hub` points at another room | Re-point it at this room's forwarder with `git remote set-url hub <url>`. |
| the remote is right and it is still refused | Check that `daemon.json` exists and its `agent` address matches the url. |

## Where next

- [Rooms and the hub](./rooms.md) covers what the hub and a room each hold.
- [Hooks](./hooks.md) covers the hooks atrium wires and the permission gate.
- [The control MCP server](./control-mcp.md) lists `atrium_git_url` and the other tools a card has.
