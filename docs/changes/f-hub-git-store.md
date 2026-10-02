## Test plan

## @LETTER@. The hub's own git store

### @LETTER@1. A public repo is seeded once

1. On the hub machine, with the hub running, `atrium rooms git init https://github.com/netfoundry/omnigent`.
2. `atrium rooms git store`.

**Expected:** `github/netfoundry/omnigent  created, seeded  main <sha>`. The list shows the repo with that sha and
`git@hub.atrium:netfoundry/omnigent.git`. The sha is the head of the forge's default branch, even when the forge calls
it `master`, and the hub's copy calls it `main`.

### @LETTER@2. A second init changes nothing

1. Run the same `init` again, then once with `.git` on the end and once as `git@github.com:netfoundry/omnigent.git`.

**Expected:** each says `already there, main left alone`, the sha is the same, and the hub fetched nothing.

### @LETTER@3. A private or missing repo is made empty

1. `atrium rooms git init https://github.com/netfoundry/no-such-private-repo`.

**Expected:** `created, empty` with `does not exist on the forge or is private, so push main to it as the operator.`
The list shows it with `(empty)`.

### @LETTER@4. A network error says to run it again

1. Turn the hub machine's network off and `init` a public repo that is not yet in the store. Turn it back on and run the
   same `init`.

**Expected:** the first leaves an empty repo and says the forge could not be reached, to push `main` or run init again.
The second says `seeded` and the list shows the sha. A third says `already there`.

### @LETTER@5. A URL with a credential is refused

1. `atrium rooms git init https://ghp_abc@github.com/o/r`, then `init 'https://github.com/o/r\nx'`, then `init
   https://github.com/../x`.

**Expected:** each is refused with a sentence that does not repeat the URL, and nothing is made under `git.store`.

### @LETTER@6. Case-only twins are refused

1. `init https://github.com/foo/bar`, then `init https://github.com/Foo/Bar`.

**Expected:** the second says `github/Foo/Bar cannot be made, because github/foo/bar is already in the store and a
disk that ignores case would make them one directory`. Only `foo` exists on disk.

### @LETTER@7. Nothing of the URL is kept

1. Open the new repo's `config` file and its `hooks` directory under `git.store`.

**Expected:** no `url`, no `remote`, no credential and no `receivepack` line, and no hook file. On a hub with git 2.45
or later the repo is reftable, otherwise files.

### @LETTER@8. Settings

1. `atrium rooms git settings`. Then `--create-on-push on`, then `--store D:/gitstore`, then `--store ''`.

**Expected:** the first shows the default `<hub dir>/git` and `off`. Each change shows in the next call, and
`--store ''` puts the default back. A relative path is refused. `PUT /_hub/git/settings` from another
machine answers 403.

### @LETTER@9. The list for the board

1. `curl http://127.0.0.1:7778/_hub/git/repos`, and the same from another machine on the overlay.

**Expected:** `{"repos":[{"host":"github","owner":"netfoundry","repo":"omnigent",
"url":"git@hub.atrium:netfoundry/omnigent.git","path":"/git/hub/github/netfoundry/omnigent.git",
"main":{"sha":"...","at":"..."},"branches":[]}]}`. An empty store is `{"repos":[]}`. No answer holds a path on the
hub's disk.

### @LETTER@10. The existing mirror is untouched

1. On a hub with a `git_repos` entry for atrium, run the steps above, then move `claude/main` in the checkout.

**Expected:** the mirror's HEAD stays on `claude/main`, rooms still sync it, and `atrium rooms git store` does not
list it. `atrium rooms git init https://github.com/dovholuknf/atrium` takes it into the store and changes nothing in
it. Run that init again: it says it is a mirror the hub already serves, and the mirror still has no `main`.

### @LETTER@11. Init is for the operator on the hub machine

1. `POST /_hub/git/init` from another machine, and through a zrok share.

**Expected:** 403, and nothing is made.

## Decisions

- decided: The brief and the design say `atrium hub git init`, but `atrium hub` was removed on purpose and a test pins
  that (`TestTheCollidingNamesLandWhereThePlanSays`). Where do the verbs go? / Under `atrium rooms git`, where the hub's
  git verbs already are: `rooms git init <url>`, `rooms git store` (the list) and `rooms git settings`. The route
  names, `/_hub/git/init` and the rest, are as briefed. / Adding a top level `hub` would break the pinned plan, and
  `rooms git` is already the hub's own git group. If fabric wants the `hub` spelling, it is a one-line alias plus
  changing that pin, and nothing else moves.
- decided: Which URLs does init take? / `https://host/owner/repo` with or without `.git` and a trailing slash, and the
  scp form `git@host:owner/repo.git`. No `http`, `git`, `ssh` or `file` scheme, no port, no query and no escapes. / The
  seed is a plain https fetch and nothing of the URL is kept, so the narrowest shape that covers the forge URLs people
  copy is enough.
- decided: What about the scp form's `git` user, given that a URL with userinfo is refused? / Only the user `git` in the
  scp form is allowed, and any other user or any `user@` in an https URL is refused. / `git@github.com:o/r.git` is the
  form clint's brief lists, and it carries no secret.
- decided: Is the seed fetched over ssh for the scp form? / No, always `https://<host>/<owner>/<repo>.git`. / The hub
  has no credential, so only a public repo can seed, and ssh would need a key the hub does not have.
- decided: What is a valid owner or repo name? / Letters, digits, dot, dash and underscore up to 100, not `.` or `..`,
  not ending in a dot or `.git`, and not a Windows device name such as `NUL` or `con.txt`. A path with more than two
  parts (a gitlab subgroup) is refused. / sg4's NTFS drops a trailing dot and cannot make `NUL`, and the design's names
  are `<host>/<owner>/<repo>`.
- decided: Which hosts, and what is a "full" name? / The host is the URL's hostname lowercased, `github.com` is stored
  as `github`, and a name `github.com/o/r` means `github/o/r`. A name must still pass `ValidName`. / One canonical
  directory per forge, so `github.com` and `github` cannot make two.
- decided: The default `git.store` is `<hub dir>/git`, which is also where the `git_repos` mirrors are. How are they
  told apart? / A marker file `atrium-store` inside the bare repository, written by init. The store lists only marked
  repos. / It keeps the one default the design gives and lists nothing it did not make.
- decided: What does init do with a bare repository that is at the path without the marker, such as a `git_repos`
  mirror? / It takes it into the store by writing the marker alone, fetches nothing, and does not touch HEAD or any
  ref. A directory that is not a bare repository is refused as a conflict and left alone. / The brief says a mirror is
  listed only if the operator inited it, and the mirror's HEAD has to stay on `claude/main` because the mirror resets
  it every pass.
- decided: Is an adopted mirror ever seeded, and how is it told from one the store made? / Never. The marker file says
  `made` or `adopted` (an older marker, `made by ...`, reads as made). Only a repository the store made, with no main,
  is seeded, so a second init of an adopted mirror is a no-op that says it is a mirror the hub already serves. / A
  mirror only has `claude/main`, its main is always empty, and a seed would put a forge main into the repo the link
  serves to rooms (review M1).
- decided: Which locks does init take on an adopted mirror? / The repository's store lock, and also the mirror pass's
  `mirror:<name>` lock whenever the directory is a configured `git_repos` mirror. / `mirrorOne` runs `cleanLocks` in
  the same directory (review M2).
- decided: How are `Foo/x` and `foo/y` kept apart? / One store-wide lock (`store:*`) held across the case check and the
  `MkdirAll` of the owner directory, taken after the repository's own lock. A failed init removes the empty owner and
  host directories it made. / The repository locks differ and both checks could pass before either directory existed
  (review L2).
- decided: How does the scrub handle Windows and symlinks? / It removes the root and the hub directory as given, with
  symlinks resolved, with `\` and `/`, and without regard to case on Windows. / git prints the real path, which can be
  `/private/var` for `/var` or another case of a drive (review L1).
- decided: Is the seed fetched with `transfer.fsckObjects`? / Yes, now. A forge whose history fails git's object
  checks is not stored and the note says so, and says to push `main`, not to rerun. / The objects are untrusted and the
  hub serves them on to rooms (review L3). This reverses my first choice, made because old history can fail fsck, and a
  repo that fails is an empty repo the operator pushes `main` to.
- decided: Where is a leading dash refused? / In `ParseName` and `ParseURL` for the owner and the repo (hosts already
  start with a letter or digit), so `Path`, `Exists`, `Init` and anything the next items build on them refuse it. The
  shared `ValidName` is not changed, since `git_repos` and the room side use it. / Review L4.
- decided: What does a collision check compare? / Every level of the path, the host, the owner and the repo, folded to
  lower case, against what is on disk, and the same name again is the no-op. / `Foo/` and `foo/` are one directory on
  NTFS at any level.
- decided: How does init tell a missing or private repo from a network error? / By git's own words: not found, a
  credential asked for, `terminal prompts disabled` and `does not appear to be a git repository` are "does not exist or
  is private", and everything else is "could not be reached" with git's first line. Only the second says to run init
  again. / The brief asks for the distinction only as far as git tells.
- decided: What does an empty forge repository do? / It makes an empty repo and says to push `main`, as for a private
  one. / There is no default branch to seed.
- decided: What config does the seed run under? / The hub's runner, which strips every `GIT_*` variable and sets no
  prompt, plus `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL` at the null device, `GIT_ASKPASS` empty and
  `GIT_ALLOW_PROTOCOL=https`, with a two minute bound. / The runner leaves the operator's `~/.gitconfig` alone, and a
  global `insteadOf` or credential helper there could redirect the seed or hand out a credential.
- decided: Are `transfer.fsckObjects` and `--depth` used on the seed? / Neither. / Old public repos fail fsck on history
  nobody can change, and a shallow `main` would break the later fast-forward pushes.
- decided: Which git makes a repo reftable? / `git --version` 2.45 or later, and a probe that makes a throwaway reftable
  repo succeed. The probe runs per init and is not cached. / The brief says both, and a per-init probe costs a few
  milliseconds against an init that fetches.
- decided: Are hooks and the template written? / No, `git init --template=` copies nothing, so there is no hooks
  directory. / The next item writes the pre-receive. Sample hooks would only be clutter.
- decided: Who can GET `/_hub/git/settings`? / The operator on the hub machine only, the GET too. / `store` is a path on
  the hub's disk, and the brief says no answer names one. `/_hub/git/repos` is the open one, like `/_hub/growls`.
- decided: Do settings need a running hub? / No, `atrium rooms git settings` writes the hub's own store the way
  `rooms git repos` does, and the hub reads it on every use. The route exists for the board. / Both reach one store and
  one validation.
- decided: What if `git.store` is changed after repos exist? / Nothing is moved, and the list shows only what is under
  the new root. / A move of bare repositories is the operator's, and a setting that moved data would be a surprise.
- decided: What is `main.at`? / The commit time of main's tip, through `Store.MainAt`, one function to swap. / The next
  item replaces it with the last operator push from the push log.
- decided: Is the init a conflict (409) or a refusal (400)? / A bad URL or name is 400, a case collision or something
  else at the path is 409, anything else is 500. A seed that fails is a 200 with an empty repo and a note. / The CLI
  prints the sentence in each case.

## Mutation checks

Each of these 27 changes was made to the code on its own, the tests were run, and a test failed (a build error does not
count, and two first tries that only broke the build were redone). Parsing: the userinfo refusal, the scp user rule, the
control character rule, the trailing dot and `.git` rule. Store: the case collision check, the no-refetch on a second
init, the neutralised global git config, the 2.45 version gate, the reftable probe, the marker in `List`, `master`
stored as `main`, `claude/main` left out of the branches, newest first, the URL path in place of a disk path, the
scrub of hub paths from sentences, a mirror's HEAD left alone when taken in, the rerun sentence for a network error,
and `--template=` (no hooks). Routes: init open to a non-loopback caller, `repos` made loopback only, init accepting
a GET, the init body bound, a conflict answered as 500, and the settings GET opened. Settings: `create_on_push` reading
on by default, and a relative `git.store` accepted.

Review fixes on 06ea9026 (M1, M2, L1 to L4) each have a test, and each was mutation-checked on its own with a test
failing: an adopted mirror seeded on a second init, the marker kind always reading made, the mirror lock never taken,
the mirror lock taken for every repository, symlinks not resolved by the scrub, the scrub's case folding branch never
taken, the backslash form not scrubbed, no store-wide lock around the case check, an empty owner directory left after a
failed init, no `transfer.fsckObjects` on the seed, an fsck failure told as a network error, and a leading dash
allowed. One more, the `runtime.GOOS == "windows"` gate on the fold, is equivalent on this macOS machine and is
covered by the `scrubPaths(..., true)` test instead.

## For the next item (f-new-hub-receive)

- `Hub.Store()` is the one `*gitsync.Store`. `Path(name)` gives the bare directory for a short or full name, `Exists`
  says whether the store holds it, `List()` gives `Entry{Ref, Dir}` and `Root()` the current `git.store`.
- `ParseName` and `ParseURL` give a `Ref{Host, Owner, Repo}`. Use `Ref.Name()` as the lock key with
  `Hub.lock("store:" + lower(name))`, as `Init` does, for every git step on a repo.
- `Store.env()` is the environment a git step on a store repo runs under. `Store.collision(ref)` is the case check to
  call before a push makes a repo.
- `git.create_on_push` is `hubstore.Store.GitCreateOnPush()`. Nothing reads it yet.
- API added in the review fix, all additive: `KindMade` and `KindAdopted`, `Store.Adopted(name)` (true for a mirror
  taken into the store, which must never be seeded or have its refs changed by the store), `writeMarker(dir, kind)` and
  `markerKind(dir)` (unexported). A receive step on a repository should also take `mirror:<name>` when
  `Store.mirrorLock(dir)` is not nil, as `Init` does. `Store.scrub` now resolves symlinks. Segments may not start with
  `-`.
- Repos are made with no hook, no `http.receivepack` and no config of ours. The marker file is `atrium-store`.
- `Store.MainAt` and `BranchView.Room/Card/Released` are where the push log plugs in.
- A hub deploy needs no migration: the two settings are rows in `hub_setting` and read as their defaults when absent.
