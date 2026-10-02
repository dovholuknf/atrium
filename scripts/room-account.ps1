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
#            (S-1-5-21-*-512) or Enterprise Admins (S-1-5-21-*-519). Groups are matched by SID, never by name, because
#            the names are localized, and a filtered token (UAC) still lists Administrators, so membership is judged
#            apart from elevation.
#   macOS    uid 0, the group admin or wheel, or `sudo -n -l` working with no password.
#   Linux    uid 0, the group sudo, wheel or admin, or `sudo -n -l` working with no password.
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
$script:AccountWinProbe = @'
try {
    $id = [Security.Principal.WindowsIdentity]::GetCurrent()
    "user=$($id.Name)"
    "host=$env:COMPUTERNAME"
    "elevated=$((New-Object Security.Principal.WindowsPrincipal($id)).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator))"
    "sids=$(($id.Groups | ForEach-Object { $_.Value }) -join ';')"
} catch { "err=$($_.Exception.Message)" }
'@

# sh. `sudo -n -l` only lists, never asks (-n) and never runs anything. The group list is comma separated.
$script:AccountUnixProbe = @'
echo "user=$(id -un)"; echo "uid=$(id -u)"; echo "groups=$(id -Gn | tr ' ' ',')"; echo "host=$(uname -n)"
if command -v sudo >/dev/null 2>&1; then
  o=$(sudo -n -l </dev/null 2>&1); echo "sudo.rc=$?"
  echo "sudo.nopasswd=$(printf '%s\n' "$o" | grep -Ec 'NOPASSWD:[[:space:]]*ALL')"
  echo "sudo.all=$(printf '%s\n' "$o" | grep -Ec '\)[[:space:]]+(NOPASSWD:[[:space:]]*)?ALL[[:space:]]*$')"
else echo "sudo.rc=none"; fi
'@

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
        $member = $sids -contains 'S-1-5-32-544'
        if ($member) { $why += "is in Administrators (S-1-5-32-544)$(if (-not $elev) { ' with a filtered token' })" }
        if ($elev) { $why += 'the token is elevated' }
        if ($sids | Where-Object { $_ -match '^S-1-5-21-\d+-\d+-\d+-512$' }) { $why += 'is in Domain Admins (RID 512)' }
        if ($sids | Where-Object { $_ -match '^S-1-5-21-\d+-\d+-\d+-519$' }) { $why += 'is in Enterprise Admins (RID 519)' }
    } else {
        if ("$($Kv['uid'])" -eq '0') { $why += 'is root (uid 0)' }
        $adminGroups = if ($Kind -eq 'mac') { @('admin', 'wheel') } else { @('sudo', 'wheel', 'admin') }
        foreach ($g in @("$($Kv['groups'])" -split ',' | Where-Object { $_ })) { if ($g -in $adminGroups) { $why += "is in the group $g" } }
        if ("$($Kv['sudo.rc'])" -eq '0') {
            if ([int]"0$($Kv['sudo.nopasswd'])" -gt 0) { $why += 'can run sudo with no password (sudo -n -l says NOPASSWD: ALL)' }
            elseif ([int]"0$($Kv['sudo.all'])" -gt 0) { $why += 'can run sudo with no password prompt (sudo -n -l lists ALL)' }
        }
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
    $kv = @{}
    foreach ($l in @($Out)) { $s = "$l"; $i = $s.IndexOf('='); if ($i -gt 0) { $kv[$s.Substring(0, $i).Trim()] = $s.Substring($i + 1).TrimEnd() } }
    $v = Get-AccountVerdict $kv $Kind $Local $Operators $SameMachine
    if (-not $v.Known) {
        $what = if ($kv['err']) { $kv['err'] } elseif ($Code -ne 0) { "the probe exited $Code" } else { 'the probe said nothing' }
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
