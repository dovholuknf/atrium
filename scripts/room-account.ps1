# Who a room runs as, and whether that is a good idea. Dot-sourced by provision-room.ps1, room-toolchain.ps1,
# room-check.ps1 and scripts/test-room-account.ps1. It holds functions and two payload texts and runs nothing when
# sourced. The page a person reads about it is docs/room-accounts.md.
#
# The stance: atrium does not recommend running a room as an administrator or as the operator's own everyday account,
# because an agent in the room acts with everything that account holds. This says so, as ONE step line, and never stops
# a run on its own.
#
# WHAT IS LOOKED FOR, on the target and read only, as the account the script's ssh login is (which is the account the
# room runs as, since provisioning and the toolchain run as that login, see "THE ACCOUNT" in provision-room.ps1):
#   Windows  the token is elevated, or the token's groups hold Administrators (S-1-5-32-544), Domain Admins
#            (S-1-5-21-*-512), Enterprise Admins (S-1-5-21-*-519), Backup Operators (S-1-5-32-551) or Hyper-V
#            Administrators (S-1-5-32-578). Groups are matched by SID, never by name, because the names are localized.
#            THE GROUPS ARE READ FROM `whoami /groups /fo csv /nh`, NOT FROM WindowsIdentity.Groups, because under UAC
#            an admin's filtered token carries Administrators as a "deny only" group, and .NET's Groups leaves those
#            out (and IsInRole is false for them), so a filtered admin would read as a clean account. whoami lists them.
#            The attribute column that says "deny only" is localized, so it is not read: a SID in the list is a member,
#            and a member whose token is not elevated is reported as having a filtered token.
#   macOS    uid 0, the group admin or wheel, or `sudo -n -l` working at all without a password.
#   Linux    uid 0, the group sudo, wheel or admin, the groups docker, lxd, incus-admin or libvirt (each is root on the
#            host), or `sudo -n -l` working at all without a password. A rule for one named command is still root when
#            that command takes a shell escape (vim, less, find, tar, systemctl, docker, pip), so it warns as well.
#   all      THE OPERATOR'S OWN ACCOUNT is a HEURISTIC and covers two cases only. (1) The login name is one the
#            operator listed, with -OperatorAccount or the environment variable ATRIUM_OPERATOR_ACCOUNT (a comma
#            separated list of `name`, `DOMAIN\name` or `name@host`). (2) The login name is the account running this
#            script AND the target is this same machine (the host names match, or the target is `local`). A different
#            machine reached under your own name cannot be told from a dedicated account, which is what -OperatorAccount
#            is for. Nothing about the account's files or history is read.
#
# The verdict is a pure function of the facts the probe prints, so it is tested without a machine: Get-AccountVerdict.

# ── what the target is asked ────────────────────────────────────────────────

# Windows PowerShell 5.1. No variables go in, so there is nothing to quote. A failure is an err= line and never a throw.
# The SID column of whoami's csv is the third, and it is the same on every language. An empty list is a failure, since
# every token holds Everyone (S-1-1-0), and it must never read as "no admin group". The one group matched by NAME is
# docker-users (Docker Desktop's, which reaches host files), because its RID differs on every machine. The name is the
# installer's own and is not localized. Only a count is printed.
$script:AccountWinProbe = @'
try {
    $id = [Security.Principal.WindowsIdentity]::GetCurrent()
    $rows = @(whoami /groups /fo csv /nh | ConvertFrom-Csv -Header n, t, s, a)
    $sids = @($rows | ForEach-Object { $_.s } | Where-Object { $_ -like 'S-1-*' })
    if (-not $sids) { throw 'whoami /groups listed no groups' }
    "user=$($id.Name)"
    "host=$env:COMPUTERNAME"
    "elevated=$((New-Object Security.Principal.WindowsPrincipal($id)).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator))"
    "sids=$($sids -join ';')"
    "docker=$(@($rows | Where-Object { $_.n -like '*\docker-users' }).Count)"
} catch { "err=$($_.Exception.Message)" }
'@

# sh. `sudo -n -l` only lists, never asks (-n) and never runs anything. The group list is comma separated. sudo can wait on
# the network (LDAP, SSSD, a host name that does not resolve), so it runs in the background with a watcher that kills it
# after 5 seconds, which reads as sudo.rc=timeout. (macOS has no timeout(1), and a sleep and a kill need nothing.) The
# watcher's output goes nowhere so the capture does not wait for its sleep, and nothing is written to disk. THE 5 SECONDS
# IS USUAL, NOT A GUARANTEE: a sudo that has made itself root (real uid 0) cannot be signalled by the user, so the kill
# does nothing and the capture waits for it. A process group kill would be refused the same way. What bounds the probe
# then is the 45 second cap on the whole call in Invoke-AccountProbe, which reads `could not tell`.
#
# The probe's output is delimited by Invoke-AccountProbe with a marker pair made up on THIS side for each run (see there).
# On Unix the marker arrives on stdin, and the account's login shell starts before `sh -s` does, so ~/.bashrc runs first
# and can read stdin, and a determined account can learn the marker too.
$script:AccountUnixProbe = @'
echo "user=$(id -un)"; echo "uid=$(id -u)"; echo "groups=$(id -Gn | tr ' ' ',')"; echo "host=$(uname -n)"
if command -v sudo >/dev/null 2>&1; then
  o=$( (sudo -n -l </dev/null 2>&1 & p=$!; (sleep 5; kill -9 $p) >/dev/null 2>&1 & w=$!; wait $p; r=$?; kill $w 2>/dev/null; [ $r -eq 137 ] && r=timeout; echo "sudo.rc=$r") )
  printf '%s\n' "$o" | grep '^sudo.rc='
  echo "sudo.nopasswd=$(printf '%s\n' "$o" | grep -Ec 'NOPASSWD:[[:space:]]*ALL')"
  echo "sudo.all=$(printf '%s\n' "$o" | grep -Ec '\)[[:space:]]+(NOPASSWD:[[:space:]]*)?ALL[[:space:]]*$')"
else echo "sudo.rc=none"; fi
'@

# ── running a probe with a time cap ────────────────────────────────────────

# Runs the probe for $Kind (windows or unix) on the target, or here for $IsLocal, and returns Out and Code like each script's
# own Invoke-Remote. It does not use that, because it needs a time cap: an ssh that never answers, or a directory server that
# never replies to `id`, must become the "could not tell" warn and not a hang. The cap is for the whole call, so it must be
# longer than the ConnectTimeout in $SshBase (25). A timeout comes back as an err= line. The probes need no PATH wrapper
# and no remote file, and the Windows one is small enough to go as plain -EncodedCommand, which the test measures.
function Invoke-AccountProbe {
    param([string] $Kind, [string] $Ssh, [string[]] $SshBase = @(), [string] $Target, [bool] $IsLocal = $false, [int] $Seconds = 45)
    $win = $Kind -eq 'windows'
    $stdin = $null
    $mark = 'ATRIUM-ACCT-' + [guid]::NewGuid().ToString('N')
    if ($win) {
        $text = "'$mark-begin'`n" + $script:AccountWinProbe + "`n'$mark-end'"
        $psArgs = @('-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass', '-EncodedCommand', [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($text)))
        if ($IsLocal) { $file = 'powershell.exe'; $argv = $psArgs } else { $file = $Ssh; $argv = @($SshBase) + @($Target, "powershell $($psArgs -join ' ')") }
    } else {
        $stdin = "echo $mark-begin`n" + ($script:AccountUnixProbe -replace "`r", '') + "`necho $mark-end`n#"
        if ($IsLocal) { $file = 'sh'; $argv = @('-s') } else { $file = $Ssh; $argv = @($SshBase) + @($Target, 'sh -s') }
    }
    $psi = New-Object Diagnostics.ProcessStartInfo $file
    foreach ($a in $argv) { $psi.ArgumentList.Add([string]$a) }
    $psi.UseShellExecute = $false
    $psi.RedirectStandardInput = $true; $psi.RedirectStandardOutput = $true; $psi.RedirectStandardError = $true
    try { $p = [Diagnostics.Process]::Start($psi) } catch { return [pscustomobject]@{ Out = @("err=could not run $file ($($_.Exception.Message))"); Code = 0 } }
    $so = $p.StandardOutput.ReadToEndAsync(); $se = $p.StandardError.ReadToEndAsync()
    try { if ($stdin) { $p.StandardInput.Write($stdin) }; $p.StandardInput.Close() } catch { }
    if (-not $p.WaitForExit($Seconds * 1000)) {
        try { $p.Kill($true) } catch { }
        return [pscustomobject]@{ Out = @("err=the probe did not answer in $Seconds seconds"); Code = 0 }
    }
    $p.WaitForExit()
    $lines = @(("$($so.Result)`n$($se.Result)" -split "`r?`n") | Where-Object { $_ -and $_ -notmatch '^#< CLIXML|^<Objs |^</Objs>' })
    Select-ProbeLines $lines $mark $p.ExitCode
}

# Only the text between the marker pair is the probe's. The login shell on the target runs the account's rc files (and an
# EXIT trap in one prints AFTER the probe), and any of that can print `uid=1000` or `groups=staff` to make an admin read
# as clean. The marker is random for each run and sent to the target inside the probe. IT DEFENDS AGAINST NOISE AND A
# NAIVE PROFILE, NOT AGAINST A HOSTILE ACCOUNT: on Windows it is in the command line, which a process of the same account
# can read, and on Unix it is on stdin, which the login shell's rc files (~/.bashrc runs before `sh -s`) can read. Exactly
# one begin and one end, in that order, or the answer is an err= line. No marker at all and a nonzero exit is the plain
# "the probe exited N" (ssh failed).
function Select-ProbeLines {
    param([string[]] $Lines, [string] $Mark, [int] $Code = 0)
    $t = @($Lines | ForEach-Object { "$_".Trim() })
    $b = @(for ($i = 0; $i -lt $t.Count; $i++) { if ($t[$i] -eq "$Mark-begin") { $i } })
    $e = @(for ($i = 0; $i -lt $t.Count; $i++) { if ($t[$i] -eq "$Mark-end") { $i } })
    if (-not $b.Count -and -not $e.Count) {
        if ($Code -ne 0) { return [pscustomobject]@{ Out = @(); Code = $Code } }
        return [pscustomobject]@{ Out = @('err=the probe output had no begin and end marker, so it was noisy or cut off'); Code = 0 }
    }
    if ($b.Count -ne 1 -or $e.Count -ne 1 -or $b[0] -gt $e[0]) {
        return [pscustomobject]@{ Out = @('err=the probe output was tampered with or noisy (the begin and end markers are not there once each, in order)'); Code = 0 }
    }
    $inner = if ($e[0] - $b[0] -gt 1) { @($Lines[($b[0] + 1)..($e[0] - 1)]) } else { @() }
    [pscustomobject]@{ Out = $inner; Code = $Code }
}

# ── the operator's list ─────────────────────────────────────────────────────

# -OperatorAccount and ATRIUM_OPERATOR_ACCOUNT, split on commas and semicolons, with blanks dropped.
function Get-OperatorList {
    param([string[]] $Param = @(), [string] $FromEnv = $env:ATRIUM_OPERATOR_ACCOUNT)
    @(@($Param) + @($FromEnv) | ForEach-Object { "$_" -split '[,;]' } | ForEach-Object { $_.Trim() } | Where-Object { $_ })
}
# One entry: name, DOMAIN\name or name@host. Returns why not, or $null.
function Test-OperatorArg {
    param([string] $a)
    if ($a -notmatch '^([A-Za-z0-9_.$-]+\\)?[A-Za-z0-9_.$-]+(@[A-Za-z0-9_.-]+)?$') { return "bad operator account '$a', it is name, DOMAIN\name or name@host" }
    $null
}

# ── who this side is ────────────────────────────────────────────────────────

function Get-LocalIdentity {
    [pscustomobject]@{ User = [Environment]::UserName; Host = [Environment]::MachineName }
}
function Get-ShortName { param([string] $n) (("$n" -replace '^.*\\', '') -split '\.')[0].ToLowerInvariant() }

# ── the verdict ─────────────────────────────────────────────────────────────

function Join-Reasons {
    param([string[]] $r)
    $r = @($r)
    if ($r.Count -le 1) { return ($r -join '') }
    ($r[0..($r.Count - 2)] -join ', ') + ' and ' + $r[-1]
}

# Facts in, verdict out. $Kv is the probe's key=value lines as a hashtable. $Kind is windows, mac or linux. $Local is
# Get-LocalIdentity. $SameMachine is $true when the target is `local`. Returns User, Reasons (the phrases that make this
# a warn, in the order found), Operator ($true when one of them is the operator rule) and Known ($false when the probe
# said nothing).
function Get-AccountVerdict {
    param($Kv, [string] $Kind, $Local, [string[]] $Operators = @(), [bool] $SameMachine = $false)
    $user = "$($Kv['user'])"
    if (-not $user) { return [pscustomobject]@{ User = ''; Reasons = @(); Operator = $false; Known = $false } }
    $why = @()
    if ($Kind -eq 'windows') {
        $sids = @("$($Kv['sids'])" -split ';' | Where-Object { $_ })
        $elev = "$($Kv['elevated'])" -eq 'True'
        # No list is no answer. Every token holds Everyone, so an empty one is a probe that failed, not a clean account.
        if (-not $sids.Count) { return [pscustomobject]@{ User = $user; Reasons = @(); Operator = $false; Known = $false } }
        $member = $sids -contains 'S-1-5-32-544'
        if ($member) { $why += "is in Administrators (S-1-5-32-544)$(if (-not $elev) { ' with a filtered token' })" }
        if ($elev) { $why += 'the token is elevated' }
        if ($sids | Where-Object { $_ -match '^S-1-5-21-\d+-\d+-\d+-512$' }) { $why += 'is in Domain Admins (RID 512)' }
        if ($sids | Where-Object { $_ -match '^S-1-5-21-\d+-\d+-\d+-519$' }) { $why += 'is in Enterprise Admins (RID 519)' }
        if ($sids -contains 'S-1-5-32-551') { $why += 'is in Backup Operators (S-1-5-32-551, can read every file)' }
        if ($sids -contains 'S-1-5-32-578') { $why += 'is in Hyper-V Administrators (S-1-5-32-578, can mount any disk)' }
        if ([int]"0$($Kv['docker'])" -gt 0) { $why += 'is in the group docker-users (Docker Desktop reaches host files)' }
    } else {
        if ("$($Kv['uid'])" -eq '0') { $why += 'is root (uid 0)' }
        $adminGroups = if ($Kind -eq 'mac') { @('admin', 'wheel') } else { @('sudo', 'wheel', 'admin') }
        # Each of these is root on the host: docker run -v /:/h, an lxd or incus container with the host disk, a libvirt guest.
        $rootGroups = if ($Kind -eq 'linux') { @('docker', 'lxd', 'incus-admin', 'libvirt') } else { @() }
        foreach ($g in @("$($Kv['groups'])" -split ',' | Where-Object { $_ })) {
            if ($g -in $adminGroups) { $why += "is in the group $g" }
            elseif ($g -in $rootGroups) { $why += "is in the group $g (root on this host)" }
        }
        # Any sudo -n -l that works is a warn: it means some rule needs no password, and a rule for one command is still root
        # when the command takes a shell escape. A timeout is a warn too, since it is not known to be clean.
        $src = "$($Kv['sudo.rc'])"
        if ($src -eq '0') {
            if ([int]"0$($Kv['sudo.nopasswd'])" -gt 0) { $why += 'can run any command with sudo and no password (sudo -n -l says NOPASSWD: ALL)' }
            elseif ([int]"0$($Kv['sudo.all'])" -gt 0) { $why += 'can run any command with sudo (sudo -n -l lists ALL)' }
            else { $why += 'can use sudo with no password for some command (sudo -n -l works), and one command is usually a way to root' }
        } elseif ($src -eq 'timeout') { $why += 'may have sudo, since sudo -n -l did not answer in 5 seconds' }
    }
    $bare = Get-ShortName $user
    $hostShort = Get-ShortName "$($Kv['host'])"
    $isOp = $false
    foreach ($o in $Operators) {
        $name, $h = $o -split '@', 2
        $nameMatch = if ($name -match '\\') { $user -ieq $name } else { (($user -replace '^.*\\', '') -ieq $name) }
        if ($nameMatch -and (-not $h -or (Get-ShortName $h) -eq $hostShort)) { $isOp = $true; $why += 'is listed as an operator account (-OperatorAccount or ATRIUM_OPERATOR_ACCOUNT)'; break }
    }
    if (-not $isOp -and $Local -and $bare -and $bare -eq (Get-ShortName $Local.User) -and ($SameMachine -or $hostShort -eq (Get-ShortName $Local.Host))) {
        $isOp = $true; $why += 'is the account that runs this script, on this machine, so it is the operator''s own login'
    }
    [pscustomobject]@{ User = $user; Reasons = $why; Operator = $isOp; Known = $true }
}

# The step for one run: Status (ok, warn or fail), Detail, and Refuse ($true when the caller must stop with its refuse
# code).
# $Out and $Code are the probe's answer. $Where names the target for a probe that did not answer.
function Get-AccountResult {
    param($Out, [int] $Code, [string] $Kind, [string] $Where, $Local, [string[]] $Operators = @(), [bool] $SameMachine = $false,
        [bool] $Accept = $false, [bool] $Require = $false)
    # The FIRST value of a key is the one taken, and the same key said again with ANOTHER value is not an answer: it is
    # `could not tell`, never an ok. (Output that came after the probe, or an echo of its own, must not decide the verdict.)
    $kv = @{}; $dup = $false
    $probeKeys = 'user', 'uid', 'groups', 'host', 'elevated', 'sids', 'sudo.rc', 'sudo.nopasswd', 'sudo.all', 'err'
    foreach ($l in @($Out)) {
        $s = "$l"; $i = $s.IndexOf('=')
        if ($i -le 0) { continue }
        $k = $s.Substring(0, $i).Trim(); $val = $s.Substring($i + 1).TrimEnd()
        if ($kv.ContainsKey($k)) { if ($k -in $probeKeys -and $kv[$k] -cne $val) { $dup = $true } } else { $kv[$k] = $val }
    }
    if ($dup) { $kv = @{ err = 'the probe output was tampered with or noisy (a fact was said twice with two values)' } }
    $v = Get-AccountVerdict $kv $Kind $Local $Operators $SameMachine
    if (-not $v.Known) {
        $what = if ($kv['err']) { $kv['err'] } elseif ($Code -ne 0) { "the probe exited $Code" } elseif ($kv['user']) { 'the probe listed no groups' } else { 'the probe said nothing' }
        if ($Require) { return [pscustomobject]@{ Status = 'fail'; Refuse = $true; Detail = "could not tell who the room runs as on $Where ($what), and -RequireDedicatedAccount will not go on without knowing. see docs/room-accounts.md" } }
        return [pscustomobject]@{ Status = 'warn'; Refuse = $false; Detail = "could not tell who the room runs as on $Where ($what). see docs/room-accounts.md" }
    }
    $isUnix = $Kind -ne 'windows'
    if (-not $v.Reasons.Count) {
        $none = if ($isUnix) { 'not elevated, not in an admin group, no passwordless sudo, not the operator''s account' } else { 'not elevated, not in an admin group, not the operator''s account' }
        return [pscustomobject]@{ Status = 'ok'; Refuse = $false; Detail = "$($v.User): $none" }
    }
    $says = "$($v.User) $(Join-Reasons $v.Reasons)"
    if ($Accept) { return [pscustomobject]@{ Status = 'ok'; Refuse = $false; Detail = "accepted by the operator (-IAcceptRunningAsMe): $says. see docs/room-accounts.md" } }
    if ($Require) { return [pscustomobject]@{ Status = 'fail'; Refuse = $true; Detail = "$says, and -RequireDedicatedAccount does not go on with that. see docs/room-accounts.md" } }
    [pscustomobject]@{ Status = 'warn'; Refuse = $false; Detail = "$says. an agent in this room does whatever it is talked into with that account's rights. see docs/room-accounts.md, and -IAcceptRunningAsMe says you accept it" }
}
