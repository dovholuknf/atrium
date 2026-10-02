## Test plan

## @LETTER@. A Windows room builds ziti-sdk-c: `room-toolchain.ps1 -Profile c`

`scripts/room-toolchain.ps1 <target> -Profile c` provisions a room for C builds of openziti/ziti-sdk-c: MSYS2 mingw
(gcc, cmake, ninja, openssl, pkgconf), the room's PATH record, git identity and credential access, vcpkg with the
`x64-mingw-static` triplet, a ziti-sdk-c checkout and a `CMakeUserPresets.json` with the `cwdming` preset. It is
opt-in. Without `-Profile c` every call prints and does what it did before. The code is in
`scripts/room-toolchain-c.ps1`, the tests in `scripts/test-room-toolchain-c.ps1`, and the usage is in the header of
`scripts/room-toolchain.ps1`.

### @LETTER@1. Offline, on any machine with pwsh 7

1. `pwsh -NoProfile -File scripts/test-room-toolchain-c.ps1` ends `all N checks pass.`
2. `pwsh -NoProfile -File scripts/check-powershell.ps1` ends `all powershell parses.`
3. `pwsh -NoProfile -File scripts/room-toolchain.ps1 x -PayloadSizes` prints one `payload <act> <size>` line for every
   Windows payload. Every size is under 7800, and the new C acts are under 7400.

**Expected:** all three pass. The tests include a simulated Windows room (a fake ssh runs the payloads under pwsh
against fake directories) and the MSYS2 stage with a pretend remote. They need no network.

### @LETTER@2. `-Check` against sg3 from sg4, read only

1. After clint says MSYS2 is done, from sg4 run (leave out `-Msys2Dir` when MSYS2 is at `C:\msys64` or is already on
   the PATH record):

   ```
   pwsh -File scripts\room-toolchain.ps1 <user>@sg3 -Profile c -Check -Msys2Dir V:\work\tools\msys64
   ```
2. Read the lines. Each of `msys2`, `msys2-acl`, `gcc`, `cmake`, `ninja`, `pkgconf`, `openssl`, `git-identity`,
   `git-credential`, `vcpkg`, `triplet`, `sdk-checkout` and `cmake-preset` is `ok <version> at <path>` or
   `warn MISSING` with what a real run would do. A step that needs a person is `needs-human` and the run ends with a
   block of `room-toolchain needs-human <command>` lines.
3. Check that nothing was written on sg3: no `C:\Users\<user>\vcpkg`, no ziti-sdk-c checkout, no
   `CMakeUserPresets.json`, no change in `git config --global --list`, no change in the ACL of the MSYS2 directory
   (`icacls V:\work\tools\msys64`).

**Expected:** exit code 0 even when a step says `needs-human`, and no file or setting changed. The check does ask
github.com once, with `git ls-remote`, which writes nothing.

### @LETTER@3. The real run, and what a person has to do, in order

1. Run the following, with the identity the room's user should commit as. Add `-CheckRepo <owner>/<repo>` only to
   check a private repository.

   ```
   pwsh -File scripts\room-toolchain.ps1 <user>@sg3 -Profile c -Msys2Dir V:\work\tools\msys64 -GitUserName "<name>" -GitUserEmail <email>
   ```
2. If `msys2-acl` or `pacman` says `needs-human`, an admin runs the `icacls` command it printed (the exact text is
   also in the summary block), then the run is repeated.
3. If `git-credential` says `needs-human`, a person logs in to the room's user once at a console on sg3, not over ssh:
   `git credential-manager github login`. It opens a browser or a device code prompt and asks which GitHub account.
   Nothing is typed into the script. This only applies to a private repo given with `-CheckRepo`, because the public
   repo needs no login.
4. If `git-identity` says `needs-human`, rerun with `-GitUserName` and `-GitUserEmail`, or run the two
   `git config --global` commands it printed as the room's user. The script never invents an identity and never
   copies the machine's.
5. If `runner-path` says `needs-human`, run the same command as the other runner account (`localai`). The PATH record
   is per user, so one run only writes it for the account that ran it.

**Expected:** `done` steps for what was missing, `ok` for what was there, and the run ends `room-toolchain done ok` or
`room-toolchain done needs-human 6` while a human item remains. Exit code 6 is only used when nothing failed. A sha256
mismatch of the MSYS2 archive is exit 4 with nothing unpacked.

### @LETTER@4. A second run changes nothing

1. Repeat the run from @LETTER@3 with the same arguments.

**Expected:** every step is `ok` (or `skip`), no `done`, no pacman run, no clone, `CMakeUserPresets.json` byte for
byte the same, exit 0 once the human items are done. A run that was stopped half way (MSYS2 unpacked and pacman not
run, vcpkg cloned and not bootstrapped) resumes where it stopped.

### @LETTER@5. hot-loop confirms a build

1. As the room's user, in a new shell started after the PATH record was written (a session's login bash reads
   `~/.bash_profile`, a room needs a restart, which this script never does), in the checkout:

   ```
   cmake --preset cwdming
   cmake --build build/cwdming
   ```
2. For the tests and samples, `cmake --preset cwdming-with-tests` and the same build.

**Expected:** vcpkg builds libuv, openssl, zlib, llhttp, libsodium, json-c, stc and protobuf-c for `x64-mingw-static`,
and the SDK builds with gcc. If vcpkg stops while building `pkgconf` (the host dependency) with a message about Visual
Studio or MSVC, the host triplet override is the cause, see the risk below.

### @LETTER@6. What the preset says, and where it was read

The generated file defines three presets and keeps anything already in the file.

- `mingw-vcpkg-base` (hidden) inherits `ci-windows-x64-mingw`, which already brings Ninja, the vcpkg toolchain file
  from `$env{VCPKG_ROOT}`, gcc and g++, `TLSUV_TLSLIB=openssl`, the static link flags and developer mode. It adds
  `VCPKG_TARGET_TRIPLET` and `VCPKG_HOST_TRIPLET` `x64-mingw-static`, `CMAKE_BUILD_TYPE` Debug,
  `PKG_CONFIG_EXECUTABLE` `<msys2>/mingw64/bin/pkg-config.exe`, and the environment `VCPKG_ROOT`, `OPENSSL_ROOT_DIR`
  `<msys2>/mingw64` and `PATH` with `<msys2>/mingw64/bin` first. `VCPKG_BINARY_SOURCES` is added only with
  `-VcpkgBinaryCache <dir>`.
- `cwdming` inherits `mingw-vcpkg-base`, `binaryDir` `${sourceDir}/build/cwdming`.
- `cwdming-with-tests` inherits `cwdming`, `ziti_DEVELOPER_MODE=ON`, `VCPKG_MANIFEST_FEATURES=dev-features`.

Sources: openziti/ziti-sdk-c `CMakePresets.json` (names `ci-windows-x64-mingw`, `vcpkg-win64-mingw-static`,
`flags-windows-mingw`, `ci-win64-mingw`, `ci-build`, `dev-mode`, `vcpkg`, `ninja`), `vcpkg.json`, `BUILD.md` and
`.github/actions/build/action.yml`, read from a full clone on 2026-10-02. The names `mingw-vcpkg-base` and `cwdming`
do not exist upstream (never in any commit or tag). hot-loop's own definitions, from sg4, were used for `cwdming`,
`cwdming-with-tests` and the contents of the base preset, except that theirs does not inherit upstream and sets no
host triplet. All three presets are produced by one function, `Get-CwdmingPresets` in `scripts/room-toolchain-c.ps1`.

**Risk, unproven until a real `cmake --preset cwdming` runs on sg3:** upstream's mingw preset sets
`VCPKG_HOST_TRIPLET=x64-windows` and `vcpkg.json` lists `pkgconf` as a host dependency on Windows, so vcpkg would
build it with MSVC and need Visual Studio. This change overrides the host triplet to `x64-mingw-static` so gcc builds
the host tools. hot-loop's own preset sets no host triplet, so the override is untested anywhere. If it fails, the fix
is one line in `Get-CwdmingPresets`.

### @LETTER@7. NOT proven here (macOS, pwsh 7, no Windows)

These were never run. Each is the first thing to look at if a real run misbehaves.

- Windows PowerShell 5.1 running any of the payloads. The payloads are parsed by the pwsh 7 parser and scanned for
  syntax 5.1 lacks (`??`, ternary, `&&`, `||`, three argument `Join-Path`, `-AsHashtable`), and run under pwsh 7
  against fake directories. 5.1's `ConvertTo-Json` and `ConvertFrom-Json` differences (a `//` comment is refused by
  5.1 and read by 7, the layout of the written JSON) are only reasoned about.
- The MSYS2 self-extractor on Windows (`-o"<dir>" -y`, a `msys64` folder inside), the first start, `pacman -Syuu`
  twice and `pacman -S --needed` with real packages, and how long they take. Each pacman call is capped at 25 minutes.
- `icacls`: its output format as parsed (`ConvertFrom-Icacls` was written from the documented format), `/grant` with
  `(OI)(CI)`, inheritance propagating to a tree of about 100 thousand files, `/remove:g` and `/grant:r`, and refusals.
- Account lookup of `claude` and `localai` (`NTAccount.Translate`) and the writable test.
- Git Credential Manager. It ships in the PortableGit this installs (the 2.56.0 archive holds
  `ucrt64/bin/git-credential-manager.exe` and `gcmcore.dll`, checked by listing the archive), but `git
  credential-manager --version` answering, `credential.helper manager` working, and `github login` were not run.
- `bootstrap-vcpkg.bat -disableMetrics` run through Start-Process, and the vcpkg binary download it does.
- A `cmake --preset cwdming` configure and a build, with gcc from MSYS2 and the vcpkg manifest.
- The clone steps against github.com (the sim uses a fake git), and the ssh keepalive over a long pacman run.
- Drive letters, UNC paths and a path with a space or an apostrophe on a real Windows disk. They are covered as text:
  quoting, argument validation, and the J helper that joins path segments.

### @LETTER@8. Decided

- decided: the `cwdming` presets and the base preset / hot-loop's real definitions via fabric, self-contained, no
  `include` / `mingw-vcpkg-base` is not upstream and the shared file is on sg4 only. The preset inherits upstream
  `ci-windows-x64-mingw` so its gcc and static flags apply, and does not use hot-loop's `VCPKG_INSTALLED_DIR`.
- decided: host triplet `x64-mingw-static` / fabric's call, no Visual Studio on sg3 / unproven, see the risk above.
- decided: pacman set is `mingw-w64-x86_64-toolchain`, `-cmake`, `-ninja`, `-openssl`, `-pkgconf` / fabric's update
  from hot-loop's base preset. `-pkgconf` ships `mingw64/bin/pkg-config.exe` and `pkgconf.exe`. `-openssl` ships
  `libssl.a`, `libcrypto.a` and `include/openssl/ssl.h`. Both lists were read from the packages on
  repo.msys2.org/mingw/mingw64.
- decided: the MSYS2 hash comes from the msys2/msys2-installer GitHub release of the same date, not from
  repo.msys2.org / the brief named a `.sha256` next to the file on repo.msys2.org and there is none (it holds the file
  and a `.sig`). The release carries `msys2-base-x86_64-<date>.sfx.exe.sha256` and its own asset digest, the two must
  agree, and the file fetched from repo.msys2.org was compared byte for byte (same sha256) with the release's. The
  file still comes from repo.msys2.org as the brief said.
- decided: an explicit `-Msys2Dir` is the only place looked at and used, found or not / so a typo installs a new MSYS2
  there instead of silently using another one. Without it: `C:\msys64`, `<Prefix>\msys64`, then any directory two
  above a `mingw64\bin` on the PATH record.
- decided: an MSYS2 that is already there gets `pacman -S --needed` for the gaps only, never `-Syuu` / a
  hand-installed MSYS2 is not ours to upgrade, and `-S` against a stale sync database can fail with 404, in which case
  the tail of pacman's output is printed. A freshly unpacked one gets the first start and `-Syuu` twice.
- decided: `-Check` exits 0 even when a step is `needs-human`, and still prints the summary block / exit 6 means a run
  finished and a human is needed, and a check runs nothing.
- decided: a new status word `needs-human` next to ok, done, skip, warn and fail. `room-check.ps1` reads only the tool
  steps it asks for, so it is unchanged. Not added: a `-Profile` passthrough in `room-check.ps1`, a follow-up, because
  it would also need `room-check` to understand the new steps and exit 6.
- decided: when `-Profile c` is given, `git` is added to `-Tools` / the C steps need Git for Windows and the existing
  git tool installs it. `-Tools git,pwsh` leaves go and node out for a room that only builds C.
- decided: `credential.helper manager` is set in the global config only when neither the global nor the system config
  names `manager` or `manager-core` / Git for Windows normally sets it in its system config, and a second entry would
  be noise.
- decided: `-GitHubOwners` (default `dovholuknf,openziti`) limits which owners `-CheckRepo` may name. The credential
  check of the public repo needs no login, so a private repo is checked only when asked.
- decided: another runner account (`localai`) gets RX if its ACL says it cannot read, and a `runner-path` needs-human
  item with the command to run as that account / the PATH record is per user and the script must not write another
  user's profile.
- decided: the Modify grant for the user running pacman is made only when that user cannot write, and what the user
  had before is put back / so the ACL ends as it began.
- decided: `-Check` reads `CMakeUserPresets.json` with a read only act and runs the merge on this side, because
  staging the preset text on the room would write a file. The merge text is the same text the room runs.
- decided: Windows PowerShell 5.1 reformats a merged `CMakeUserPresets.json`: the same content in a different layout.
  The merge saves with `ConvertTo-Json`, so the indentation and spacing change, `<` and `'` come out as `\u003c` and
  `\u0027`, and any comments in the file are lost. The old file is kept as `CMakeUserPresets.json.atrium-bak`.
  `include`, `buildPresets` and every other key are kept. A file that is not valid JSON, has no version of 2 or more,
  or has a `configurePresets` that is not a list is left alone and is needs-human. A file that needs no change is not
  rewritten at all.
- decided: TLS 1.2 is switched on at the top of `cinstall` only (L1) / it is the only act that downloads under 5.1, and
  `CCommon` had no room left for the line under the 7800 limit. A test fails if another act ever downloads without it.
- decided: the Modify grant is written to `~/.atrium/toolchain/acl-grants.txt` on the room before pacman runs and
  forgotten after the take-back (L2) / an ssh drop or a kill during pacman skips the take-back, and a rerun saw
  "writable" and planned nothing. Only what this script granted is recorded, as `dir|account|what the account had`. A
  rerun reverts it and says so. `-Check` reports it as `warn` and changes nothing. A revert that icacls refuses is
  needs-human with the commands, and the record stays so the next run tries again.
- decided: the ACL take-back restores every explicit entry the account had (L3) / it put back only the first one. All
  of them go back in one icacls call, `/grant:r` for the first and `/grant` for the rest.
- decided: a new file is `version` 4, as hot-loop's is / presets with `environment` and `inherits` need nothing newer.
- decided: macOS and Linux get a report only (`cc`, `gcc`, `cmake`, `ninja`, `git`, the vcpkg directory, the checkout)
  and a run says `skip` for installing / the brief, and a system package manager is the right installer there.
- decided: ssh gets `ServerAliveInterval=30` for `-Profile c` / a pacman or vcpkg step is silent for minutes.
- decided: pwsh is not installed again for this. It is in the default `-Tools` and no C step needs it, the remote work
  runs under Windows PowerShell 5.1 / the brief.

### @LETTER@9. How the tests were checked

The tests were broken on purpose, one change at a time, and each one went red: the merge adding presets that are
already there, the icacls refusal for Everyone and Users, a failure code losing to exit 6, MSYS2 prereleases being
accepted, an icacls deny for the account itself being ignored, curly quotes not being doubled, a payload growing to
12032 encoded characters, `-TestBadHash` not corrupting the hash, the git helper never being set, a non git directory
in the way being replaced, the preset version check removed, `-Check` passing `Dry=0` to vcpkg and the checkout, and
exit 6 never being produced. Three of those first stayed green, so tests were added: the checkout refusal message, the
`-Check` writing nothing on an empty room, and the deny that beats a Users allow. The mutation runner was a throwaway
script and is not in the repo.

The four follow-up lows each have a test and were each broken on purpose: the TLS line removed from `cinstall`, the
grant record not written before the grant, the stale grant not seen on a rerun, `cacls` not reporting the record, the
record kept after the take-back, the restore putting back only the first entry, and the docs sentence of the L4 note
removed. Each went red.

Bugs the tests found while being written: a null `configurePresets` merged as a list with a null first, `Run` always
started in the home directory, the `git credential-manager --version` probe passed one argument for two, the
credential check read `auth=` instead of `auth.N=`, vcpkg's version is a date and not a dotted number, a deny ACE did
not beat a Users allow, and `git status` in `-Check` could refresh the index (now `--no-optional-locks`).

Largest encoded payloads, with every value as long as a real call can make it (the test limit is 7400, the hard one
7800): `probe` 5480, `install` 7136 and `record` 7592 (all three existing, unchanged), `cpresets` 7316, `cinstall` 7220,
`csdk` 6856, `cvcpkg` 6624, `cmsys` 6272, `cstate` 3816, `cacls` 4032. The probe was split in two (`cmsys` and `cacls`)
and the TLS line sits in `cinstall` alone to stay under the limit.
`scripts/test-room-folders.ps1` stops at its first `sh` step in this sandbox, as
`docs/backlog/fabric/f-new-review-87ed8711.md` already says, and is not touched by this change.
