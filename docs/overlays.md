# Reaching the board from elsewhere

Atrium listens on loopback and has no login. That is on purpose, and it is written down in two places already:
an authentication layer invented here would be a worse one than the overlays that exist, and reaching the board
from another machine is an overlay's job.

The gap that left is that "use an overlay" was advice rather than a feature. Atrium now drives one.

**Since 2026-09-06 the PUBLISHED board can ask who you are.** Read the section at the end, "A login, and only
in front of the published board", before assuming the paragraph above still describes everything. The short
version: loopback still has no login, atrium still owns no credentials, and what changed is that a board on a
public address can require an OIDC sign-in.

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

## What has actually been exercised

Worth writing down, because "atrium drives an overlay" is a claim and the two halves of it are not equally
proved.

**zrok is proved end to end.** A private share was started from the board, opened locally with
`zrok access private <token>`, and the board's real card data came back through the tunnel with a 200. Then the
share was released and the token stopped resolving. That is the whole path: daemon, embedded SDK, zrok service,
`zrok access`, HTTP. `docs/test-plan.md` section H has it as a scenario to repeat.

**OpenZiti has not been.** Not because anything is known to be wrong with it, but because this machine has no
enrolled identity, so there has never been anything to bind a service as. Everything up to that point is
covered: the identity states, the token reading, the capability query against a controller, and the pre-flight
that refuses a start with nothing configured. The listener itself is unexercised and this document should stop
saying otherwise the moment somebody enrolls one.

The pre-flight is worth its own line. A start that has not been set up is refused **before** it is attempted,
in atrium's words, naming the next step. Without it the attempt goes ahead, the library fails, and what reaches
the board is zrok's or ziti's own message about a thing that was never configured. Those are accurate and they
answer a different question: they say what broke, not what to do.

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
parsing those would tie atrium to somebody else's column widths. The older `~/.zrok` is recognized too, newest
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

### A daemon that is only looking does not bind anything

A reserved name is one name on one account, so the process that re-binds it on startup has to be the one the
operator is using. `atrium preview` opens a COPY of a database and inherits every share row in it, and the first
version of it restored those shares and swept the ones whose cards had gone, from a process meant to be a
window. It took the operator's name off them.

`Options.Passive` is the answer: a preview does not call `RestoreCardShares` and does not call
`SweepDeadCardShares`. See `docs/preview-design.md`, which also says why two ordinary daemons cannot share a
database, and why an overlay name is one of the reasons.

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
- **the address**, on its own line with a copy button. It comes back as DATA from the SDK rather than being
  matched out of a child process's log lines, which is what an earlier version did and is why this used to warn
  that a share's output is not a documented format. There is no child process now: `internal/daemon/
  overlay_native.go` takes a `net.Listener` from the SDK and serves the board's own handler on it.
- **whatever went wrong**, as a sentence about what to do next. `internal/daemon/overlay_zrok_errors.go` turns
  what zrok returns into an account limit, a revoked token, a name already taken, an unreachable instance or the
  instance's own error, and always appends the original, because the classification is a guess.

## Lending one session rather than publishing the board

A second shape, and the reason it is not an option on the first one: publishing the board hands over every card,
every directory, the settings and the file browser. That is right for reaching your own board from your own
phone and wrong for giving somebody a link.

`share this session` on a card serves a **separate restricted handler** on its own share, rather than the board
with a filter over it. The surface is an allowlist and has to stay one, so an endpoint added later is invisible
to a guest until somebody adds it deliberately: the page and its assets, `/v1/health`, a `GET` on that one card,
its attach socket, its icon. Everything else answers 403, and `internal/daemon/overlay_guest.go` names the ones
refused on purpose so nobody has to work out whether they were forgotten.

Read-only is enforced on the SOCKET rather than by hiding a control, because a guest owns their copy of the
page. Permission prompts stay with the operator whichever mode is chosen.

**A share is the agent's terminal and never the card's shell.** A card can hold two terminals, told apart by
`?kind=shell` on the same attach path the share already allows, so without an explicit refusal a writable guest
could have appended six characters to their one route and got a general purpose command line on this machine.
Lending a session means lending that session: the agent, the conversation, the work. A shell in the card's
directory is the machine, and handing that over is the line at the top of this file. Refused rather than
quietly redirected to the runner, because a guest asking for it is either confused or trying it, and both are
better answered.

This is the shape of hazard the allowlist exists for, and it is worth noticing that the allowlist alone did not
catch it: the route was already allowed, and what changed was what the route could mean. An endpoint that grows
a parameter is a new endpoint.

### The address is reserved, and that is not the same as guessable

This was got wrong once and the reasoning is worth keeping, because it is a plausible mistake to make twice.

The first version refused to reserve a name for a lent session, on the grounds that the board's address is one
you keep while a lent session's address IS the credential: there is no login, so whoever holds the link drives
the terminal. The conclusion drawn was that the address should be fresh every time and die when the share
stops or atrium restarts.

**Unguessable and durable are independent properties.** Reserving decides whether an address survives. It
decides nothing at all about whether anybody can guess it. A name generated once, at random, and held by the
controller is both unguessable and durable, and the first version threw the second one away for nothing.

So a share reserves `atrium-` followed by twelve characters drawn from a thirty two symbol alphabet: sixty
bits, which is not walkable at any rate a public frontend would serve. The alphabet excludes `l`, `o`, `0` and
`1`, because an address gets read down a phone and a confusable character turns an unguessable link into a
support question. The prefix is deliberate and it costs nothing: the entropy is entirely in the suffix, and it
is what makes a leftover share identifiable on an account that also holds shares from other tools.

**The two operations that used to be one.**

- **Unbinding** stops the listener and releases the SHARE, keeping the name and the row that says this card
  should be lent out. A share nothing is answering is a live address returning errors, so it does not stay up.
  This is what a shutdown does.
- **Stopping** releases the share, the name and the row. It is the operator saying this link should not work
  any more, and it is the only path that gives an address up. It cannot be undone: sharing again reserves a
  different name, which whoever you sent the first one to does not have.

Conflating them is what made every share die at the moment the daemon came back, and it presented as the share
inventory being empty for no visible reason.

**A private share is weaker and says so.** It has no name at all, and its token is the address. Deleting the
share puts the token back on the shelf, so the rebind asks for the same one and usually gets it. Nothing holds
it in the meantime and another account may take it, so `usually` is the accurate word. The board is told when a
rebind came back with a different token rather than being allowed to keep showing the old address.

**Orphans are atrium's own, and nothing else's.** A pruned card leaves a name reserved on the account that
nothing will ever ask for again, and nobody can see it: the board draws cards, and the card is what went. The
sweep joins the share table against `task` and releases what is left over. It never reads the account and
deletes what it does not recognise. This machine's zrok account is not atrium's, and a sweep that worked from
the account rather than from its own records would eventually release somebody else's share.

## Adding another one

`OVERLAY_UI` in the board describes the fields and `overlayViews` describes the panel. A third overlay is an
entry in each plus a way to get a `net.Listener`, not a branch through the rendering.

## A login, and only in front of the published board

This reverses part of the rule at the top of this file, and the reversal is narrower than it sounds.

**Why it moved.** The rule held while the board was only ever on loopback or behind a private share. It stopped
holding when a reserved public address made handing out a link comfortable. A public URL with no login, in
front of something that reads files, answers permission prompts and types into terminals, is not a line worth
defending on principle.

**What did not move, and it is the part the rule was protecting.** Atrium owns no credentials. There is no user
table, no password and nothing to hash. Identity is delegated to an OIDC provider, atrium verifies what that
provider signed, and the session cookie proves a completed verification rather than standing in for a password.
A design review flagged the first version of this plan, which included username and password, as contradicting
the settled decision. It was right, and the password half was dropped rather than argued for.

### Where it applies, which is what makes it safe

The published board is a different `net.Listener` served by a different `http.Server`. The guard wraps THAT
handler and nothing else, so the boundary is structural rather than a rule somebody has to remember:

- **Loopback is untouched.** No login, exactly as before.
- **Every hook, the CLI and the MCP server keep working**, because they talk to loopback and were never going
  to carry a credential. `TestTheLocalBoardIsNotWrapped` fails if this stops being true, and it would otherwise
  break quietly: a hook that fails is designed never to fail a session.
- **A lent session keeps its own rule.** It has its own handler and its own allowlist, and the address IS the
  credential there by design. Making a guest sign in would defeat the feature.

### The decisions inside it

- **Empty allow list means nobody**, and the configuration is refused at save time rather than discovered at
  sign-in. The tempting default is "anybody the provider authenticated", which on a provider with open
  registration is the whole internet with an extra step.
- **The id token is verified against the provider's published keys**, not decoded. A token that is merely
  parsed is a claim anybody can write, and skipping the signature turns a login into a form where you type your
  own subject.
- **The cookie is signed and not encrypted.** Nothing in it is secret: a subject is not a credential and an
  expiry is public. What has to be impossible is editing it.
- **An API call is refused rather than redirected.** Bouncing one through a login page produces an HTML
  document where JSON was expected, which reads as a corrupt response rather than as a missing session.
- **A login state is single use**, or a callback can be replayed.

### What is not built

- No PKCE. A confidential client with a secret is what the demo provider offers, and adding PKCE for a public
  client is the next thing if a provider needs it.
- No refresh. A session lasts twelve hours and then you sign in again.
- No roles. Everybody who gets in gets the whole board, which is the same grant a share has always been.
