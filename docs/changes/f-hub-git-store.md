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
it.

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

## For the next item (f-new-hub-receive)

- `Hub.Store()` is the one `*gitsync.Store`. `Path(name)` gives the bare directory for a short or full name, `Exists`
  says whether the store holds it, `List()` gives `Entry{Ref, Dir}` and `Root()` the current `git.store`.
- `ParseName` and `ParseURL` give a `Ref{Host, Owner, Repo}`. Use `Ref.Name()` as the lock key with
  `Hub.lock("store:" + lower(name))`, as `Init` does, for every git step on a repo.
- `Store.env()` is the environment a git step on a store repo runs under. `Store.collision(ref)` is the case check to
  call before a push makes a repo.
- `git.create_on_push` is `hubstore.Store.GitCreateOnPush()`. Nothing reads it yet.
- Repos are made with no hook, no `http.receivepack` and no config of ours. The marker file is `atrium-store`.
- `Store.MainAt` and `BranchView.Room/Card/Released` are where the push log plugs in.
- A hub deploy needs no migration: the two settings are rows in `hub_setting` and read as their defaults when absent.
