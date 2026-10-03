# Review: r-scm-clone (hub forge stage 2) 5b60c227

Range `fa5bb21b..5b60c227`, one commit on claude/r-scm-clone. It adds:

- `git.scm_root` and `git.credential_helper` as room settings;
- `atrium_git_clone`: the MCP tool, `POST /v1/tasks/{id}/git/clone`, and `gitsync.SCM`;
- the one yes for an operator's clone, as a permission row;
- the `hub`/`atrium-hub` remote through a stopgap `hubremote.go`;
- the origin pushurl guard on clones atrium makes.

It merges onto landing `526337e1` cleanly. The gitsync, daemon and api tests pass at the tip, and so does `go vet`.
`gofmt -l` lists only `internal/daemon/fyi_test.go`, which was already red on main.

Verdict: **HOLD on M1.** M1 is a small fix. The rest is sound.

## What holds up

- **The URL.**
  - `ParseURL` takes only https or `git@host:o/r`, with no user, token, port, query, escape or control character.
  - The clone URL is rebuilt from the checked host, owner and repo (`httpsURL`). It is never what was given.
  - argv ends with `--`.
  - `protocol.allow=never` and `protocol.https.allow=always` are set, plus `GIT_ALLOW_PROTOCOL=https`.
  - There are no submodules (no `--recurse`) and no tags.
- **The path.**
  - `<root>/<host>/<owner>/<repo>` goes through `safepath.Contained`, and is checked again after the parent is made.
  - A non-empty folder that is not a clone is left alone.
  - A failed clone removes only what it made.
  - Each folder has a lock.
- **The environment.** `CleanEnv` strips every `GIT_*`. The runner's hardening prepends `-c credential.helper=`, so
  the operator's helpers are reset before atrium's own is added. That part of "every other helper switched off" is
  true.
- **The failure sentence.** Every git failure answers the fixed sentence, so no URL or stderr reaches the card.
- **The one yes.**
  - An operator's clone is untouched until a permission row on the asking card is approved.
  - A no is a 403. "Asked" is a 200 with `state: asked`.
  - The pushurl guard goes on only after the yes.
- **The hub remote.** An existing `hub` that points elsewhere is left alone, and `atrium-hub` is used instead. Both
  taken is reported, not overwritten. A failure is a note, not a failed clone.

## Medium

### M1: atrium's credential helper is offered to any https host a card names, with no prompt (proven)

`ParseURL` accepts any plain hostname (`hostRe`), so `https://collector.example/o/r` passes. The clone then runs with
`-c credential.helper=<git.credential_helper>`, and that config is not scoped to a URL.

Git asks every unscoped helper for every host, so a server that answers 401 receives whatever the helper gives. The
operator's helper is meant for github.com. A card that has no Bash approval can still call `atrium_git_clone` and
have the room send that credential to a host of its choosing.

Proof with git itself: one helper, set two ways, that prints a password naming the host it was asked for.

```
-c credential.helper=<h>                     host=evil.example -> password=SECRET-FOR-evil.example
-c credential.https://github.com.helper=<h>  host=evil.example -> fatal: could not read Username ... prompts disabled
                                             host=github.com   -> password=SECRET-FOR-github.com
```

The fix is one line: scope it to the host being cloned.

- Build `credential.https://<host>.helper=<h>` from the checked `Ref` (`github.com` for `github`), so the helper is
  asked only for the host in the clone URL, and never for a redirect or a second host.
- Better still, give the setting a host list, `git.credential_hosts`, defaulting to `github.com`, and pass the
  helper only when the clone's host is on it.
- The test: a clone against a stub https 401 on another host must not call the helper. The test runner's `source`
  hook can point at a local https stub, or the helper can be a script that writes a marker file.

## Lows

- **L1: `atrium.clone=made` is read from the clone's own config.** A card can write it into a folder it planted under
  the scm root, and so skip the yes. The yes is a permission step, not a wall against the room's own account, so this
  is a note: say in the doc that the yes guards against accidents and not against a card that means it. The same
  applies to a `.git` file whose `gitdir:` points at another repository.
- **L2: the helper setting is a command.** `!…` runs a shell. It is set through `POST /v1/settings`, which has no
  operator gate today (r-new-sec-auth-guard). Until that lands, consider making it settable only from the CLI or
  the board.
- **L3: one lock per spelled path.** `Foo/bar` and `foo/bar` take different locks for one folder on a case-insensitive
  disk. `originMismatch` lowercases, so the result is right, but two clones can race. Key the lock on the
  lowercased path. This is the same note as f-hub-git-store L2.
- **L4: the hub URL when the agent listens on all interfaces.** `0.0.0.0:7782` gives `http://0.0.0.0:7782/...`.
  That works on mac and Linux but not on Windows (sg4). The stopgap only adds `127.0.0.1` for a bare `:port`. It is
  replaced when r-hub-remote lands; make sure that helper handles the case.

Atrium-Verdict: hold fa5bb21b..5b60c227
Quality: careful work: argv-only git, a rebuilt URL, contained paths, a fixed failure sentence and a real yes. M1
is the one place the room's credential can leave the host it was meant for. m1mini commits are unsigned.
