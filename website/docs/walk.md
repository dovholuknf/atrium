---
title: The walk drawer
description: What the walk button in a terminal header does, when it shows, and where its state is kept.
---

# The walk drawer

The **walk** button sits in a terminal's header. It opens a drawer beside the terminal that lets you go through
what an agent produced, one item at a time. The terminal stays where it is, so you can still talk to the agent.

## When the button shows

Atrium decides when you attach to a card. The button shows when any one of these is true:

- The card is the walker of a row in the pulls view.
- The card's directory holds finding files under `findings/`. A finding file is named like
  `01-med-share.go-L165.txt`, a number, a severity (`blocking`, `high`, `critical`, `med`, `medium`, `low` or `nit`),
  a file name and a line.
- The card is on a `claude/*` branch other than `claude/main`, and that branch differs from its base.

Otherwise the button stays hidden. A card with no worktree never shows it.

## What the label says

The label is the count, so you can see the size of the job before you open it.

| Label | Meaning |
| --- | --- |
| `walk PR #N` | The card walks pull request `N` from the pulls view. |
| `walk N findings` | The card has `N` finding files and no pulls row. |
| `walk branch: N files` | There are no findings. The drawer lists the `N` files the branch changed against its base. |

## Walking findings

The **rail** on the left lists every finding with its number, severity and `file:line`. A mark in front of a row
shows its state, and `new` tags a finding that appeared while you were walking. The right side shows one finding:
the code at that line, the comment as it would be posted, the evidence folded away, and a thread with the review
card, where you can ask about the finding and read its answers. The header shows progress, such as
`3 of 14: 2 accepted, 1 posted, 2 dismissed`. The first number counts the findings you have settled, which means
posted or dismissed.

A finding is in one of these states, and a finding with none is still open:

| State | Key | Meaning |
| --- | --- | --- |
| accepted | `y` | It should be raised, and you have not posted it yet. |
| posted | `d` | You commented on it. It asks once for the comment's URL, and Enter skips that. |
| dismissed | `s` | Do not raise it. |
| deferred | `f` | Come back to it later. |

`u` sets a finding back to open.

### Keys

The keys work while the drawer has focus. They do nothing while you type in a box.

| Key | Does |
| --- | --- |
| `j`, `k` | Next and previous item. |
| `g` | Go to the first item that is still open. |
| `y`, `s`, `f`, `d` | Accept, dismiss, defer, mark posted. `p` also marks posted. |
| `u` | Undo, set it back to open. |
| `e` | Edit the comment in place. |
| `c` | Copy the comment. |
| `Enter` | Copy the comment, then open its line on GitHub. |
| `o` | Open the line on GitHub in a tab. |
| `a` | Type `about 03 share.go:165, ` into the terminal and leave the line for you to finish. |
| `A` | Type a short question about the finding into the terminal and submit it. |
| `m` | Open the More menu, which holds the rarer actions. |
| `Esc` | Close the menu, then the drawer. |

Closing the drawer keeps the walk as it is, and a drawer you left open comes back when you reattach. **walk done**
types `walk done` into the walker and submits it. **own tab** opens the walk in a tab of its own.

## Walking a branch

With no findings, the rail lists the files the branch changed, with `+added -removed` beside each. `j` and `k` step
through the files. `n` and `p` jump to the next and previous change, and `[` and `]` to the next and previous agent
comment. You can write a comment of your own on a line. The **Post to** button for the forge copies the comment and
opens that line of the files page, and you paste it there. **share walk** exports your comments so another person can open them.

## Where the walk is kept

- **A findings folder.** Each state is a `Walk:` line under `Evidence` in the finding's own file, for example
  `Walk: accepted 2026-11-12T14:13:00Z`. The agent that wrote the findings reads the same file, and the line survives
  a renumbering. An edit to a comment writes the file back with the hash it read, so a file that changed under you is
  refused. You then see yours next to what is on disk and choose **keep mine** or **take theirs**.
- **A pulls row.** The findings come from `GET /v1/prs/{id}/findings`, an edit is a `PUT` to one finding, and a mark
  is a `POST` to `findings/{key}/walk`. The daemon keeps the marks in `walk.txt` in the run folder. The row in the
  pulls view takes the new counts at once.

The drawer reads the list again when the card has news, when the tab becomes visible, and once a minute. It does not
poll in between.

## Nothing posts to GitHub

The drawer never sends anything to GitHub. Accept, dismiss, defer and posted are notes in a file. **posted** is you
telling atrium that you already commented, and the link is only a link. `c` copies the comment and `o` opens the
line, and you paste it yourself.
