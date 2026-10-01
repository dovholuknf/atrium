# Review of c4da1774: room-toolchain writes a block into a Windows room's ~/.bash_profile

Reviewer: @review, 2026-10-01. Range 404f6def..c4da1774 on claude/fabric. Two commits:

- 370dcb61 is 54a19bd2 rebased: `git range-diff 1d6a48f4..54a19bd2 404f6def..370dcb61` shows `=`, so its OK
  (0c06e8bb) still holds by patch-id and it is not re-read here.
- c4da1774 is new: scripts/room-toolchain.ps1 and its changelog. Unsigned, noted.

**Verdict: HOLD 370dcb61..c4da1774** on the medium below.

## MEDIUM (proven): a new ~/.bash_profile hides ~/.profile and ~/.bash_login

A login bash reads the FIRST of `~/.bash_profile`, `~/.bash_login` and `~/.profile`, and only that one. The record
payload creates `~/.bash_profile` when it is missing (`$cur = ''`, then `$cur + $blk`), so on a Windows room whose
login setup lives in `~/.profile` or `~/.bash_login`, every session's login bash stops reading it the moment this
runs. Nothing says so: the step reports `done`.

Proven with proof-c4da1774.sh (beside land-review.ps1) on Git for Windows bash 5.2.26 (msys): a HOME holding only
`.profile` with `export FROM_PROFILE=yes` gives `FROM_PROFILE=yes` from `bash -l`. After a `.bash_profile` holding only
the atrium block is added, `FROM_PROFILE` is empty.

sg3 is not hit because Cygwin's skeleton already gave it a `~/.bash_profile`. A room with Git for Windows bash and no
Cygwin (claudevm, sgg, any new Windows room) usually has only `.bashrc` or `.profile`, and it is hit on the first run.

Fix: when `~/.bash_profile` does not exist, write it with the block AND, outside the block, the file bash would have
read: `if [ -f ~/.bash_login ]; then . ~/.bash_login; elif [ -f ~/.profile ]; then . ~/.profile; fi`. Or write the block
into whichever of the three bash reads today, creating `~/.bash_profile` only when none exists. Either way the
`bashhook` probe has to look in the same file.

## LOW: a profile path with a quote breaks the login bash

`$PF` is put inside single quotes in bash (`f=$(cygpath -u '<path>')`). A Windows user name holding `'` (for example
O'Brien, so `C:\Users\O'Brien\.atrium\toolchain\path.txt`) gives an unterminated quote in `~/.bash_profile`, and bash
stops reading the profile at the parse error. Escape `'` as `'\''`, or read the path from `$HOME` in bash instead of
from PowerShell. Not proven, reasoned from the quoting.

## NIT

- The `bashhook` probe matches the comment text `Cygwin points TMP`. The block's own `# >>> atrium toolchain` marker
  is what the replace keys on, so the probe could key on the same marker and a later rewording of the comment will
  not make the probe say "missing" forever.

## Read and fine

- The replace is in place and idempotent: CRLF folded to LF before the match, a non-greedy match between the markers,
  `bashprofile=same` on a second run, as @fabric reported.
- `${d%$'\r'}` survives the PowerShell single-quote doubling and strips a CRLF from path.txt lines.
- `"$LOCALAPPDATA\\Temp"` becomes one backslash inside bash's double quotes, and an empty `LOCALAPPDATA` (an ssh
  login without it) skips the TMP line through `[ -d "$t" ]`.
- `-Check` warns only when the record and room-env.ps1 exist and the bash block does not. The non-Windows path is
  unchanged (`$bashMissing` is false off Windows).

Verdict: HOLD 370dcb61..c4da1774. Re-read 370dcb61..tip, hub-ok and room-ok as asked (provision tooling).

Quality: after the Sonnet switch, the diagnosis of the sg3 failures is careful and the fix was proven there. The miss
is the second-order one again: proven on the one room that already had the file the fix assumes.

## Re-read 370dcb61..0254ee51: OK hub and room

- **Medium closed.** `Bp` returns the first existing of `.bash_profile`, `.bash_login`, `.profile`, the same order a
  login bash reads them, and a new `.bash_profile` only when none exists. The probe and the write both use `Bp`, so
  they look in the same file.
- **Low closed.** `$PF -replace "'", "'\''"` gives the standard close, escaped quote, reopen inside bash single quotes.
- **Nit closed.** The probe keys on `>>> atrium toolchain v2`. The replace regex still matches `# >>> atrium
  toolchain` with any suffix, so sg3's v1 block is replaced in place rather than a second block appended, as @fabric
  saw.
- New, note only: when the block lands in `.profile`, a POSIX `sh -l` reads it too. `$'\r'` is not dash syntax, but it
  does not fail there: the CR is left on, `cygpath` gives nothing, and the line is skipped. No breakage on a Windows
  room.

@fabric's temp-HOME runs (`.profile` only, none, login plus profile, second run `same`, an `o'brien` path) cover the
cases. I read them and did not rerun them.

Verdict: OK hub and room 370dcb61..0254ee51. 0254ee51 is unsigned, noted.
