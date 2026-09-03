# Reaching the board from elsewhere

Atrium listens on loopback and has no login. That is on purpose, and it is written down in two places already:
an authentication layer invented here would be a worse one than the overlays that exist, and reaching the board
from another machine is an overlay's job.

The gap that left is that "use an overlay" was advice rather than a feature. Atrium now drives one.

## What atrium does, and what it does not

Atrium keeps the configuration, opens the listener, and shows what came back. That is all.

- It **never decides who may connect.** That is a policy on the overlay, and it stays there. Atrium can report
  what the network says it may do and never changes it.
- It **never proxies traffic.** The board answers the overlay listener directly, with the same handler the local
  board uses. Nothing goes through an extra hop that would not have gone through it anyway.
- It **never issues an identity.** An identity comes from enrolling against a network somebody else administers.
  Atrium passes a token to the tool that turns it into one, and stores the path.

The listener ends when the daemon does. An address that outlives the board it points at answers with a
connection refused, which reads as the overlay being broken.

Both overlays are embedded SDKs rather than child processes. The one thing atrium still shells out for is
setting an account up in the first place: see "What runs, and what does not" below.

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
| zrok instance | Which zrok to talk to. Shown on the setup block, because enabling talks to whatever it names. |
| share | `private` or `public`. |
| share token | Reuses a private share so its address survives a restart. |
| reserved name | Keeps one address for a public share. The **reserve it** button holds it on your account. |

**Private is the default.** A private share needs zrok on the other end, and the safe default for something with
no login is the one that needs an account. Turning on a public share asks first, and says plainly that whoever
opens the link can read every command and answer permission requests.

A private share prints no URL, because there is nothing to open. What the board shows is the command the other
end runs: `zrok access private <token>`.

### Keeping an address

An address that survives a restart is not one flag. It is two facts for a public share and one mechanism for a
private one, and getting this wrong is silent: everything works until the first stop, and then the link you
handed out is gone.

**`sdk.ShareRequest.Reserved` is read by nothing in zrok.** The field is on the struct and no code consumes it.
Atrium used to set it, which did nothing. Do not set it.

**For a public share**, reserving is a property of the NAME, not the share:

1. The name exists (`zrok2 create name`).
2. The name is marked reserved rather than ephemeral (`zrok2 modify name --reserved`).
3. The share asks for that name when it starts, which is the `NameSelections` atrium sends.

Without step 2 the controller deletes the name when the share is unshared. `cleanupShareNameMappings` in
`controller/unshare.go` is where that happens, and it keeps reserved names and drops the rest. The **reserve it**
button beside the name field does steps 1 and 2, both of them every time, because a name that exists but is
ephemeral fails exactly like one that was never created.

**For a private share**, the token is requested rather than owned. Releasing the share puts it back, so the next
start asks for the same token and gets it as long as nobody took it in between.

Either way the share is released when atrium stops. That is what frees the ziti resources underneath it, and
keeping it alive instead would leave a share nothing answers and an address the next start could not claim.

### What runs, and what does not

Sharing is the embedded SDK. Atrium holds the listener and answers it with the same handler the local board
uses, so there is no child process and nothing is proxied.

The `zrok` executable is used by exactly two things, `enable` and `disable`, which are one-time account
operations. A machine that is already enabled shares with no executable anywhere. The board says so, and
disables only the button that really needs it.

## OpenZiti

| Field | What it is |
| --- | --- |
| identity file | An enrolled identity JSON. Filled in by enrolling, or point it at your own. |
| service | The service this board answers. **what can I host?** asks the network which ones are possible. |

The SDK opens the identity, authenticates, and binds the named service. Atrium answers that listener itself,
so nothing is forwarded and there is no backend to configure. The service has to already exist with a bind
policy this identity satisfies, both of which are administered on the network.

### Asking what an identity may do

A service that does not exist and a service this identity may only dial fail the same way: the listener
refuses. **what can I host?** puts the difference on screen. It authenticates, lists what the controller
returns, and marks each one bindable or dial-only. Bindable ones are clickable, because the next thing anybody
does with that list is type one of those names into the box above it.

Read-only on purpose. Creating services, configs and policies stays out of scope: a network somebody
administers is not one a board should be editing. Reporting what that network already says is the other side of
the same line.

## A share makes loopback stop meaning anything

`atrium stop` is loopback only unless `--shutdown-token` is set. That rule reads the source address of the
request, and it was written when the only way to reach the daemon was to be at this keyboard.

An overlay breaks that. The connection terminates on this machine, so a request that arrived from another
continent presents as `127.0.0.1`. "Only someone at this keyboard" would silently become "anyone the overlay
admits", and a kill switch is the worst thing to hand out by accident.

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
