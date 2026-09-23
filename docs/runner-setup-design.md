# Runner setup: making a runner work where atrium launches it

A runner is a harness row: a command, arguments, an environment. That is enough to start a process. It is not
enough to make the process useful. Every agent CLI keeps its own state about the machine it runs on, and atrium
launches it in places that state has never heard of.

The first case is gemini. Atrium launches it in a fresh worktree, gemini has never seen that folder, and it stops at
its trust prompt or runs in restricted mode. Other runners fail the same way for other reasons: a first-run trust
store, a sign-in that was never done, hooks that were never wired, an MCP server that is not registered.

This page is the shape atrium uses to find and fix those problems, one adapter per runner, and the checklist for
adding the next one.

## What gemini actually does

Measured against **gemini-cli 0.60.0**, installed with npm under the Windows user, by reading the installed bundle
(`packages/core/dist/src/utils/trust.js` inside it) rather than the docs.

- **Trust lives in `~/.gemini/trustedFolders.json`.** A flat JSON object from a path to one of `TRUST_FOLDER`,
  `TRUST_PARENT` or `DO_NOT_TRUST`. `GEMINI_CLI_TRUSTED_FOLDERS_PATH` moves the file. `GEMINI_CLI_HOME` moves the
  whole `~/.gemini` directory.
- **A rule covers its subtree.** `TRUST_FOLDER` on a path trusts that path and everything under it. `TRUST_PARENT`
  on a path trusts the path's parent and everything under that. This is what gemini writes when you answer its
  prompt with "trust the parent folder".
- **The longest matching rule wins.** A `DO_NOT_TRUST` on a worktree beats a `TRUST_FOLDER` on the root above it.
- **Keys are normalised on read.** Gemini lowercases paths on Windows and macOS and resolves symlinks, so
  `D:\Worktrees` and `d:/worktrees` are one rule.
- **An invalid file is fatal.** A trust level gemini does not know, or a file that is not a JSON object, stops
  gemini with `Please fix the configuration file and try again`. Atrium must never write one.
- **Folder trust is on by default.** `security.folderTrust.enabled` in `~/.gemini/settings.json`, default `true`.
  With it off, every folder is trusted.
- **Two environment variables override the file.** `GEMINI_CLI_TRUST_WORKSPACE=true` trusts any folder and
  `=false` distrusts it. `GEMINI_RESTRICTED_MODE=true` distrusts it.
- **Untrusted is not only a prompt.** In an untrusted folder gemini does not read `~/.gemini/.env`, so a key kept
  there is invisible and gemini appears signed out as well.
- **Sign-in is `security.auth.selectedType` in `~/.gemini/settings.json`.** One of `gemini-api-key`,
  `oauth-personal`, `vertex-ai`, `compute-default-credentials` or `cloud-shell`. An API key can come from
  `GEMINI_API_KEY` in the environment, from a `.env` file, or from the OS keychain (service `gemini-cli-api-key`),
  with an encrypted file `~/.gemini/gemini-credentials.json` as the keychain fallback. A Google sign-in lands in
  `~/.gemini/oauth_creds.json`.

## The shape: one adapter per runner

The code lives in `internal/runnersetup`. It is one package with an adapter table, the same choice
`internal/claudeconf/target.go` made for hooks: everything below "which file, which checks" is shared, and two
packages would be two places for the backup and parse rules to drift apart.

An adapter has three parts.

**Detect.** Which harness rows are this runner, and is it installed. A row belongs to an adapter by its command's
leaf name (`gemini`, `gemini.cmd`, `C:/.../gemini.ps1` are all gemini), the same rule the board already uses for
hooks. Installed means the row's binary resolves, exactly the way launching resolves it. The version is read from
the npm package metadata beside the binary, without running the runner, through the same function the update check
uses.

**Check.** A list of named checks. Each one answers with a state and a sentence:

| state | meaning |
| --- | --- |
| `ok` | nothing to do |
| `fail` | this will stop the runner working, and there is a fix |
| `warn` | atrium cannot tell, or the setup works but breaks a rule atrium keeps |
| `n/a` | this check does not apply on this machine or to this runner |

**Fix.** Every failing check carries exactly one of two kinds of fix.

- **apply**: atrium edits the runner's config file itself. It keeps a backup, writes atomically, writes through a
  symlink rather than over it, refuses a file it cannot parse, and never removes or changes an entry it did not
  add. The board shows a `fix` button.
- **explain**: atrium prints the exact command for the human to run, and the board shows it with a copy button.

## What atrium applies and what it only explains

The line is the one `docs/overlays.md` draws for credentials, extended to every decision a runner asks a person to
make.

| check | runner | fix | why |
| --- | --- | --- | --- |
| trusts the workspace | gemini | apply | the operator already chose the folder by making it a provider root |
| trust at launch | gemini | apply, automatic | the operator chose the folder by launching into it |
| a `DO_NOT_TRUST` rule | gemini | explain | somebody said no. atrium never overrides a no |
| trusted-folders file invalid | gemini | explain | gemini refuses it too. only a person can say what was meant |
| signed in | gemini, claude | explain | atrium never holds a credential. it may only name the command |
| key in atrium's harness env | gemini | explain | the key is somewhere atrium promised it would never be |
| hooks wired | claude | apply | already applied this way by the hooks dialog, with the same backup |
| hooks wired | gemini | n/a | atrium has no gemini hooks target yet |

**Sign-in is always explained, never applied.** Atrium does not store an API key or a token, does not pass one
through, and does not read one's value. It checks for the NAME of a variable, or that a file exists, and says which
command signs in. When the key is in the OS keychain atrium cannot see it, so the check says `warn` rather than
claiming the runner is signed out.

**Trust is applied, because the decision was already made.** Gemini asks "do you trust this folder" so that a
person decides before a folder's own configuration is loaded. When atrium is the thing opening the folder, the
person decided when they configured a provider root or launched a card into it. Asking again at a prompt nobody is
watching does not add a decision. It only stops the session.

Atrium keeps two limits on that. It only ever adds `TRUST_FOLDER`, never `TRUST_PARENT` and never
`GEMINI_CLI_TRUST_WORKSPACE=true`, which would trust every folder on the machine including ones atrium never opened.
And it never touches a path that already has a rule, so a `DO_NOT_TRUST` stays a no.

## Trusting ahead of time: the root once, or each worktree at launch

There are two moments to trust a folder, and atrium does both.

**The root, once, from the board.** The workspace roots are the provider roots on this room: each provider's `root`
(`D:/git/github`) and, when worktrees are on, its `worktree_root` (`D:/worktrees`). The `trusts the workspace` check
tests each root against gemini's rules the way gemini does. A root that is not trusted is a `fail` with a `fix`
button that adds one `TRUST_FOLDER` rule for that root. After that every worktree created under it is trusted with
no further writes, and `trustedFolders.json` stays one line longer, not one line per worktree.

This is the recommended fix and the one the board leads with.

**Each worktree, at launch.** When a gemini card launches, before the process starts, atrium asks gemini's question
for the launch folder. When no rule decides it, and the folder is inside a workspace root, atrium adds
`TRUST_FOLDER` for exactly that folder. When a rule already decides it, trusted or not, atrium writes nothing. When
the folder is outside every root atrium writes nothing, and gemini asks as it always has.

This covers a room where nobody has pressed the root fix yet, which is every room on its first day. It never fires
once the root is trusted, because the root rule then decides every folder under it.

The launch write never fails a launch. A write that goes wrong is logged and the runner starts anyway, and gemini's
own prompt is the fallback.

**Where there are no providers,** there are no roots. The check says `warn`, names the setting, and launch-time
trust does nothing. The home directory is not a root for this purpose, even though the directory picker treats it
as one: trusting home would trust everything the operator owns.

## Backups

Every applied fix keeps two copies beside the file it edits.

- `<file>.atrium-original.bak` is the file as it was before atrium first changed it. Written once, never
  overwritten, so the operator can always get back to what they had.
- `<file>.atrium-last.bak` is the file as it was before the most recent change. Overwritten each time.

This differs from the hooks installer, which keeps a timestamped copy per write. Hooks are written a few times in a
machine's life. Launch-time trust writes once per new worktree, and a timestamped copy per worktree would bury
`~/.gemini` in backups nobody reads. Two named files are bounded and still hold the two states worth having.

The claude hooks fix goes through `claudeconf.InstallOnlyTarget` unchanged, so it keeps that installer's own backup.

## The board

Each row on the runners pane that has an adapter carries a `setup` chip beside the hooks chip. It reads `setup ok`,
or `setup: 2 to fix` in the attention color when any check fails. Pressing it opens the setup dialog for that row:

- the binary atrium found and its version, or that it is not installed
- one line per check with its state and sentence
- a `fix` button on each check atrium can apply, which confirms first and names the file it will edit
- the exact command, with a copy button, on each check atrium only explains
- after a fix, where the backup went

A row with no adapter shows nothing new. Rows are per room, so the dialog and its fix go to the room the row came
from, the same way editing a runner does.

## API

- `GET /v1/harnesses` gains a `setup` field on each row with an adapter: `{adapter, installed, exe, version,
  checks[], failing}`. It rides on the existing list for the same reason the model history does: the runners pane
  reads both in one breath, and the hub already merges that list across rooms.
- `POST /v1/harnesses/{id}/setup/fix` with `{"check": "<id>", "target": "<path>"}` applies one fix and answers the
  new report and the backup path. It refuses a check that is explain-only, and a target that is not a workspace
  root, with 409.

## Adapters today

**gemini**

1. `trust`: trusts the workspace. Applies `TRUST_FOLDER` for an untrusted root. Explains a `DO_NOT_TRUST` and an
   invalid file. `ok` when folder trust is off in settings or `GEMINI_CLI_TRUST_WORKSPACE=true` is in the row's env.
2. `auth`: signed in. Reads `selectedType`. For `gemini-api-key`, looks for `GEMINI_API_KEY` in the environment the
   runner inherits, in `~/.gemini/.env` and `~/.env`, and for the encrypted keychain fallback file. Warns when the key
   sits in the harness row's env. Explains `gemini` and `/auth` when nothing is selected.
3. `hooks`: `n/a`. Atrium has no gemini hooks target, so a gemini card reports nothing about itself.
4. Launch: trusts the launch folder as described above.

**claude**

1. `auth`: signed in. `~/.claude/.credentials.json` exists, or `ANTHROPIC_API_KEY` is set. `n/a` on macOS, where the
   credential is in the keychain. Explains `claude` and `/login`.
2. `hooks`: hooks wired. The same report as the hooks dialog, and the same install behind the fix.

Claude's own folder trust (`hasTrustDialogAccepted` in `~/.claude.json`) is not checked. Claude walks up from the
folder with a bound atrium could not confirm from the shipped binary, and that file is rewritten by every running
claude session, so a check could be wrong and a fix could race. It is listed under open questions.

## How to add the next runner

Codex, an ollama-based agent, aider: the steps are the same.

1. **Measure it.** Install the runner, find where it keeps trust, sign-in and hooks by reading its installed code or
   running it against a scratch home, and write what you found at the top of its adapter file with the version you
   measured. Do not trust its docs for file names.
2. **Create `internal/runnersetup/<runner>.go`** with one `Adapter` value: `ID`, `Label`, `Cmds` (the command leaf
   names), and `Package` (its npm package, or empty).
3. **Write each check as a `Check`** with an `ID`, a `Label` and a `Run` function that reads files under `env.Home`
   only, never `os.UserHomeDir()`, so tests can point it at a temp directory.
4. **Decide apply or explain per check** using the table above. Credentials and any "no" a person wrote are always
   explain. Give an apply check an `Apply` function that edits through `writeJSONFile`, which does the backup, the
   symlink and the atomic write.
5. **Add a `Launch` function** only if the runner needs a per-folder step before it starts. It must never fail the
   launch.
6. **Append the adapter to `Adapters`** in `runnersetup.go`.
7. **Test every check against a temp HOME**, one test per state it can answer, and one test that an apply keeps both
   backups and preserves entries it did not add.
8. **Add a scenario to `docs/test-plan.md`** section AA and a line to `CHANGELOG.md`.

Eight steps. The board, the API, the hub merge and the launch hook need no change.

## Open questions

1. **Launch-time trust is on for every gemini row.** The conservative alternative is a per-row switch that starts
   off. It is on because the only way to reach the code is launching into a folder inside a root the operator set
   up, and the prompt it replaces is one nobody is watching. Say if it should be opt-in.
2. **Worktrees trusted at launch are never pruned.** A rule for a deleted worktree matches nothing and costs one line.
   Pruning means atrium deleting entries, which this design never does. Leaving them is the default.
3. **Claude folder trust.** Worth a read-only check once the parent-walk bound is confirmed. Never a fix while
   `~/.claude.json` is shared with live sessions.
4. **Gemini hooks.** Gemini 0.60 has a hooks system in `settings.json`. A gemini hooks target in `claudeconf` would
   turn the `n/a` into a real check with a fix. Not built here.
5. **Codex adapter.** Its hooks check is one line on top of the existing codex target, and its hook trust is an
   explain. Left for the next runner so the checklist gets a real test.
