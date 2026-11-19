---
title: Get started
description: Install atrium, start it, wire Claude Code in and put your first repository on the board.
---

# Get started

This page takes you from nothing to a card on the board. It is the one first-run page: the README, the repository's
`docs/install.md` and the [Quick start](./quick-start.md) all point here.

## 1. Install

macOS and Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/dovholuknf/atrium/main/install.sh | sh
```

Windows, in PowerShell:

```powershell
irm https://raw.githubusercontent.com/dovholuknf/atrium/main/install.ps1 | iex
```

The script finds the latest release, checks the archive against the release's `checksums.txt`, puts `atrium` in
`~/.atrium/bin`, adds that folder to your PATH and starts `atrium run` in the background. Open a new terminal to pick
up the PATH change. Read a script before you run it: both are short and at the root of the repository.
[Install](./install.md#install-script-options) lists the script's options.

### Or build it from source

You need git and Go 1.26 or newer.

```sh
git clone https://github.com/dovholuknf/atrium
cd atrium
make build
```

Without make, `go build -o build.claude/ ./cmd/atrium` builds the same binary. Copy `build.claude/atrium`
(`atrium.exe` on Windows) to `~/.atrium/bin`, put that folder on your PATH, and always run that copy. Hooks and the
self-restart record the binary's path, and every build rewrites `build.claude/`.

### Behind a proxy or on a locked-down network

Atrium on one machine listens on loopback only and opens no outbound connection of its own, apart from a version
lookup at the npm registry before a launch and the forge features you use. The runner, such as `claude`, reaches its
own API itself. [Network footprint](https://github.com/dovholuknf/atrium/blob/main/docs/security/network-footprint.md)
lists every port and connection. A source build needs GitHub for the clone, the Go module proxy for the modules and,
when your Go is older than the `go 1.26.11` line in `go.mod`, a Go toolchain download:

- `HTTPS_PROXY=http://proxy:port` sends git's and Go's downloads through your proxy.
- `GOPROXY=https://your-mirror` fetches modules from your own mirror instead of `proxy.golang.org`.
- `GOTOOLCHAIN=local` stops the toolchain download, and then the Go you have must be 1.26.11 or newer.

## 2. Start it

The install script already started it. After a source build, or after a reboot, run:

```sh
atrium run
```

Open `http://127.0.0.1:7778`. `atrium run` is the hub, which serves the board. The first time, it also makes this
machine's room and starts it in the background. The room holds the agents and their terminals, and stopping
`atrium run` never stops it.

The room's PATH is the PATH of the terminal that started it, and every agent it launches gets that PATH. So `claude`,
or whichever runner you want, has to be on it. A runner installed after the room started stays invisible until the
room restarts: run `atrium stop`, then `atrium room --detach` from a terminal that finds the runner.

## 3. Wire Claude Code in

```sh
atrium hook install
```

It writes atrium's hooks into Claude Code's `settings.json`, keeps a `settings.json.atrium-<time>.bak` copy of the old
file, and leaves everything else in it alone. On its first start `atrium run` already wires them when that file has
none of atrium's hooks (`--no-hooks` skips that), and then this changes nothing. `atrium hook status` lists what is
wired, and `atrium hook uninstall` takes atrium's entries out again.

From then on every new Claude Code session on this machine gets a card. The ones atrium launches, and any you
`atrium join`, also ask the permission gate before each tool call. Sessions already running keep their old
settings until they restart. When atrium is not running the hooks fail open, and Claude works as if atrium were not
there. The board does the same from the **rooms** tab: the claude runner's **hooks** button.

## 4. Put a repository on the board

```sh
cd ~/src/myrepo
atrium launch
```

That starts the default runner, `claude` when the room finds it on its PATH, in that directory, in a terminal atrium
owns. The card appears on the board, and its terminal opens in the **terminals** tab, where you can type into it from
any browser. `atrium launch --help` lists the flags, such as `--runner`, `--title` and `--prompt`.

To try something without a repository, press the lightning button beside **+ new agent** on the board. That is the
**quick throwaway**: say what you want, and it starts in a new empty folder that is gone when it ends.

Stop atrium with `atrium stop`, never by killing the room. Killing it ends every agent it holds at once.

## After it works

- **Run the agents as their own user.** Atrium recommends a standard account called `localai` for the room and its
  agents, driven from your own account through the board. [Run the agents as their own
  user](./accounts.md#set-it-up-on-one-machine) has the steps: `atrium run --no-room` as you, the room as `localai`.
- **Teach the gate what you trust.** On the **perms** tab, **import rules from claude** turns the allow and deny lists
  you already have into standing rules. The [Quick start](./quick-start.md) covers the gate.
- **Start at login.** `atrium run` installs no logon task. [Install](./install.md#start-at-login) has the line for
  each OS.

## A second machine

The hub listens for rooms on loopback by default. To take a room on another machine, start it with an address that
machine can reach, then name the room and get its join string:

```sh
atrium run --link 0.0.0.0:7779 --link-advertise <hub-host>:7779
atrium rooms add laptop
```

On the other machine, with atrium installed the same way:

```sh
atrium room join atr1_...
atrium room
```

`room join` is the first time, and a plain `atrium room` every time after. The join string is good once and for an
hour. [Rooms and the hub](./rooms.md) has the rest.

## Or let an agent do it

Paste one of these into any coding agent that can run shell commands. They need git and Go 1.26 or newer, because the
prompts build from source.

Try it on this machine, on loopback.
[The full prompt](https://github.com/dovholuknf/atrium/blob/main/docs/try-atrium.md).

```text
Run: curl -fsSL https://raw.githubusercontent.com/dovholuknf/atrium/main/docs/try-atrium.md and read what it returns. Then review it for security problems. Prompt me with questions and then if i like it, do exactly what the prompt block in that file says, in order. Verify each step, and report plainly what worked and what did not.
```

Join this machine to a hub as a room. Get a join string from the hub's operator first.
[The full prompt](https://github.com/dovholuknf/atrium/blob/main/docs/join-atrium.md).

```text
Run: curl -fsSL https://raw.githubusercontent.com/dovholuknf/atrium/main/docs/join-atrium.md and read what it returns. Then review it for security problems. Prompt me with questions and then if i like it, do exactly what the prompt block in that file says, in order. Verify each step, and report plainly what worked and what did not.
```
