# Packaging: installing atrium, running it as a service, and what publishing costs

There is no way to install atrium. You build it and copy the binary somewhere, and every machine ends up with a
different somewhere. Three things go stale when that somewhere moves: every hook in `settings.json`, the logon
task, and the binary swap `restart_atrium` performs.

An `atrium install` subcommand was written for this and removed before it shipped. Copying a file to a fixed
path is the shallow half of the problem, and doing it in-tree makes the deep half harder to reach: no version,
no uninstall, no PATH entry, no upgrade, no way to be told an upgrade exists, and a `~/.atrium/bin` convention
invented here that no packaging format would have agreed with.

**Registration belongs in packaging, not in the binary.** Everything below that starts atrium at login is a
script or a package scriptlet. Nothing in `cmd/atrium` knows how to install itself, and nothing should learn.

**What is built, and what is not.** Everything that does not need a certificate, an account or a publishing
decision is written, and most of it has now been run rather than only written.

| | Built | Needs you |
| --- | --- | --- |
| A version the binary reports | yes, `atrium version` | nothing |
| Cross-platform release builds | yes, `scripts/release.sh`, run and verified | nothing |
| deb and rpm, both arches | yes, `scripts/package-linux.sh`, built and unpacked | a Linux box to install on |
| systemd unit, enabled on install | yes, `packaging/postinstall.sh`, read inside the package | a Linux box |
| macOS LaunchAgent | yes, `packaging/atrium.plist` and `scripts/atrium-service.sh` | a Mac to run it on |
| Windows logon task, with verbs | yes, `scripts/atrium-service.ps1`, each verb run twice | a desktop session |
| CI, and a release workflow | yes, `.github/workflows/`, all logic in `scripts/` | nothing until you push |
| Publishing to GitHub Releases | yes, `scripts/publish-release.sh`, dry run verified | a tag, and one command |
| Scoop manifest | written, `packaging/scoop-atrium.json` | a bucket repository, a release |
| An apt and yum repository | not written, costed below | a GPG signing key and somewhere to host it |
| Homebrew | not written | a tap, and Developer ID signing to be pleasant |
| Chocolatey | not written | a code signing certificate, community moderation |
| Microsoft Store | not written | MSIX, package identity, a certificate, and a decision |

---

## Running as a service, on three operating systems

The ask was "run as a service in all the operating systems". Read as intent rather than as a mechanism, that
means: atrium starts by itself, stays running, comes back after a reboot, and can be installed, removed,
started, stopped and questioned with one obvious command. All four of those are now true everywhere. The
mechanism is different on each platform, and it has to be.

### The constraint that decides all three

**Atrium supervises pseudo terminals and spawns claude sessions that need the user's environment.** PATH, shell
configuration, the ssh agent, credential helpers, and the Claude Code configuration in the home directory. A
daemon running as LocalSystem on Windows, or from a system-wide systemd unit, or from a launchd LaunchDaemon,
has none of those, and every runner it started would inherit none of them.

The failure that prevents is specific and quiet: atrium installs, reports itself running, serves the board, and
every session it starts is useless. Nothing errors. Supervision is most of what atrium is for, so a
system-context atrium is atrium with its main feature removed and nothing to say so.

So on every platform atrium runs **as you, in your session**. What that costs is stated per platform below
rather than hidden, because on two of the three it costs something real.

`packaging/atrium.service`, `packaging/atrium.plist` and `scripts/atrium-autostart.ps1` are one design in three
spellings. Change them together.

### Linux: a systemd user unit, enabled for one person by the package

```
sudo dpkg -i atrium_0.4.1_amd64.deb        # or: sudo dnf install ./atrium-0.4.1-1.x86_64.rpm
systemctl --user status atrium
```

The second line is a check rather than a step. The package already did it.

`atrium.service` goes to `/usr/lib/systemd/user/atrium.service`, which is where systemd expects a vendor unit
and where the package manager may own the file. Somebody who wants to change it drops an override in
`~/.config/systemd/user/atrium.service.d/`, which survives every upgrade. Editing the shipped file would be
overwritten by the next one.

**The subtle part is enabling a per-user unit from a postinstall that runs as root.** Three obvious ways to do
it are all wrong, and `packaging/postinstall.sh` names them at the top so nobody reaches for one again:

- `systemctl enable atrium` enables it as a **system** unit. There is no system unit by that name, so this
  either fails or, on a machine that happens to have one, starts the wrong thing.
- `systemctl --user enable atrium` talks to **root's** user manager, which is not the manager anybody wanted,
  and fails with a message about a bus on a machine where root has no session.
- `systemctl --global enable atrium` enables it for **every user on the machine**, now and in future. That is a
  policy decision about somebody's server, taken by a package, and it is not a package's to take.

What is correct is to find the one human who typed the install command and enable it for that human only. The
postinstall does that in this order:

1. **Identify the person.** `SUDO_USER`, then `PKEXEC_UID` for a graphical package manager going through
   polkit, then `DOAS_USER`. A bare `root` answer is not a person: it means a container build, a cloud-init run
   or a configuration manager, and enabling anything there produces a daemon nobody asked for supervising
   sessions nobody is in. In that case the postinstall prints the two commands and stops.
2. **Turn on lingering,** unless `ATRIUM_NO_LINGER=1`. This is what makes the daemon survive logout. It is also
   done first for a mechanical reason: `loginctl enable-linger` starts the user's systemd manager if it is not
   already up, which creates `/run/user/$uid`, and everything after this step talks to that manager over that
   path.
3. **Reach the manager as that user.** `runuser -u "$user" -- env XDG_RUNTIME_DIR=/run/user/$uid
   DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/$uid/bus systemctl --user enable --now atrium.service`. Both
   variables are named explicitly because root has neither of them for another account. `runuser` rather than
   `su -` because it does not run a login shell, so a `.bashrc` that prints something or exits non-zero cannot
   break an install.
4. **Fall back to writing the symlink by hand** when there is no running manager, no bus, or no `runuser`. This
   is not a hack: `~/.config/systemd/user/default.target.wants/atrium.service` pointing at the vendor unit is
   exactly what `systemctl --user enable` creates. What is lost is the `--now`, so it starts at the next login
   instead of this second, and the postinstall says so out loud rather than reporting success.

**This reverses the earlier rule that a package enables nothing.** "Installing says put this here, starting a
daemon is a different sentence" is a good rule and it is wrong for atrium specifically. Atrium's whole job is to
still be running when you come back to it. An install that leaves you to start it by hand leaves you exactly
where this repository started: a daemon somebody ran once by hand months ago, which nothing would bring back,
so `atrium stop` looked like losing everything. Two escape hatches are provided instead: `ATRIUM_NO_ENABLE=1`
installs the files and touches nothing, and `ATRIUM_NO_LINGER=1` enables it but lets it stop at logout.

**Removal has to know the difference between a removal and an upgrade.** `packaging/preremove.sh` runs on both,
and the two packaging systems say which with different words: deb passes `remove` or `upgrade <version>`, rpm
passes `0` for the last copy going away and `1` for one replacing another. Without that check, `apt upgrade
atrium` quietly turns atrium off and the person who ran it finds out at the next reboot.

The trade, stated: **lingering is on by default, which means atrium keeps running after you log out.** That is a
decision about your machine and `ATRIUM_NO_LINGER=1` refuses it. Removal leaves lingering as it found it,
because it may have been on before atrium existed there, and turning off something you did not turn on is how
an uninstall breaks something unrelated.

### macOS: a LaunchAgent, never a LaunchDaemon

```
scripts/atrium-service.sh install
scripts/atrium-service.sh status
```

`packaging/atrium.plist` is a template rather than a finished file, because a plist cannot expand anything.
launchd reads it as data, so every path has to be absolute and already correct, and there is no equivalent of
systemd's `%h`. The install script substitutes five placeholders and writes the result to
`~/Library/LaunchAgents/io.github.dovholuknf.atrium.plist`.

A LaunchAgent belongs to your GUI login session. A LaunchDaemon runs as root before anybody logs in and has
none of your environment, which is the constraint at the top of this section arriving on a third platform.

Three details in that file are each a failure avoided:

- **It starts through a login shell:** `$SHELL -l -c 'exec atrium daemon --db ...'`. A LaunchAgent inherits the
  PATH launchd was configured with, which is `/usr/bin:/bin:/usr/sbin:/sbin` plus `/etc/paths.d`. `claude` is
  usually in none of those. Without this, atrium starts fine and then cannot spawn a single runner, which looks
  like atrium being broken rather than like a PATH. `exec` so launchd supervises atrium and not a shell holding
  it, because otherwise SIGTERM reaches the shell and atrium is killed rather than wound down.
- **`KeepAlive` is `SuccessfulExit: false`, not `true`.** A bare true means `launchctl stop` starts it straight
  back up and `atrium stop` becomes a command that appears to do nothing.
- **`ExitTimeOut` is 30 seconds,** matching the daemon's own grace period. launchd sends SIGTERM, waits, then
  sends SIGKILL. Too short and a stop becomes a kill, which takes every supervised runner down at the moment
  atrium was trying to wind them up tidily.

`launchctl bootstrap` on a label that is already loaded fails with "service already loaded" and changes nothing,
so re-installing after editing the plist would silently keep the old one. The script boots it out first, which
is the supported way to reload and is what makes `install` idempotent.

The trade, stated: **a LaunchAgent goes away when you log out, and macOS has no equivalent of
`enable-linger`.** The nearest thing is a LaunchDaemon, which is the thing this file exists not to be.

### Windows: a logon task, and why not a real service

```
.\scripts\atrium-service.ps1 install
.\scripts\atrium-service.ps1 status
```

This is the platform where the literal reading of "run as a service" produces something that installs, starts,
reports `Running`, and supervises nothing anybody can use. Three options were weighed.

**1. A real Windows service running as the logged-in user.** `sc.exe create atrium binPath=... obj=DOMAIN\user
password=...`.

What it buys is genuinely the strongest form of "stays running" that Windows has: it starts before anybody logs
in, survives logout, restarts through the SCM, and answers `Get-Service`. What it costs is three things, and
each on its own is enough:

- **A stored password.** The account's password goes into the LSA secret store at registration and has to be
  re-entered every time it changes. On a machine signed in with a Microsoft account or joined to Entra there is
  frequently no password to give. A gMSA avoids the password, is domain-only, and is a **different account**,
  which defeats the purpose: the reason to run as the user is to be the user.
- **Session 0.** A service runs in session 0 no matter whose account it uses, and never joins the interactive
  session. The user profile is not loaded unless the service loads it itself, so DPAPI, the credential manager,
  the ssh agent and the per-session PATH are all absent or different. Every claude session it spawned would
  inherit that.
- **It would need code in the binary.** A console program registered as a service is killed by the SCM after
  about thirty seconds for not answering the service control protocol. Making atrium a service means a service
  control handler in `cmd/atrium`, which is a change to atrium rather than to packaging, and this document opens
  by saying registration does not belong in the binary.

**2. A service wrapper, WinSW or NSSM.** Solves only the third bullet: the wrapper answers the SCM and runs
atrium as a child. The stored password and session 0 are unchanged, and it adds a third-party binary that has to
be shipped, versioned and trusted. It turns "atrium supervises nothing usable" into "atrium supervises nothing
usable, with a dependency".

**3. A logon task. This is what ships.** It runs as you, in your session, with your PATH, your profile, your ssh
agent and your Claude Code configuration, so it can open a pseudo terminal you can attach to. No stored
credential, and no elevation to register. It restarts on failure, has no run-time limit, and survives a reboot:
the machine comes up, you log in, atrium starts.

**A real service running as the logged-in user without a stored credential is not achievable,** which is the
question this section was asked to answer. gMSA is the only credential-free option and it is a different
account. So the honest answer is the logon task, and the cost is stated rather than papered over: **it stops
when you log out.** Windows has no `enable-linger`. Linux can buy its way out of the same trade and Windows and
macOS cannot.

`scripts/atrium-service.ps1` wraps `scripts/atrium-autostart.ps1`, which still owns the registration itself.
Two scripts writing the same task is how they drift into registering two daemons on one database, so there is
one place that knows how to write it and one place that knows the verbs.

The verbs are `install`, `uninstall`, `start`, `stop`, `restart`, `status` and `selftest`. Two of them are worth
a note:

- **`stop` calls `atrium stop` before it touches the task.** A kill is not a stop. The daemon owns a pseudo
  terminal per supervised runner and closing one takes the attached process with it, so ending the task's
  process ends every agent at once.
- **`status` asks the daemon, not only the scheduler.** A task reporting `Running` is not the same as atrium
  being up, and with `conhost --headless` in front the process the scheduler reports is conhost. The only honest
  answer comes from the location file and a `/v1/health` call.

---

## Publishing Linux: GitHub Releases, and what the alternatives would cost

**Chosen: GitHub Releases carrying the `.deb` and the `.rpm` as assets.** `scripts/publish-release.sh`
implements it.

It costs nothing that is not already built. `scripts/release.sh` writes the archives and the checksum file,
`scripts/package-linux.sh` writes the packages, and the publish step uploads them. Installing is `curl` then
`dpkg -i` or `dnf install ./atrium.rpm`, which is one line in a README. It is also the substrate under both
alternatives: an apt repository and an OCI artefact would each be built from exactly these files, so choosing
this throws nothing away and changing course later changes only the last step.

What it does not give you is `apt upgrade`. Named as the cost rather than discovered later: you find out about a
new atrium the way you find out about a new anything on GitHub, by looking.

**An apt and yum repository, hosted on GitHub Pages.** This is the one that buys the real thing, `apt upgrade
atrium` and `dnf upgrade atrium`. It costs a **GPG signing key**, and that key is the whole story: generated,
kept somewhere that is not a repository, put into CI as a secret, published somewhere people can decide to trust
it, and rotated at some point by somebody who remembers how. That is the same class of cost as code signing,
which is already what gates Chocolatey and a pleasant Homebrew. It is worth doing when there are users to
upgrade, and there are not yet.

**GHCR as an OCI registry.** Worth being precise about, because the obvious reading of it is wrong. A
**container image of the daemon is close to useless**: atrium opens pseudo terminals and spawns claude sessions
that need the user's PATH, shell configuration, ssh agent and Claude Code configuration, and a container has
none of those. It is the same constraint that makes the systemd unit a user unit, one layer further out. GHCR
can also hold non-container **OCI artefacts**, which is a real and different idea, and `oras pull` of a `.deb`
would work. But nothing on a Linux machine reaches for `oras` to install software, so it buys a distribution
channel with no clients.

The full reasoning is repeated at the top of `scripts/publish-release.sh`, because that is the file that
implements the answer and is where somebody will go to change it.

---

## The five package managers, easiest first

### Scoop, which is nearly free

A JSON manifest in a bucket repository, pointing at a GitHub release asset and its hash. No account, no review,
no signing. `packaging/scoop-atrium.json` is written and carries an `autoupdate` block, which is what makes a
bucket worth having: scoop rewrites the version, URL and hash itself when a new tag appears, so publishing
becomes one commit rather than three edits somebody gets wrong.

**Do this one first,** and not only because it is easy. It exercises the release shape end to end, so if the
archive layout or the checksum file is wrong it is found here rather than four targets later.

### deb and rpm, from one config

`nfpm` emits both from `packaging/nfpm.yaml`. Chosen over `fpm` and hand-rolled `dpkg-deb` because it is one
static Go binary and needs no build host of the target distribution, which is the same property that makes the
cross-compilation free. Both packages are built on Windows and that is not a compromise: nfpm writes the `ar`
and `cpio` archives itself.

`scripts/package-linux.sh` is what to run. It builds both architectures, stages the binary each one needs, and
rewrites the checksum file.

**Three defects were found by building the real packages and unpacking them,** and none of them would have been
found by reading the config. All three are recorded in `packaging/nfpm.yaml` beside the line that had them:

1. **`type: doc` is not an nfpm content type, and an unrecognised type is dropped silently.** The valid types
   are `symlink`, `ghost`, `config`, `config|noreplace`, `dir` and `tree`. Both documentation files carried
   `type: doc`, so the package built, reported success, and shipped without them. It was found by unpacking
   `data.tar.gz` and counting.
2. **nfpm does not expand environment variables in `contents.src`.** It expands them almost everywhere else, so
   `arch: ${ARCH}` and `version: ${VERSION}` both work, and `src: ${BINARY}` goes straight to the globber, which
   reports `glob failed: ${BINARY}: no matching files` and gives no hint that expansion was ever expected. The
   first guess was that the path was wrong. Verified against nfpm v2.43.0 with a two-line config. The binary is
   now copied to one fixed staging path and named in the config as a constant.
3. **`type: config` on `/usr/lib/systemd/user/atrium.service` was wrong.** A conffile is a file the
   administrator is expected to edit, so dpkg stops an upgrade to ask about it. This one lives under `/usr/lib`,
   which is package-owned by policy. The symptom would have been every `apt upgrade` prompting about a file
   nobody had touched.

Two smaller things fixed at the same time. **File modes are stated rather than inherited,** because these
packages are built on Windows where the source file's mode is whatever the filesystem invented, and a binary
that unpacks as 0644 installs cleanly and then cannot be run. And **`package-linux.sh` deletes any package
already in the output directory before it builds.** A run that fails halfway leaves a zero-byte `.deb` behind,
because nfpm creates the target file and then discovers it cannot fill it, and the next run hashes whatever it
finds and puts that empty file in the release. That happened on the first run. The empty-file hash is
`e3b0c44298fc...`, which is worth recognising on sight.

**The package version drops the leading `v`.** dpkg requires an upstream version to begin with a digit, so
`v0.4.1` is rejected by a strict dpkg and mis-sorted by a lenient one, and rpm sorts a leading letter in a way
nobody predicts. The git tag keeps its `v`, because that is what the tag is called and what the release URL
contains. The package version does not.

### Homebrew, where signing starts to matter

A tap of our own to begin with, since core has criteria atrium does not meet. A formula installing the binary
and a `brew services` plist. The plist is already written as `packaging/atrium.plist` and a formula would render
the same template.

Unsigned means macOS quarantines the download and the first run is a right-click-open, which is possible and
unpleasant. Making it pleasant means a Developer ID certificate and notarization, which is an Apple developer
account and a per-release step.

### Chocolatey, where signing is required rather than merely nice

A nuspec, an install script, and moderation on the community feed. It wants a code-signed executable, and that
certificate is the real cost of this target and the next one together. Worth doing only if there is an audience
for it beyond one machine.

### The Microsoft Store, last and possibly never

MSIX, a package identity and a certificate. Also the strictest, and the one with a design consequence rather
than only a cost: **an MSIX-packaged application runs with a virtualized filesystem and registry.** That moves
where `~/.atrium/atrium.db` actually lands, and it moves it whether or not anybody decides it should. The
decision has to be made before packaging rather than discovered after.

And the audience question: atrium is for people who already have a terminal open.

---

## CI, which contains no logic

`.github/workflows/ci.yml` checks out the code, installs Go and node, and calls `scripts/ci.sh`. That is the
whole file. `.github/workflows/release.yml` checks out with full history, then calls `scripts/release.sh`,
`scripts/package-linux.sh` and `scripts/publish-release.sh` in order.

The failure the rule prevents is ordinary and expensive: a check that exists only inside a YAML file can only be
debugged by pushing, and every push to debug it is a five minute round trip to find out you got a quoting wrong.
Everything CI does runs on this machine, today, with `bash scripts/ci.sh`.

Two details in the release workflow are each a bug avoided:

- **`fetch-depth: 0`.** `release.sh` stamps the binary using `git describe --tags --exact-match`, and a shallow
  clone with no tags makes that fail and the version silently become `dev`. A release reporting itself as `dev`
  is one a package manager will never offer an upgrade over.
- **`publish-release.sh` passes `--verify-tag`.** Without it, `gh release create` happily creates the tag for
  you off whatever HEAD happens to be, so a release can be cut from a commit nobody tagged and nobody can find
  again.

`scripts/ci.sh` runs gofmt, `go vet`, a build, the tests, `check-board.sh`, `check-skins.sh`, a parse of every
shell script in `packaging/` and `scripts/`, and `check-powershell.ps1` where pwsh is present. The build passes
`-o build.claude/` even though `./...` writes nothing anybody keeps, because a CI script that the repository's
own hook would block is a script nobody can run locally.

---

## Three things packaging has to get right

### 1. Which binary is running

Every hook in `settings.json` names one, the logon task names one, and `restart_atrium` swaps onto one. The
daemon records its own binary in the location file it already writes, and `claudeconf.HookExe` resolves against
that. This is correct however the binary arrived, which is why packaging does not disturb it.

Both service scripts resolve the binary from PATH first and deliberately never default to the build in this
checkout. A path under `build.claude/` is a moving identity: it changes with the checkout, every `go build`
rewrites it, and on Windows it cannot be written at all while the daemon is running from it, so rebuilding
during a working session fails with a sharing violation that reads like a virus scanner.

### 2. Self-update against a package manager

**This is the unsolved one and it is worth naming before it bites.** `restart_atrium` renames a staged binary
over the running one. Under scoop, choco, a deb or a Homebrew formula, the package manager owns that file, and a
rename behind its back leaves it reporting a version that is not what is installed. The next `scoop update`
would then quietly put the old one back.

The rule to land on is probably: **a packaged atrium refuses the swap and tells you the upgrade command for the
manager that owns it.** That means the build has to know how it was installed, which is one more linker
variable, set by each packaging path.

`docs/reload-design.md` assumes the swap always works, and will need a section when this is decided.

### 3. Where the database lives

`~/.atrium/atrium.db`, keyed off `WORKTREE_ROOT`. Both the systemd unit and the Windows task name it explicitly
rather than letting it be inherited, and that is the whole reason those files are more than one line each.
Which database atrium opens otherwise depends on `WORKTREE_ROOT` in the environment it was started from, and a
unit or a task inherits almost nothing, so leaving it out means the daemon started at login opens a **different**
database from the one you get running it by hand. The symptom is a board that has lost every card.

Packaging is also the moment to decide whether `~/.atrium` is right at all, because MSIX will move it whether or
not anybody decides.

---

## What was proved by running it, and what was not

**Proved on this machine, by running it:**

- `bash scripts/ci.sh` passes end to end: gofmt, vet, build, tests, the board, the skins, and every script
  parsed.
- `scripts/release.sh` builds all five targets and writes one checksum file.
- `scripts/package-linux.sh` builds four packages, two architectures times deb and rpm, using nfpm v2.43.0.
- The `.deb` was unpacked and inspected: `/usr/bin/atrium` at mode 0755, the unit at
  `/usr/lib/systemd/user/atrium.service` at 0644, both documentation files present, an empty `conffiles`, and
  both `postinst` and `prerm` present and executable. The `.rpm` was checked for the same four paths and for the
  scriptlet text.
- `scripts/publish-release.sh` verifies every asset against `checksums.txt` and then refuses to publish, which
  is what it does without `--publish`.
- On Windows, `scripts/atrium-service.ps1` was run for real: `status` against no registration, `install` twice,
  `start`, `status`, `stop` twice, and `uninstall` twice. Each verb is idempotent, exactly one registration
  exists after two installs, and the second uninstall says there is nothing to remove and succeeds. Nothing was
  left behind.

**A defect found that way, which reading would not have caught:** `scripts/atrium-autostart.ps1` had
`[CmdletBinding()]` and a parameter named `$Db`. CmdletBinding adds the common parameters, and `-Debug` carries
the alias `db`, so PowerShell refused to bind **any** invocation of the script, including `-Remove`, with a
message about an alias nobody had written. The script had been written, reviewed and documented and had never
been executed once. `[Parameter(Position = 0)]` on any single parameter does the same thing, because either
attribute turns a script into an advanced function. Both scripts now carry neither, and the position of a
parameter comes from the order it is declared in.

**Not proved, and why:**

- **No package has been installed on a Linux machine.** There is not one here. What that leaves untested is the
  postinstall's live path: the `runuser` call into a running user manager, and `loginctl enable-linger`. The
  fallback path that writes the enable symlink by hand is the one that runs when the live path cannot, so a
  failure there degrades to "starts at next login" rather than to nothing.
- **The systemd unit has never been loaded.** `KillMode=mixed` is the setting to check first: the default kills
  the whole cgroup at stop, which would take every supervised runner down at the moment atrium is trying to wind
  them up tidily.
- **The macOS LaunchAgent has never been loaded,** because there is no Mac here. `scripts/atrium-service.sh`
  parses and its logic is small, but `launchctl bootstrap`, `kickstart` and `bootout` have not been run.
- **The Windows `start` verb could not be proved on this machine.** The interactive console session belongs to a
  different account than the one the tests ran as, and an Interactive-logon task cannot run for an account with
  no interactive session. The registration succeeds, `Start-ScheduledTask` returns, and the task stays `Ready`
  with a last result of `0x41303`, `SCHED_S_TASK_HAS_NOT_RUN`. That is the task scheduler being correct. On a
  normal desktop, where you are the person logged in, this is exactly the case that works, and the selftest
  says so rather than reporting a pass it did not earn.
- **Nothing is signed and nothing has been published.** No release exists, so the URL in
  `packaging/scoop-atrium.json` still points at nothing.
- **No workflow has run.** `.github/workflows/` has never been pushed. Every script it calls has been run by
  hand here, which is the point of the rule that put them in scripts.

---

## Publishing for the first time, in order

There used to be six commands here, and six commands is six chances to run one out of order. There is now one,
and the order lives in `scripts/cut-release.sh` instead of in this document.

```bash
# 1. Say what would happen. This is the DEFAULT: it builds everything, proves
#    everything, writes the scoop manifest, and touches nothing remote.
bash scripts/cut-release.sh v0.1.0

# 2. Do it. The tag is created and pushed, and the release is published.
bash scripts/cut-release.sh v0.1.0 --execute
```

Step 1 is not a preview of step 2, it is the same run without the last stage. It compiles all five platforms,
builds the deb and the rpm, runs the freshly built binary to check `atrium version` reports `v0.1.0` and not
`dev`, rebuilds the commit in a clean export to prove nothing outside it reached the compiler, re-hashes every
artefact against `checksums.txt`, and writes `build.claude/release/v0.1.0/scoop/atrium.json` with the version,
the URL and the hash already filled in. Then it prints the `gh` command it did not run.

`bash scripts/cut-release.sh v0.1.0 --preflight` is the cheaper question: it runs only the refusals and stops
before building anything. Useful before you have decided on a version.

**What it refuses, and none of these can be waived by a flag.** A dirty working tree, because a release nobody
can rebuild is a release with no source. A tag that already exists, because a tag is the only name a release
has. A binary whose stamped version is not the one being released, which is the `dev` failure that makes a
package manager never offer an upgrade. A build the tagged commit does not reproduce. A checksum that does not
match its artefact. `--skip-ci` is the one waiver, because CI is a gate that also runs elsewhere.

Pushing the tag in step 2 fires `.github/workflows/release.yml`, which runs **the same script** with
`--from-tag --skip-ci --execute` on a runner. So step 2 will race the workflow and one of the two will find the
release already exists. Pick one. Prefer the workflow once it has worked at least once, and until then use
`--execute` here where you can see it fail.

Then, and only then, the scoop bucket:

```bash
# In the bucket repository, not this one. The manifest is already filled in:
cp build.claude/release/v0.1.0/scoop/atrium.json <bucket>/bucket/atrium.json
git add bucket/atrium.json && git commit -m "atrium 0.1.0" && git push

# Then, on any Windows machine:
scoop bucket add dovholuknf https://github.com/dovholuknf/scoop-bucket
scoop install atrium
```

That last pair is the real test of the release shape. If the archive layout or the checksum file is wrong, this
is where it shows, and it shows to you rather than to a stranger.
