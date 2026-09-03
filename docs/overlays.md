# Reaching the board from elsewhere

Atrium listens on loopback and has no login. That is on purpose, and it is written down in two places already:
an authentication layer invented here would be a worse one than the overlays that exist, and reaching the board
from another machine is an overlay's job.

The gap that left is that "use an overlay" was advice rather than a feature. Atrium now drives one.

## What atrium does, and what it does not

Atrium keeps the configuration, starts the process, and shows what it printed. That is all.

- It **never handles an identity.** The path to a ziti identity is passed to the tunneler, which owns the key
  inside it. Atrium does not open the file.
- It **never proxies traffic.** Nothing goes through the daemon that would not have gone through it anyway.
- It **has no opinion about who may connect.** That is a policy on the overlay, and it stays there.

The share is a child process. It ends when the daemon does, because an address that outlives the board it points
at answers with a connection refused, which reads as the overlay being broken.

## Getting set up

Neither tool can share anything until this machine has been given something first, and that step is the one
people are actually stuck on. Atrium shows the state and offers the next thing, in three stages: not installed,
installed but not set up, ready.

**zrok needs an environment.** That comes from an account token, which you get once and reuse on every machine.
Paste it and press enable. Atrium runs `zrok enable <token> --headless`, and the description you give is what
the zrok console lists this machine as.

Whether an environment exists is read off disk, from `~/.zrok2/environment.json`, which is the same file the
zrok CLI itself checks. It is NOT read out of `zrok status`, which prints boxed tables for a person to look at:
parsing those would tie atrium to somebody else's column widths. The older `~/.zrok` is recognised too, newest
first.

The account token inside that file never leaves the daemon. Atrium reports THAT one is present, never what it
is, because the board has no use for a credential.

**OpenZiti needs an identity.** That comes from a one-use enrollment token your network administrator issues.
Paste it and press enroll. Atrium runs `ziti enroll identity --jwt <file> --out <file>` and then points itself at
what came out, so there is no path to copy back.

Two things happen before the token is spent. Its claims are read, so the board can say which network it is for
and refuse an expired one here with a date rather than at a controller. And it is written to a file rather than
passed as an argument, because an argument is visible to anything on this machine that can list processes. The
file is deleted either way.

Atrium never creates a service, writes a policy, or talks to a controller. A network you administer is not one a
board should be editing.

## zrok

| Field | What it is |
| --- | --- |
| share | `private` or `public`. |
| what to publish | The board's own address, `localhost:7778` unless you changed it. |
| share token | From `zrok create share`. Reuses a private share so its address survives a restart. |
| reserved name | From `zrok create name`. Keeps one address for a public share. |
| extra options | Anything atrium has never heard of, split on spaces. |

**Private is the default.** A private share needs zrok on the other end, and the safe default for something with
no login is the one that needs an account. Turning on a public share asks first, and says plainly that whoever
opens the link can read every command and answer permission requests.

Keeping one address is a different flag per mode, and neither exists on the other subcommand, so each is only
sent with the mode it belongs to. `zrok share reserved` is gone in v2 and is not what atrium runs.

`--headless` is always passed. Atrium runs this as a child and reads its output, and without that flag zrok
paints a full-screen interface into a pipe and nothing readable comes back.

A private share prints no URL, because there is nothing to open. It prints the command the other end runs, and
that is what the board shows: `zrok access private <token>`.

## OpenZiti

| Field | What it is |
| --- | --- |
| identity file | An enrolled identity JSON. Filled in by enrolling, or point it at your own. |
| extra options | As above. |

This runs `ziti tunnel host`, which binds every service the identity is allowed to bind and forwards each to
whatever its own configuration says. There is no service or backend field here because that command takes
neither: those live on the network. There were two, and they were removed, because a form that collects
something and then drops it is one you stop trusting.

## A share makes loopback stop meaning anything

`atrium stop` is loopback only unless `--shutdown-token` is set. That rule reads the source address of the
request, and it was written when the only way to reach the daemon was to be at this keyboard.

A tunneler breaks that. It runs on this machine and terminates the connection here, so a request that arrived from
another continent presents as `127.0.0.1`. "Only someone at this keyboard" would silently become "anyone the
overlay admits", and a kill switch is the worst thing to hand out by accident.

So while any share is running, the shutdown endpoint stops trusting a loopback address and requires the token.
Start the daemon with `--shutdown-token` if you want to stop it remotely, or stop the share first.

Two things this does not cover, and both are yours to get right:

- **Never publish the agent listener.** `:7777` carries the permission gate. Anything that can reach it can
  answer a request on an agent's behalf. The default backend is the board on `:7778` for that reason.
- **A public zrok share has no login in front of it.** Whoever has the link can read every command and answer
  permission requests. Private is the default, and turning public on says so before it does it.

## What it shows

- **not installed** when the command is not on the daemon's PATH, with a link to where to get it. Resolved with
  the same lookup starting a runner uses, so what the board reports is what starting a share will find.
- **the address**, on its own line with a copy button, once the share prints one. Matched out of the output
  rather than parsed, because what a share prints is not a documented format.
- **whatever it said**, in full, under the panel. A share that refuses explains itself there and nowhere else.

## Adding another one

`OVERLAY_UI` in the board describes the fields, `overlayViews` describes the panel, and one `*Args` method turns
a config into a command line. A third overlay is an entry in each, not a branch through the rendering.
