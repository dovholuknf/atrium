---
title: Run the agents as their own user
description: Give the agents a standard operating system account of their own, localai, and drive them from yours.
---

# Run the agents as their own user

Atrium recommends that the agents never run as you. Make a standard account called `localai`, not an administrator
and not in any admin group, and run the room as that account. You keep your own account, your own desktop and your
own keys, and you work with the agents through the board in your browser.

You can run everything as yourself, and on a throwaway machine that may be fine. It is not what atrium recommends.

## Why

An agent runs as whoever started it. It holds that account's ssh keys, its git logins, its browser profile, the
tokens its cloud CLIs left behind, and whatever administrator rights the account has.

It can also be talked into things. A README in a repository it cloned, a pull request description, a web page it
was asked to read: all of it is text the model reads, and any of it can carry instructions. The permission gate
does not change that. The gate stops mistakes and keeps a record. It does not contain an agent that was talked into
a command that looks like the dozen you already approved.

Run as `localai`, the same command finds `localai`'s home and nothing of yours. The operating system's file
permissions draw that line, not atrium. They were built to keep one user out of another's files, and atrium was not.

## The board is the safe space

Separate accounts usually cost you the agent: its terminal, its files and its prompts are behind a login you are
not using. Atrium puts all of it in your browser, under your own account, so nothing needs to cross the boundary
except what you choose to send.

- **Talk to it.** Every `localai` session is a card with a terminal you type into from the board.
- **Hand it something.** Paste or drag a file onto the terminal and it lands in the card's own directory.
  [Files](./files.md).
- **Take something back.** Browse the card's directory, download a file or a zip, or click a path the agent printed
  to read it in the browser.
- **Answer it.** Its permission requests come to your **perms** tab. [Permissions](./permissions.md).

Your clipboard, your documents and your credentials stay on your side. The agent sees the bytes you gave it.

## Set it up on one machine

Make the account first. It needs an administrator once.

| OS | Command, as an administrator |
| --- | --- |
| Windows | `net user localai /add` |
| macOS | `sudo sysadminctl -addUser localai -password -` |
| Linux | `sudo useradd -m localai` |

Leave it out of Administrators, `admin`, `sudo`, `wheel` and `docker` or `docker-users`. Each of those gives the
account the machine.

Then start the hub as yourself, without a room of its own, and mint a join string for the `localai` room:

```bash
atrium run --no-room
atrium rooms add localai
```

Log in as `localai`, put a copy of `atrium` on its PATH, and join:

```bash
atrium room join <join string>
atrium room
```

Sign each runner in as `localai` once (for Claude Code, run `claude` and log in), and give `localai` the git
identity and the forge login you want the agents to have, and no more. To keep the room running, install the
[start at login](./install.md#start-at-login) service as `localai`.

The `localai` room only reaches what `localai` can reach. To let it work on a folder of yours, share that folder
with a group or an ACL, the same way you would with any other user.

## Its limits

- **The board does not tell local users apart.** The hub and the room listen on loopback, and every account on the
  machine shares loopback. A process running as `localai` can reach the board's address. A separate machine or a
  virtual machine for the room, joined over [an overlay](./overlays.md) or direct mTLS, removes that.
- **The room needs `localai` logged in on Windows and macOS.** Its service starts at that account's logon. Log in
  as `localai` once after a reboot, then switch back to your own account. Switch user leaves its session running.
  On Linux, `ATRIUM_LINGER=1` keeps it running with nobody logged in.

## If you run it as yourself

Then make the first mistake smaller, knowing this contains nothing. Give the agents a browser profile signed in to
nothing, do not forward your ssh agent, keep cloud credentials out of the environment and shell profiles the room
reads, and work in worktrees.

`docs/room-accounts.md` in the repository has the full version: what the provisioning scripts check and warn about,
and the setup for a room on another machine, account by account.
