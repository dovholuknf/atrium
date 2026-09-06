# Overnight brief, 2026-09-06

What was asked for late on 2026-09-05, written down before any of it is built. This is a working document.
The parts that turn out to be decisions rather than tasks belong in `docs/overlays.md`, `docs/backlog.md` and
`docs/federation-design-v2.md`, and should be moved there as they land.

## 1. The zrok panel, reworked

The current panel lets you change things that cannot take effect, and hides the things that can. Every item
below is that same complaint from a different angle.

### Enabled means locked

**If zrok is enabled, the configuration is read only.** To change any of it you disable first.

This is already true underneath and the panel does not say so. The endpoint refuses to move while an
environment is enabled, because a token issued by one instance sent to another fails as a broken token rather
than as a wrong address. A form that accepts an edit and then reports that refusal has taught you nothing,
twice: once when you typed it and once when it came back.

So the fields go read only with one control, `disable to reconfigure`, and the panel says what disabling costs.

### Whose account, as a toggle

When nothing is enabled yet, the panel offers a real choice, and it can tell which options are available
because it can look:

- **The token already on this machine.** Shown only when `$HOME/.zrok2/environment.json` exists for the user
  the daemon runs as. Nothing to type. Atrium shares from the same account as the `zrok` command.
- **Atrium's own token.** A separate environment, its own account, usually a different instance.

A toggle rather than a checkbox, because these are two states of one decision and not two independent facts.

**Choosing atrium's own makes the account token MANDATORY.** It is a password field, it is required before the
enable button does anything, and it is validated by being used: enabling either works or comes back with what
the instance said. There is no useful offline validation of a token, and pretending otherwise with a regular
expression would reject valid tokens from an instance that formats them differently.

### The instance

Always shown, always overridable, defaulted to the public zrok. The hint says which of those two it currently
is, because "the public service, because nobody said otherwise" and "the instance you configured" look
identical when only the value is shown.

### Public and private are not a dropdown

They are two independent capabilities and **both can be on at once**. Two checkboxes, not one select.

The current single `mode` is a leftover from when a share was one thing. A machine may reasonably want a public
link for the board and a private share for a lent session, or the reverse. The stored config becomes two
booleans and the share paths read whichever applies.

### `the address to keep` is the wrong words

It becomes **"what share name would you like to use to access the dashboard"**. The field never explained what
it was for, and the answer is short: it is the hostname people will type.

## 2. Authentication on the board

**This reverses a documented decision and that is worth stating plainly.** `CLAUDE.md` and `docs/overlays.md`
both say authentication is out of scope: single machine, loopback, and reaching it from elsewhere is an
overlay's job rather than an auth layer invented here.

That held while the board was only ever on loopback or behind a zrok private share. It stops holding the moment
the board has a public address, which is what the reserved name work just made comfortable. A public link with
no login in front of a board that can read files, answer permission prompts and type into terminals is not a
line worth defending on principle.

What is wanted:

- **Username and password**, for the simple case.
- **OIDC**, for the real one. Google, Keycloak, whatever.
- **Reuse the zrok instance's own login if possible.** The deployment at
  `D:/worktrees/github/openziti/zrok/zrok2-openziti2/deploy` already runs an IdP, with `SOCIAL-LOGIN.md`,
  `ZITADEL.md` and `idp-*.sh` beside the compose files, so an OIDC provider is already standing. If atrium can
  be a client of that same IdP, somebody already signed in to the zrok console is one redirect from being
  signed in to the board.

Open questions that have to be answered before this is built, not during:

- **Who is the session for.** Atrium has no users, no accounts and no roles. The smallest thing that works is
  one identity, one session cookie, and an allowlist of subjects from the IdP.
- **What loopback does.** The rule should stay that a request from the machine itself needs no login, or every
  hook, the CLI and the MCP server need credentials.
- **What a lent session does.** Sharing one card currently rests on the address being the whole credential. If
  the board grows a login, a guest either gets an exemption for their one card or has to sign in, and the
  second answer defeats the feature.
- **The redirect URL.** OIDC needs a stable one, which ties this to the reserved share name.

## 3. OpenZiti, tonight

Same shape as zrok: atrium binds a service and serves the board on it, with no tunneler in the middle.

Read only, from `D:/worktrees/github/openziti/zrok/zrok2-openziti2/deploy`, including
`zrok2-openziti2-docker/CREDENTIALS.md`. Nothing in that tree is to be modified.

To produce by morning:

1. **An identity for atrium**, created and enrolled, that can bind a service for the board.
2. **The service and the policies** it needs: bind for atrium, dial for the operator.
3. **A tunneler `.jwt`** the operator can enroll on their own machine, which then reaches the board directly.

The board is hosted by the atrium process itself, which is already how `startZitiNative` works: the SDK hands
back a listener and the board is one handler.

## 4. Hub and rooms

The multi machine idea, named. `atrium room` or `atrium spoke`, undecided.

- Two Linux machines, `cdwsl` and `cdzrok`. Both reachable over ssh with passwordless sudo.
- Atrium has to be built for Linux and copied over, so a cross build plus scp.
- Each runs the new subcommand and dials back to the hub, which is this machine.
- The goal for the morning is one hub with two rooms attached and visible.

`docs/federation-design-v2.md` already holds the shape and its rules: leaves dial out, the forum holds nothing,
and identity stays somebody else's job. Two constraints from it that decide what a demo can honestly show:

- **A pseudo terminal cannot leave the machine that made it.** So a room federates its CARDS, their status and
  their permission requests. It does not federate attach. Anybody expecting to type into a remote runner from
  the hub is going to be disappointed, and it is better to say that now than to demonstrate it.
- **The transport is a decision, not a detail.** The leaf dials out, and over what is open: zrok, ziti, or
  plain TLS. Ziti is the interesting answer and it is the one being stood up tonight anyway.

## What could stop this overnight

- **Permission prompts.** A gated session stops at the first one and waits for a human who is asleep. Either
  the board wide switch goes on or the run stalls at the first `ssh`.
- **The zrok 500.** `POST /share` on `api-v2.zrok.io` still fails for this account, and every probe says it is
  the instance rather than atrium. Anything depending on a zrok share has to go through the new instance.
- **A daemon restart kills supervised runners.** Any overnight work that needs a restart to take effect has to
  reach a stopping point first.
