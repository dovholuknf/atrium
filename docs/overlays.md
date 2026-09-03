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

## zrok

| Field | What it is |
| --- | --- |
| share | `private` or `public`. |
| what to publish | The board's own address, `localhost:7778` unless you changed it. |
| reserved token | From `zrok reserve`. Keeps the same address across restarts. |
| extra options | Anything atrium has never heard of, split on spaces. |

**Private is the default.** A private share needs zrok on the other end, and the safe default for something with
no login is the one that needs an account. Turning on a public share asks first, and says plainly that whoever
opens the link can read every command and answer permission requests.

A reserved token carries its own mode and its own address, so when one is set the mode field is not sent: the
token is the whole instruction.

`--headless` is always passed. Atrium runs this as a child and reads its output, and without that flag zrok
paints a full-screen interface into a pipe and nothing readable comes back.

## OpenZiti

| Field | What it is |
| --- | --- |
| identity file | An enrolled identity JSON. Passed through as given. |
| service | The service this hosts. It has to already exist, with a bind policy this identity satisfies. |
| what to publish | The board's own address. |
| extra options | As above. |

This runs `ziti tunnel host`. Atrium does not create the service, write a policy, or enrol anything: a network
you administer is not one a board should be editing.

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
