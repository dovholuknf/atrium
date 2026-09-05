# Packaging: five ways to install atrium, and what each one costs

There is no way to install atrium. You build it and copy the binary somewhere, and every machine ends up with a
different somewhere. Three things go stale when that somewhere moves: every hook in `settings.json`, the logon
task, and the binary swap `restart_atrium` performs.

An `atrium install` subcommand was written for this and removed before it shipped. Copying a file to a fixed
path is the shallow half of the problem, and doing it in-tree makes the deep half harder to reach: no version,
no uninstall, no PATH entry, no upgrade, no way to be told an upgrade exists, and a `~/.atrium/bin` convention
invented here that no packaging format would have agreed with.

**What is built, and what is not.** Everything below that does not need a certificate, an account or a
publishing decision is written and in the repository. Nothing has been published, nothing is signed, and the
manifests point at a release that does not exist yet.

| | Built | Needs you |
| --- | --- | --- |
| A version the binary reports | yes, `atrium version` | nothing |
| Cross-platform release builds | yes, `scripts/release.sh`, run and verified | nothing |
| Scoop manifest | written, `packaging/scoop-atrium.json` | a bucket repository, a release |
| deb and rpm | written, `packaging/nfpm.yaml` | `nfpm` installed, a release |
| systemd user unit | written, `packaging/atrium.service` | nothing |
| Homebrew | not written | a tap, and Developer ID signing to be pleasant |
| Chocolatey | not written | a code signing certificate, community moderation |
| Microsoft Store | not written | MSIX, package identity, a certificate, and a decision |

---

## What has to be true first, and now is

**A version.** `atrium version` reports the tag it was built from, the commit, the board hash and the platform.
Set by the linker rather than by a constant somebody edits, because a hand-edited constant is wrong between the
edit and the tag and wrong again on every branch build. Unstamped builds say `dev`, which is the honest answer
for a binary built from a working tree.

It matters to packaging for a mechanical reason: scoop compares it to decide whether an update exists, deb and
rpm refuse to install one package over another without it, and Homebrew names the bottle with it.

**A release that is the same on every platform.** `scripts/release.sh` builds five targets, archives each in the
shape its ecosystem reads, and writes one `checksums.txt` covering all of them. The checksum file is the part
that matters: a manifest carrying a hash that does not match its archive is the most common packaging failure
there is, and it is only found by a stranger.

```
windows/amd64   linux/amd64   linux/arm64   darwin/arm64   darwin/amd64
```

No 32-bit anything, because a target nobody tests is a target that is broken. `linux/arm64` is there because a
small always-on box is a reasonable home for a daemon that outlives sessions.

**Cross-compilation is free here and that is not an accident.** `modernc.org/sqlite` is pure Go, so there is no
cgo anywhere in atrium, and every target is two environment variables. The script sets `CGO_ENABLED=0`
explicitly rather than relying on the default, so a machine that happens to have a C toolchain cannot quietly
produce a binary that needs one. The Linux build off a Windows machine comes out statically linked, which is
what a deb and an rpm depend on.

---

## The five targets, easiest first

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
cross-compilation free.

**The systemd unit is a USER unit, and this is the decision to read before changing anything.** A system unit
runs outside your login session: it cannot open a pseudo terminal you can attach to, and it has none of your
PATH, shell configuration, ssh agent or credential helpers, so every runner it started would inherit none of
them either. Supervision is most of what atrium is for, so a system unit installs a version of atrium with its
main feature missing and nothing to say so.

That is the same decision `scripts/atrium-autostart.ps1` makes on Windows, where it is a logon task rather than
a Windows service, for exactly the same reason. **They are one design on two platforms and should be changed
together.**

The trade, stated rather than hidden: a user unit stops when you log out unless `loginctl enable-linger` is on.
That is deliberately not done for you, because a daemon that keeps running after you log out is a decision about
your machine.

**Nothing is enabled or started on install.** Installing a package says "put this here". Starting a daemon that
opens two listeners and begins supervising processes is a different sentence. There is also a practical reason:
a postinstall runs as root, and it has no way to know which user's session to enable a user unit for.

### Homebrew, where signing starts to matter

A tap of our own to begin with, since core has criteria atrium does not meet. A formula installing the binary
and a `brew services` plist.

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

## Three things packaging has to get right

### 1. Which binary is running

Every hook in `settings.json` names one, the logon task names one, and `restart_atrium` swaps onto one. The
daemon records its own binary in the location file it already writes, and `claudeconf.HookExe` resolves against
that. This is correct however the binary arrived, which is why packaging does not disturb it.

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

`~/.atrium/atrium.db`, keyed off `WORKTREE_ROOT`. Packaging is the moment to decide whether that is right,
because MSIX will change it whether or not anybody decides, and because a package that installs to a system
location while keeping state in a home directory is making a choice either way.

---

## What is unverified, and it is most of it

- **`packaging/nfpm.yaml` has never been run.** `nfpm` is not installed on the machine it was written on, and
  installing a package to test it is a change to a machine nobody asked to change. It is written from nfpm's
  documented schema and it is the piece most likely to be wrong in a small way.
- **`packaging/atrium.service` has never been loaded.** It is written from systemd's documentation. The
  `KillMode=mixed` choice is the one to check first: the default kills the whole cgroup at stop, which would
  take every supervised runner down at the moment atrium is trying to wind them up tidily.
- **`packaging/scoop-atrium.json` has never been installed,** and the URL it names does not exist, because
  nothing has been released.
- **`scripts/release.sh` HAS been run,** on Windows, producing all five archives and a checksum file. The Linux
  binary was confirmed to be a statically linked ELF. It was not executed on Linux.
