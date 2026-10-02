# Tests for scripts/room-account.ps1 and its three callers. Needs no ssh, no Windows and no network:
#
#   pwsh -NoProfile -File scripts/test-room-account.ps1
#
# What runs here: the verdict over fixtures (the groups, ids and sudo output a Windows, macOS and Linux account give, a filtered
# token, a localized machine, a group that is only NAMED Administrators), the operator rules, the step text for every case, the
# probes themselves (the Unix one for real on this machine, the Windows one parsed, scanned for syntax 5.1 lacks, measured, and
# run under pwsh where it must report an error and not throw), and room-toolchain.ps1 and provision-room.ps1 end to end against
# a fake ssh that answers the account probe with a fixture.
# What it cannot run: Windows PowerShell 5.1, a real token or UAC, a real Windows or Linux target, or `sudo -n -l` on a machine
# that has passwordless sudo. Those answers are fixtures taken from what those systems print. The whoami lists are written in the
# documented format of `whoami /groups /fo csv /nh` (name, type, SID, attributes) and were NOT captured from a Windows machine,
# and the German attribute text is from memory. The probe reads only the SID column, which is the same on every language.
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'room-toolchain-c.ps1')
. (Join-Path $PSScriptRoot 'room-account.ps1')

$script:failed = 0
$script:ran = 0
function Check {
    param([string] $name, $got, $want)
    $script:ran++
    $g = (@($got) | ForEach-Object { "$_" }) -join '|'
    $w = (@($want) | ForEach-Object { "$_" }) -join '|'
    if ($g -ceq $w) { Write-Host "ok   $name" }
    else { $script:failed++; Write-Host "FAIL $name`n     want: $w`n     got:  $g" }
}
function KV { param([string] $text) $h = @{}; foreach ($l in ($text -split "`n")) { $i = $l.IndexOf('='); if ($i -gt 0) { $h[$l.Substring(0, $i).Trim()] = $l.Substring($i + 1).TrimEnd() } }; $h }
$pwsh = (Get-Command pwsh).Source
$unix = $IsWindows -ne $true
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("room-account-test-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Force $tmp | Out-Null

try {

$me = [pscustomobject]@{ User = 'clint'; Host = 'SG4' }
$std = 'S-1-1-0;S-1-5-32-545;S-1-5-14;S-1-5-4;S-1-2-1;S-1-5-11;S-1-5-15;S-1-5-113;S-1-2-0;S-1-5-64-10;S-1-16-8192'
function Win { param([string] $user = 'SG3\claude', [string] $elev = 'False', [string] $sids = $std, [string] $h = 'SG3') KV "user=$user`nhost=$h`nelevated=$elev`nsids=$sids" }
function Why { param($kv, [string] $kind, [string[]] $ops = @(), [bool] $same = $false) (Get-AccountVerdict $kv $kind $me $ops $same).Reasons }

# ── Windows: the token and the groups, by SID ───────────────────────────────

Check 'win: a standard user is clean' (Why (Win) 'windows').Count 0
Check 'win: Administrators and an elevated token' (Why (Win -elev 'True' -sids "$std;S-1-5-32-544") 'windows') @('is in Administrators (S-1-5-32-544)', 'the token is elevated')
# A filtered token (UAC) lists Administrators as "Group used for deny only" in whoami, and elevated is False. This pair is what
# whoami gives. WindowsIdentity.Groups would leave the 544 out (see the whoami section below), which is why it is not the source.
Check 'win: deny-only Administrators in a filtered token is a member, said with the filtered token' (Why (Win -elev 'False' -sids "$std;S-1-5-32-544") 'windows') @('is in Administrators (S-1-5-32-544) with a filtered token')
Check 'win: an elevated token alone is named' (Why (Win -elev 'True') 'windows') @('the token is elevated')
Check 'win: Domain Admins by RID' (Why (Win -user 'CORP\clint' -sids "$std;S-1-5-21-111-222-333-512") 'windows') @('is in Domain Admins (RID 512)')
Check 'win: Enterprise Admins by RID' (Why (Win -user 'CORP\clint' -sids "$std;S-1-5-21-111-222-333-519") 'windows') @('is in Enterprise Admins (RID 519)')
Check 'win: a RID that only ends in 512 is not Domain Admins' (Why (Win -sids "$std;S-1-5-21-111-222-333-5120;S-1-5-21-111-222-333-1512") 'windows').Count 0
Check 'win: a domain group NAMED Administrators has another SID and is not it' (Why (Win -sids "$std;S-1-5-21-111-222-333-1105") 'windows').Count 0
# A German machine: the group is VORDEFINIERT\Administratoren and the name would never match "Administrators". The SID does.
Check 'win: localized machine, matched by SID not by name' (Why (Win -user 'PC1\Hans' -h 'PC1' -elev 'False' -sids "$std;S-1-5-32-544") 'windows') @('is in Administrators (S-1-5-32-544) with a filtered token')
Check 'win: the same SID and a plain sid list order' (Why (Win -sids 'S-1-5-32-544') 'windows') @('is in Administrators (S-1-5-32-544) with a filtered token')
Check 'win: Users S-1-5-32-545 is not Administrators' (Why (Win -sids 'S-1-5-32-545') 'windows').Count 0
Check 'win: no user in the answer is unknown' (Get-AccountVerdict @{} 'windows' $me).Known $false
Check 'win: Backup Operators by SID' (Why (Win -sids "$std;S-1-5-32-551") 'windows') @('is in Backup Operators (S-1-5-32-551, can read every file)')
Check 'win: Hyper-V Administrators by SID' (Why (Win -sids "$std;S-1-5-32-578") 'windows') @('is in Hyper-V Administrators (S-1-5-32-578, can mount any disk)')
Check 'win: 5510 and 1578 are not those groups' (Why (Win -sids "$std;S-1-5-32-5510;S-1-5-21-1-2-3-578") 'windows').Count 0
Check 'win: a user and no group list is unknown, never a clean ok' (Get-AccountVerdict (KV "user=SG3\claude`nhost=SG3`nelevated=False`nsids=") 'windows' $me).Known $false
Check 'win: ... and the step says so' (Get-AccountResult @('user=SG3\claude', 'elevated=False', 'sids=') 0 'windows' 'sg3' $me @() $false $false $false).Detail 'could not tell who the room runs as on sg3 (the probe listed no groups). see docs/room-accounts.md'

# ── the groups as whoami /groups /fo csv /nh prints them (documented format, see the note at the end of the file) ──
# Column 3 is the SID. Column 4 is the attributes and is LOCALIZED, which is why it is never read.
$whoamiFiltered = @(
    '"Everyone","Well-known group","S-1-1-0","Mandatory group, Enabled by default, Enabled group"',
    '"NT AUTHORITY\Local account and member of Administrators group","Well-known group","S-1-5-114","Group used for deny only"',
    '"BUILTIN\Administrators","Alias","S-1-5-32-544","Group used for deny only"',
    '"BUILTIN\Users","Alias","S-1-5-32-545","Mandatory group, Enabled by default, Enabled group"',
    '"NT AUTHORITY\INTERACTIVE","Well-known group","S-1-5-4","Mandatory group, Enabled by default, Enabled group"',
    '"CONSOLE LOGON","Well-known group","S-1-2-1","Mandatory group, Enabled by default, Enabled group"',
    '"NT AUTHORITY\Authenticated Users","Well-known group","S-1-5-11","Mandatory group, Enabled by default, Enabled group"',
    '"NT AUTHORITY\This Organization","Well-known group","S-1-5-15","Mandatory group, Enabled by default, Enabled group"',
    '"NT AUTHORITY\Local account","Well-known group","S-1-5-113","Mandatory group, Enabled by default, Enabled group"',
    '"LOCAL","Well-known group","S-1-2-0","Mandatory group, Enabled by default, Enabled group"',
    '"NT AUTHORITY\NTLM Authentication","Well-known group","S-1-5-64-10","Mandatory group, Enabled by default, Enabled group"',
    '"Mandatory Label\Medium Mandatory Level","Label","S-1-16-8192","Mandatory group, Enabled by default, Enabled group"')
$whoamiElevated = @(
    '"Everyone","Well-known group","S-1-1-0","Mandatory group, Enabled by default, Enabled group"',
    '"NT AUTHORITY\Local account and member of Administrators group","Well-known group","S-1-5-114","Mandatory group, Enabled by default, Enabled group"',
    '"BUILTIN\Administrators","Alias","S-1-5-32-544","Mandatory group, Enabled by default, Enabled group, Group owner"',
    '"BUILTIN\Users","Alias","S-1-5-32-545","Mandatory group, Enabled by default, Enabled group"',
    '"NT AUTHORITY\Authenticated Users","Well-known group","S-1-5-11","Mandatory group, Enabled by default, Enabled group"',
    '"Mandatory Label\High Mandatory Level","Label","S-1-16-12288","Mandatory group, Enabled by default, Enabled group"')
# A German machine: names and attribute text are German, the SIDs are not. The attribute text here is not what makes it work.
$whoamiGerman = @(
    '"Jeder","Bekannte Gruppe","S-1-1-0","Obligatorische Gruppe, Standardmäßig aktiviert, Aktivierte Gruppe"',
    '"VORDEFINIERT\Administratoren","Alias","S-1-5-32-544","Gruppe wird nur für Verweigerung verwendet"',
    '"VORDEFINIERT\Benutzer","Alias","S-1-5-32-545","Obligatorische Gruppe, Standardmäßig aktiviert, Aktivierte Gruppe"',
    '"NT-AUTORITÄT\INTERAKTIV","Bekannte Gruppe","S-1-5-4","Obligatorische Gruppe, Standardmäßig aktiviert, Aktivierte Gruppe"',
    '"Mandatory Label\Medium Mandatory Level","Bezeichnung","S-1-16-8192","Obligatorische Gruppe, Standardmäßig aktiviert, Aktivierte Gruppe"')
# A domain admin's filtered token, with a group name that holds a comma.
$whoamiDomain = @(
    '"Everyone","Well-known group","S-1-1-0","Mandatory group, Enabled by default, Enabled group"',
    '"BUILTIN\Administrators","Alias","S-1-5-32-544","Group used for deny only"',
    '"CORP\Domain Admins","Group","S-1-5-21-111-222-333-512","Group used for deny only"',
    '"CORP\Enterprise Admins","Group","S-1-5-21-111-222-333-519","Group used for deny only"',
    '"CORP\Sales, EMEA","Group","S-1-5-21-111-222-333-1105","Mandatory group, Enabled by default, Enabled group"')
# The probe's own line, run here over a fixture. `whoami` is shadowed by a function. $id is what WindowsIdentity gives:
# Groups WITHOUT the deny-only ones (that is the .NET behaviour this probe must not rely on).
function Read-ProbeSids {
    param([string[]] $csv)
    $line = @($script:AccountWinProbe -split "`n" | Where-Object { $_.Trim() -like '$sids = *' })[0].Trim()
    function whoami { $csv }
    $id = [pscustomobject]@{ Groups = @($csv | Where-Object { $_ -notmatch 'deny only|Verweigerung' } | ForEach-Object { [pscustomobject]@{ Value = ($_ -split '","')[2] } }) }
    Invoke-Expression $line
    $sids
}
$fs = Read-ProbeSids $whoamiFiltered
Check 'whoami: the probe reads every SID, deny-only ones too' ($fs -contains 'S-1-5-32-544') $true
Check 'whoami: it reads the third column and nothing else (12 groups, no name)' @($fs.Count, @($fs | Where-Object { $_ -notlike 'S-1-*' }).Count) @(12, 0)
Check 'whoami: a filtered admin token (elevated False) from the real line is a warn with the filtered token' (Why (Win -elev 'False' -sids ($fs -join ';')) 'windows') @('is in Administrators (S-1-5-32-544) with a filtered token')
$es = Read-ProbeSids $whoamiElevated
Check 'whoami: an elevated token (elevated True) is Administrators and elevated' (Why (Win -elev 'True' -sids ($es -join ';')) 'windows') @('is in Administrators (S-1-5-32-544)', 'the token is elevated')
$gs = Read-ProbeSids $whoamiGerman
Check 'whoami: a German filtered token is matched by SID, not by VORDEFINIERT\Administratoren' (Why (Win -user 'PC1\Hans' -h 'PC1' -elev 'False' -sids ($gs -join ';')) 'windows') @('is in Administrators (S-1-5-32-544) with a filtered token')
$ds = Read-ProbeSids $whoamiDomain
Check 'whoami: a group name with a comma does not break the columns' ($ds -contains 'S-1-5-21-111-222-333-1105') $true
Check 'whoami: a domain admin filtered token says Administrators, Domain Admins and Enterprise Admins' (Why (Win -user 'CORP\clint' -elev 'False' -sids ($ds -join ';')) 'windows') @('is in Administrators (S-1-5-32-544) with a filtered token', 'is in Domain Admins (RID 512)', 'is in Enterprise Admins (RID 519)')
Check 'whoami: the standard user list has no admin group in it' (Why (Win -sids ((Read-ProbeSids ($whoamiFiltered | Where-Object { $_ -notmatch 'S-1-5-114|S-1-5-32-544' })) -join ';')) 'windows').Count 0

# ── macOS and Linux: uid, groups, sudo ──────────────────────────────────────

function Unx { param([string] $user = 'claude', [string] $uid = '501', [string] $groups = 'staff,everyone,localaccounts', [string] $rc = '1', [string] $nopw = '0', [string] $all = '0', [string] $h = 'm1mini')
    KV "user=$user`nuid=$uid`ngroups=$groups`nhost=$h`nsudo.rc=$rc`nsudo.nopasswd=$nopw`nsudo.all=$all" }
Check 'mac: the claude user on m1mini is clean (id -Gn: staff everyone localaccounts)' (Why (Unx) 'mac').Count 0
Check 'mac: uid 0' (Why (Unx -user 'root' -uid '0' -groups 'wheel,daemon,kmem,sys,tty,operator,procview,procmod,everyone,staff,certusers,localaccounts,admin') 'mac') @('is root (uid 0)', 'is in the group wheel', 'is in the group admin')
Check 'mac: the admin group (a real owner account)' (Why (Unx -user 'owner' -groups 'staff,admin,everyone,_appstore,_lpadmin,localaccounts') 'mac') @('is in the group admin')
Check 'mac: sudo needing a password is not passwordless' (Why (Unx -rc '1') 'mac').Count 0
Check 'linux: the sudo group, and docker beside it is said too' (Why (Unx -user 'dev' -uid '1000' -groups 'dev,sudo,docker') 'linux') @('is in the group sudo', 'is in the group docker (root on this host)')
Check 'linux: the wheel group' (Why (Unx -user 'dev' -uid '1000' -groups 'dev,wheel') 'linux') @('is in the group wheel')
Check 'linux: the admin group' (Why (Unx -user 'dev' -uid '1000' -groups 'dev,admin') 'linux') @('is in the group admin')
Check 'linux: docker is root on the host' (Why (Unx -user 'dev' -uid '1000' -groups 'dev,docker') 'linux') @('is in the group docker (root on this host)')
Check 'linux: lxd, incus-admin and libvirt are too' (Why (Unx -user 'dev' -uid '1000' -groups 'dev,lxd,incus-admin,libvirt') 'linux') @('is in the group lxd (root on this host)', 'is in the group incus-admin (root on this host)', 'is in the group libvirt (root on this host)')
Check 'mac: docker and libvirt are not on the macOS list' (Why (Unx -groups 'staff,docker,libvirt,lxd') 'mac').Count 0
Check 'linux: root groups are whole names too' (Why (Unx -user 'dev' -uid '1000' -groups 'dev,dockers,libvirt-qemu,lxd2') 'linux').Count 0
Check 'linux: group names are whole words (sudoers, administrators)' (Why (Unx -user 'dev' -uid '1000' -groups 'dev,sudoers,administrators,wheel2') 'linux').Count 0
Check 'linux: passwordless sudo (sudo -n -l prints (ALL) NOPASSWD: ALL)' (Why (Unx -user 'dev' -uid '1000' -groups 'dev' -rc '0' -nopw '1' -all '1') 'linux') @('can run any command with sudo and no password (sudo -n -l says NOPASSWD: ALL)')
Check 'linux: sudo that lists ALL (a cached credential or a rule with a password) says only what was seen' (Why (Unx -user 'dev' -uid '1000' -groups 'dev' -rc '0' -nopw '0' -all '1') 'linux') @('can run any command with sudo (sudo -n -l lists ALL)')
Check 'linux: sudo -n -l that works for one named command is a warn too (a narrow rule is a shell escape)' (Why (Unx -user 'dev' -uid '1000' -groups 'dev' -rc '0' -nopw '0' -all '0') 'linux') @('can use sudo with no password for some command (sudo -n -l works), and one command is usually a way to root')
Check 'mac: the same narrow rule is a warn' (Why (Unx -rc '0' -nopw '0' -all '0') 'mac').Count 1
Check 'linux: sudo -n -l that needed a password (rc 1) is clean' (Why (Unx -user 'dev' -uid '1000' -groups 'dev' -rc '1') 'linux').Count 0
Check 'linux: sudo -n -l that did not answer in time is a warn, never a clean ok' (Why (Unx -user 'dev' -uid '1000' -groups 'dev' -rc 'timeout') 'linux') @('may have sudo, since sudo -n -l did not answer in 5 seconds')
Check 'linux: no sudo installed' (Why (KV "user=dev`nuid=1000`ngroups=dev`nhost=h`nsudo.rc=none") 'linux').Count 0
Check 'linux: the sudo group and passwordless sudo are both said' (Why (Unx -user 'dev' -uid '1000' -groups 'dev,sudo' -rc '0' -nopw '1' -all '1') 'linux').Count 2
Check 'mac: the sudo group is not a macOS admin group' (Why (Unx -groups 'staff,sudo') 'mac').Count 0

# ── the operator's own account ──────────────────────────────────────────────

$opSay = 'is the account that runs this script, on this machine, so it is the operator''s own login'
Check 'operator: same login, same machine' (Why (Unx -user 'clint' -h 'sg4') 'linux') @($opSay)
Check 'operator: the host match ignores case and a domain suffix' (Why (Unx -user 'Clint' -h 'sg4.corp.example') 'linux') @($opSay)
Check 'operator: same login on ANOTHER machine is not caught (that is what -OperatorAccount is for)' (Why (Unx -user 'clint' -h 'm1mini') 'linux').Count 0
Check 'operator: the target is local, so the host is not compared' (Why (Unx -user 'clint' -h 'something-else') 'linux' @() $true) @($opSay)
Check 'operator: a different login on this machine is not the operator' (Why (Unx -user 'claude' -h 'sg4') 'linux').Count 0
Check 'operator: Windows DOMAIN\name is compared by its name' (Why (Win -user 'SG4\clint' -h 'SG4') 'windows') @($opSay)
Check 'operator: listed by name' (Why (Unx -user 'owner' -h 'm1mini') 'mac' @('owner')) @('is listed as an operator account (-OperatorAccount or ATRIUM_OPERATOR_ACCOUNT)')
Check 'operator: listed, case does not matter' (Why (Unx -user 'Owner' -h 'm1mini') 'mac' @('owner')).Count 1
Check 'operator: listed as name@host matches that host only' @((Why (Unx -user 'owner' -h 'm1mini') 'mac' @('owner@m1mini')).Count, (Why (Unx -user 'owner' -h 'other') 'mac' @('owner@m1mini')).Count) @(1, 0)
Check 'operator: listed as DOMAIN\name needs the domain too' @((Why (Win -user 'CORP\clint' -h 'PC') 'windows' @('CORP\clint')).Count, (Why (Win -user 'OTHER\clint' -h 'PC') 'windows' @('CORP\clint')).Count) @(1, 0)
Check 'operator: a listed name does not match a longer name' (Why (Unx -user 'owner2' -h 'm1mini') 'mac' @('owner')).Count 0
Check 'operator: both rules at once say it once' (Why (Unx -user 'clint' -h 'sg4') 'linux' @('clint')).Count 1
Check 'operator list: the parameter and the environment, split on commas and semicolons' (Get-OperatorList @('a, b') 'c;d,,e') @('a', 'b', 'c', 'd', 'e')
Check 'operator list: nothing' @(Get-OperatorList @() '').Count 0
Check 'operator arg: forms accepted' (@('owner', 'CORP\owner', 'owner@host.example', 'CORP\owner@host') | ForEach-Object { [bool](Test-OperatorArg $_) } | Select-Object -Unique) @($false)
Check 'operator arg: a space, a pipe and an empty host are refused' (@('a b', 'a|b', 'a@') | ForEach-Object { [bool](Test-OperatorArg $_) }) @($true, $true, $true)

# ── the step: text, status, refusal ─────────────────────────────────────────

function Res { param($kv, [string] $kind = 'windows', [bool] $accept = $false, [bool] $require = $false, [string[]] $ops = @())
    $out = @($kv.Keys | ForEach-Object { "$_=$($kv[$_])" }); Get-AccountResult $out 0 $kind 'sg3' $me $ops $false $accept $require }
$wAdmin = Win -elev 'True' -sids "$std;S-1-5-32-544"
$r = Res $wAdmin
Check 'step: elevated admin on Windows is a warn' @($r.Status, $r.Refuse) @('warn', $false)
Check 'step: the warn names the account, the reason and the page' ($r.Detail -like 'SG3\claude is in Administrators (S-1-5-32-544) and the token is elevated. *see docs/room-accounts.md*') $true
Check 'step: the warn says how to accept it' ($r.Detail -match '-IAcceptRunningAsMe') $true
Check 'step: a warn is one line' ($r.Detail -notmatch "`n") $true
$r = Res (Win)
Check 'step: a clean Windows account is ok, with no sudo in it' @($r.Status, $r.Detail) @('ok', 'SG3\claude: not elevated, not in an admin group, not the operator''s account')
$r = Res (Unx) 'mac'
Check 'step: a clean Unix account is ok with the exact text' @($r.Status, $r.Detail) @('ok', 'claude: not elevated, not in an admin group, no passwordless sudo, not the operator''s account')
$r = Res (Unx -user 'dev' -uid '1000' -groups 'dev,sudo,wheel' -rc '0' -nopw '1' -all '1') 'linux'
Check 'step: three reasons read with a comma and an and' ($r.Detail -like 'dev is in the group sudo, is in the group wheel and can run any command with sudo and no password (sudo -n -l says NOPASSWD: ALL). *') $true
$r = Res (Unx -user 'root' -uid '0' -groups 'root' -rc '0' -nopw '1' -all '1') 'linux'
Check 'step: two reasons read with an and' ($r.Detail -like 'root is root (uid 0) and can run any command with sudo and no password (sudo -n -l says NOPASSWD: ALL). *') $true
$r = Res $wAdmin 'windows' $true
Check 'step: accepted is ok, still names the reason and the flag' @($r.Status, $r.Refuse, ($r.Detail -like 'accepted by the operator (-IAcceptRunningAsMe): SG3\claude is in Administrators (S-1-5-32-544) and the token is elevated. see docs/room-accounts.md')) @('ok', $false, $true)
$r = Res (Win) 'windows' $true
Check 'step: accepting a clean account changes nothing' $r.Detail 'SG3\claude: not elevated, not in an admin group, not the operator''s account'
$r = Res $wAdmin 'windows' $false $true
Check 'step: required and elevated is a fail that refuses' @($r.Status, $r.Refuse, ($r.Detail -like '*and -RequireDedicatedAccount does not go on with that. see docs/room-accounts.md')) @('fail', $true, $true)
$r = Res (Win) 'windows' $false $true
Check 'step: required and clean goes on' @($r.Status, $r.Refuse) @('ok', $false)
$r = Res (Unx -user 'clint' -h 'sg4') 'linux' $false $true
Check 'step: required and the operator''s own account refuses' @($r.Status, $r.Refuse) @('fail', $true)
$r = Get-AccountResult @('err=Cannot call a method on a null-valued expression.') 0 'windows' 'sg3' $me @() $false $false $false
Check 'step: a probe that errs is a warn saying why, and goes on' @($r.Status, $r.Refuse, ($r.Detail -like 'could not tell who the room runs as on sg3 (Cannot call a method*')) @('warn', $false, $true)
$r = Get-AccountResult @() 255 'linux' 'sg3' $me @() $false $false $false
Check 'step: a probe that says nothing and exits 255' $r.Detail 'could not tell who the room runs as on sg3 (the probe exited 255). see docs/room-accounts.md'
$r = Get-AccountResult @() 0 'linux' 'sg3' $me @() $false $false $false
Check 'step: a probe with no output' ($r.Detail -like '*(the probe said nothing)*') $true
$r = Get-AccountResult @('err=boom') 0 'windows' 'sg3' $me @() $false $false $true
Check 'step: required and the probe cannot tell refuses' @($r.Status, $r.Refuse) @('fail', $true)
Check 'step: no group is dumped, only the matched reason' ((Res (Win -elev 'True' -sids "$std;S-1-5-32-544;S-1-5-21-1-2-3-1105") 'windows').Detail -notmatch 'S-1-5-21-1-2-3-1105|S-1-5-14') $true
Check 'step: a clean account prints no group either' ((Res (Unx -groups 'staff,everyone,localaccounts,com.apple.access_ssh')).Detail -notmatch 'com.apple|localaccounts') $true

# ── the probes ──────────────────────────────────────────────────────────────

$errs = $null
[System.Management.Automation.Language.Parser]::ParseInput($script:AccountWinProbe, [ref]$null, [ref]$errs) | Out-Null
Check 'win probe: parses' @($errs).Count 0
Check 'win probe: nothing 5.1 lacks' @(Find-Ps7Only $script:AccountWinProbe).Count 0
$len = (New-EncodedCommand $script:AccountWinProbe).Length
Check "win probe: encoded size $len is far under 7800" ($len -le 3000) $true
$wide = 'S-1-5-21-' + ('1' * 30)
Check 'win probe: with the longest remote preamble provision-room adds it is still under 7800' ((New-EncodedCommand ("`$ErrorActionPreference='Stop'; `$ProgressPreference='SilentlyContinue'`n" + ('# ' + ('x' * 900) + "`n") + $script:AccountWinProbe)).Length -le 7800) $true
Check 'win probe: the groups come from whoami, not from WindowsIdentity.Groups (which drops deny-only groups)' (($script:AccountWinProbe -match 'whoami /groups /fo csv /nh') -and ($script:AccountWinProbe -notmatch '\.Groups\b')) $true
Check 'win probe: reads only, no write verb in it' ($script:AccountWinProbe -notmatch '\b(Set|New-Item|Remove|Out-File|Add-Content|Set-Content|Start-Process|Stop|Invoke-Expression|iex)\b') $true
Check 'win probe: no variable is injected, so nothing needs quoting' ($script:AccountWinProbe -notmatch '\$Target|\$User|\$Prefix') $true
Check 'unix probe: the only sudo is `sudo -n -l`' (@([regex]::Matches($script:AccountUnixProbe, 'sudo[^\n]*') | Where-Object { $_.Value -match 'sudo -n' } | ForEach-Object { ($_.Value -split ' ')[0..2] -join ' ' }) | Select-Object -Unique) @('sudo -n -l')
Check 'unix probe: nothing is written, nothing but the probe commands run' ($script:AccountUnixProbe -notmatch '[^2]>[^&/]|\brm\b|\bmv\b|\bchmod\b|\btee\b') $true
if ($unix) {
    $o = $script:AccountUnixProbe | & sh -s 2>&1
    $k = KV ($o -join "`n")
    Check 'unix probe: run for real, the user is this user' $k['user'] (& id -un)
    Check 'unix probe: the uid is this uid' $k['uid'] (& id -u)
    Check 'unix probe: the groups are id -Gn, comma separated' $k['groups'] ((& id -Gn) -replace ' ', ',')
    Check 'unix probe: the host is uname -n' $k['host'] (& uname -n)
    Check 'unix probe: sudo.rc is a number or none' ($k['sudo.rc'] -match '^(\d+|none)$') $true
    $v = Get-AccountVerdict $k $(if ((& uname -s) -eq 'Darwin') { 'mac' } else { 'linux' }) (Get-LocalIdentity) @() $false
    Check 'unix probe: and the verdict for this machine knows the account' $v.Known $true
}
if ($unix) {
    # A sudo that never answers. It is first in PATH, so the probe meets it, and the watcher must cut it off at 5 seconds with
    # sudo.rc=timeout. (macOS has no timeout(1), so this is the path that every Mac takes.) The same probe with a sudo that
    # answers must pass its output through.
    $bin = Join-Path $tmp 'slowbin'
    New-Item -ItemType Directory -Force $bin | Out-Null
    Set-Content -LiteralPath (Join-Path $bin 'sudo') -NoNewline -Value "#!/bin/sh`nexec sleep 60`n"
    & chmod +x (Join-Path $bin 'sudo')
    $t0 = Get-Date
    $o = $script:AccountUnixProbe | & sh -c "PATH='$bin':`$PATH; export PATH; sh -s" 2>&1
    $secs = ((Get-Date) - $t0).TotalSeconds
    Check 'unix probe: a sudo that hangs is cut off, sudo.rc=timeout' (KV ($o -join "`n"))['sudo.rc'] 'timeout'
    Check "unix probe: and that took about 5 seconds ($([int]$secs)), not 60" (($secs -ge 4) -and ($secs -lt 20)) $true
    Check 'unix probe: the rest of the facts were still read' (KV ($o -join "`n"))['user'] (& id -un)
    Set-Content -LiteralPath (Join-Path $bin 'sudo') -NoNewline -Value "#!/bin/sh`necho 'User dev may run the following commands on h:'; echo '    (ALL) NOPASSWD: /usr/bin/vim'; exit 0`n"
    $o = $script:AccountUnixProbe | & sh -c "PATH='$bin':`$PATH; export PATH; sh -s" 2>&1
    $k2 = KV ($o -join "`n")
    Check 'unix probe: a sudo that answers 0 passes its rc through, with no ALL line' @($k2['sudo.rc'], $k2['sudo.nopasswd'], $k2['sudo.all']) @('0', '0', '0')
    Set-Content -LiteralPath (Join-Path $bin 'sudo') -NoNewline -Value "#!/bin/sh`necho 'a password is required'; exit 1`n"
    $o = $script:AccountUnixProbe | & sh -c "PATH='$bin':`$PATH; export PATH; sh -s" 2>&1
    Check 'unix probe: a sudo that needs a password is rc 1' (KV ($o -join "`n"))['sudo.rc'] '1'
    Check 'unix probe: the probe leaves no process or file of its own in the temp folder' @(Get-ChildItem -LiteralPath $bin).Count 1

    # The whole call, capped. A fake ssh that never answers must come back as the could-not-tell text, in seconds.
    $hang = Join-Path $tmp 'hangssh'
    Set-Content -LiteralPath $hang -NoNewline -Value "#!/bin/sh`nsleep 60`n"
    & chmod +x $hang
    $t0 = Get-Date
    $hr = Invoke-AccountProbe 'unix' $hang @('-o', 'BatchMode=yes') 'svc@lab1' $false 2
    $secs = ((Get-Date) - $t0).TotalSeconds
    Check 'probe cap: an ssh that never answers returns in seconds' (($secs -ge 1.5) -and ($secs -lt 15)) $true
    Check 'probe cap: and reads as an err= line' ($hr.Out -join '|') 'err=the probe did not answer in 2 seconds'
    Check 'probe cap: which the step turns into a warn that goes on' (Get-AccountResult $hr.Out $hr.Code 'linux' 'svc@lab1' $me @() $false $false $false).Status 'warn'
    Check 'probe cap: and a refusal under -RequireDedicatedAccount' (Get-AccountResult $hr.Out $hr.Code 'linux' 'svc@lab1' $me @() $false $false $true).Refuse $true
    $hr = Invoke-AccountProbe 'windows' $hang @() 'svc@lab1' $false 2
    Check 'probe cap: the Windows call is capped the same way' ($hr.Out -join '|') 'err=the probe did not answer in 2 seconds'
    $hr = Invoke-AccountProbe 'unix' (Join-Path $tmp 'no-such-ssh') @() 'svc@lab1' $false 2
    Check 'probe cap: an ssh that cannot start is an err= line too' (($hr.Out -join '|') -like 'err=could not run *') $true
    $hr = Invoke-AccountProbe 'unix' 'sh' @() '' $true 20
    Check 'probe cap: a local target runs the probe here (sh -s)' (KV ($hr.Out -join "`n"))['user'] (& id -un)
}
$wo = (& $pwsh -NoProfile -Command ($script:AccountWinProbe) 2>&1)
if ($unix) {
    Check 'win probe: under pwsh on a Mac it reports err= and does not throw' (@($wo | Where-Object { "$_" -match '^err=' }).Count -ge 1) $true
    Check 'win probe: and that is a warn that goes on' (Get-AccountResult $wo 0 'windows' 'x' $me @() $false $false $false).Status 'warn'
}

# ── room-toolchain.ps1 and provision-room.ps1, end to end against a fake ssh ─

if ($unix) {
    # A fake ssh. `uname -sm` and `uname -m` answer like a Linux box, an account probe gets the fixture in $FAKE_ACCOUNT, and any
    # other script is run by sh here.
    $fake = Join-Path $tmp 'ssh'
    Set-Content -LiteralPath $fake -NoNewline -Value @'
#!/bin/sh
for last; do :; done
case "$last" in
  'uname -sm') echo "${FAKE_UNAME:-Linux x86_64}"; exit 0;;
  'uname -m') echo "${FAKE_UNAME:-Linux x86_64}" | cut -d' ' -f2; exit 0;;
  'sh -s') in=$(cat); case "$in" in *'echo "user=$(id -un)"'*) cat "$FAKE_ACCOUNT"; exit 0;; esac; printf '%s\n' "$in" | sh -s; exit $?;;
esac
echo "fake ssh: not expected: $*" >&2; exit 9
'@
    & chmod +x $fake
    $acct = Join-Path $tmp 'account.txt'
    function Say { param([string] $text) Set-Content -LiteralPath $acct -Value $text }
    $env:FAKE_ACCOUNT = $acct
    $tc = Join-Path $PSScriptRoot 'room-toolchain.ps1'
    $pr = Join-Path $PSScriptRoot 'provision-room.ps1'
    function Tc { param([string[]] $a) $o = & $pwsh -NoProfile -File $tc 'svc@lab1' -Ssh $fake -Check -Tools go -StateDir (Join-Path $tmp 'st') @a 2>&1; [pscustomobject]@{ Out = @($o | ForEach-Object { "$_" }); Code = $LASTEXITCODE } }
    function Pr { param([string[]] $a) $o = & $pwsh -NoProfile -File $pr 'svc@lab1' -Ssh $fake -Scp $fake -Name lab1 @a 2>&1; [pscustomobject]@{ Out = @($o | ForEach-Object { "$_" }); Code = $LASTEXITCODE } }
    function Line { param($r, [string] $rx) @($r.Out | Where-Object { $_ -match $rx }) }

    Say "user=svc`nuid=1001`ngroups=svc`nhost=lab1`nsudo.rc=1`nsudo.nopasswd=0`nsudo.all=0"
    $r = Tc @()
    Check 'toolchain: a clean account is one ok line right after ssh' (($r.Out | Where-Object { $_ -match '^room-toolchain (ssh|account|prefix) ' } | ForEach-Object { ($_ -split ' ')[1, 2] -join ' ' }) -join '|') 'ssh ok|account ok|prefix ok'
    Check 'toolchain: the clean line is the documented one' (@(Line $r '^room-toolchain account ')[0]) 'room-toolchain account ok svc: not elevated, not in an admin group, no passwordless sudo, not the operator''s account'
    Check 'toolchain: a clean account ends ok, exit 0' @($r.Code, $r.Out[-1]) @(0, 'room-toolchain done ok')
    $r = Tc @('-RequireDedicatedAccount')
    Check 'toolchain: -RequireDedicatedAccount with a clean account goes on' @($r.Code, @(Line $r '^room-toolchain account ok').Count) @(0, 1)

    Say "user=svc`nuid=1001`ngroups=svc,sudo`nhost=lab1`nsudo.rc=0`nsudo.nopasswd=1`nsudo.all=1"
    $r = Tc @()
    Check 'toolchain: an admin account is a warn and the run goes on, exit 0' @($r.Code, @(Line $r '^room-toolchain account warn svc is in the group sudo and can run any command with sudo and no password').Count, $r.Out[-1]) @(0, 1, 'room-toolchain done ok')
    Check 'toolchain: the warn points to the doc' ((@(Line $r '^room-toolchain account warn')[0]) -match 'see docs/room-accounts.md') $true
    Check 'toolchain: the run still did its work after the warn' @(Line $r '^room-toolchain (prefix|go) ').Count 2
    $r = Tc @('-IAcceptRunningAsMe')
    Check 'toolchain: -IAcceptRunningAsMe is an ok that still names the reason' ((Line $r '^room-toolchain account ok accepted by the operator \(-IAcceptRunningAsMe\): svc is in the group sudo').Count) 1
    $r = Tc @('-RequireDedicatedAccount')
    Check 'toolchain: -RequireDedicatedAccount is a fail, exit 1, and stops there' @($r.Code, @(Line $r '^room-toolchain account fail svc is in the group sudo').Count, @(Line $r '^room-toolchain (prefix|go) ').Count, $r.Out[-1]) @(1, 1, 0, 'room-toolchain done fail 1')
    $r = Tc @('-IAcceptRunningAsMe', '-RequireDedicatedAccount')
    Check 'toolchain: accept and require together are refused up front' @($r.Code, @(Line $r '^room-toolchain args fail ').Count, @(Line $r '^room-toolchain ssh').Count) @(1, 1, 0)
    $r = Tc @('-OperatorAccount', 'a b')
    Check 'toolchain: a bad -OperatorAccount is refused up front' @($r.Code, @(Line $r '^room-toolchain args fail bad operator account').Count) @(1, 1)

    Say "user=owner`nuid=501`ngroups=staff`nhost=lab1`nsudo.rc=1"
    $r = Tc @('-OperatorAccount', 'owner')
    Check 'toolchain: -OperatorAccount makes a plain account the operator''s, a warn' @(@(Line $r '^room-toolchain account warn owner is listed as an operator account').Count, $r.Code) @(1, 0)
    $env:ATRIUM_OPERATOR_ACCOUNT = 'someone,owner@lab1'
    $r = Tc @()
    Check 'toolchain: ATRIUM_OPERATOR_ACCOUNT does the same' @(Line $r '^room-toolchain account warn owner is listed').Count 1
    Remove-Item Env:ATRIUM_OPERATOR_ACCOUNT
    Say "err=probe broke"
    $r = Tc @()
    Check 'toolchain: a probe that cannot tell is a warn and the run goes on' @($r.Code, @(Line $r '^room-toolchain account warn could not tell who the room runs as on svc@lab1 \(probe broke\)').Count) @(0, 1)
    $r = Tc @('-RequireDedicatedAccount')
    Check 'toolchain: required and the probe cannot tell is exit 1' @($r.Code, @(Line $r '^room-toolchain account fail could not tell').Count) @(1, 1)
    Say "user=svc`nuid=1001`ngroups=svc`nhost=lab1`nsudo.rc=none"
    $o = & $pwsh -NoProfile -File $tc 'svc@lab1' -Ssh $fake -Check -Tools git 2>&1
    Check 'toolchain: when no tool applies the account line still comes before the skip' (($o | Where-Object { $_ -match '^room-toolchain (account|tools) ' } | ForEach-Object { ($_ -split ' ')[1] }) -join '|') 'account|tools'

    # provision-room: the account-rights step comes after os (and after the -User check) and before the hub is looked for.
    Say "user=svc`nuid=1001`ngroups=svc,wheel`nhost=lab1`nsudo.rc=1"
    $r = Pr @()
    Check 'provision: an admin account is account-rights warn naming the reason and the doc' (@(Line $r '^provision account-rights warn svc is in the group wheel\. .*see docs/room-accounts\.md').Count) 1
    Check 'provision: the warn does not stop the run (it gets to the hub step)' (@(Line $r '^provision hub ').Count -ge 1) $true
    Check 'provision: the order is ssh, os, account-rights' (($r.Out | Where-Object { $_ -match '^provision (ssh|os|account-rights) ' } | ForEach-Object { ($_ -split ' ')[1] }) -join '|') 'ssh|os|account-rights'
    $r = Pr @('-RequireDedicatedAccount')
    Check 'provision: -RequireDedicatedAccount is a fail and exit 6' @($r.Code, @(Line $r '^provision account-rights fail svc is in the group wheel').Count, $r.Out[-1], @(Line $r '^provision hub ').Count) @(6, 1, 'provision done fail 6', 0)
    $r = Pr @('-IAcceptRunningAsMe')
    Check 'provision: -IAcceptRunningAsMe is ok and names the reason' @(Line $r '^provision account-rights ok accepted by the operator \(-IAcceptRunningAsMe\): svc is in the group wheel').Count 1
    $r = Pr @('-IAcceptRunningAsMe', '-RequireDedicatedAccount')
    Check 'provision: accept and require together are exit 1 before ssh' @($r.Code, @(Line $r '^provision args fail').Count, @(Line $r '^provision ssh').Count) @(1, 1, 0)
    $r = Pr @('-OperatorAccount', 'a|b')
    Check 'provision: a bad -OperatorAccount is exit 1 before ssh' @($r.Code, @(Line $r '^provision ssh').Count) @(1, 0)
    Say "user=svc`nuid=1001`ngroups=svc`nhost=lab1`nsudo.rc=1"
    $r = Pr @('-RequireDedicatedAccount')
    Check 'provision: a clean account with -RequireDedicatedAccount goes on past the step' @(@(Line $r '^provision account-rights ok svc: not elevated').Count, ($r.Code -ne 6)) @(1, $true)
    Say "user=svc`nuid=1001`ngroups=svc,wheel`nhost=lab1"
    $r = Pr @('-Remove', '-RequireDedicatedAccount')
    Check 'provision: -Remove never runs the account step, so it is never refused by it' @(Line $r '^provision account-rights').Count 0
    $r = Pr @('-SmokeOnly')
    Check 'provision: -SmokeOnly and -Restart do not run it either' @((Line $r '^provision account-rights').Count, (@(Pr @('-Restart')) | ForEach-Object { Line $_ '^provision account-rights' }).Count) @(0, 0)
}

    # macOS is not Linux: the group admin counts and the group sudo does not (the kind is passed through by every caller)
    $env:FAKE_UNAME = 'Darwin arm64'
    Say "user=svc`nuid=502`ngroups=staff,admin`nhost=lab1`nsudo.rc=1"
    Check 'mac: toolchain says the group admin' @(Line (Tc @()) '^room-toolchain account warn svc is in the group admin\. ').Count 1
    Check 'mac: provision says the group admin' @(Line (Pr @()) '^provision account-rights warn svc is in the group admin\. ').Count 1
    Say "user=svc`nuid=502`ngroups=staff,sudo`nhost=lab1`nsudo.rc=1"
    Check 'mac: the group sudo is not an admin group there (toolchain)' @(Line (Tc @()) '^room-toolchain account ok svc: ').Count 1
    Check 'mac: the group sudo is not an admin group there (provision)' @(Line (Pr @()) '^provision account-rights ok svc: ').Count 1
    Remove-Item Env:FAKE_UNAME

# room-check.ps1 end to end: a mock hub (python3 serving /_hub/rooms with no rooms), the same fake ssh, and an atrium that has the
# `requirements` subcommand. Skipped, and said so, where one of those is missing. The room is not attached, which is its own
# `human` row and exit 4 whatever the account says, so the exit code must be the same for every account.
function Test-Requirements { param([string] $exe) if (-not $exe) { return $false }; & $exe requirements --help *>$null; $LASTEXITCODE -eq 0 }
$atriumBin = @((Get-Command atrium -ErrorAction SilentlyContinue).Source) | Where-Object { Test-Requirements $_ } | Select-Object -First 1
if ($unix -and (Get-Command python3 -ErrorAction SilentlyContinue) -and $atriumBin) {
    $hubPy = Join-Path $tmp 'hub.py'
    Set-Content -LiteralPath $hubPy -Value @'
import http.server, json, sys
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        b = json.dumps({"rooms": []}).encode()
        self.send_response(200); self.send_header('Content-Type', 'application/json'); self.send_header('Content-Length', str(len(b))); self.end_headers(); self.wfile.write(b)
    def log_message(self, *a): pass
http.server.HTTPServer(('127.0.0.1', int(sys.argv[1])), H).serve_forever()
'@
    $port = Get-Random -Minimum 20000 -Maximum 30000
    $hub = Start-Process -FilePath python3 -ArgumentList $hubPy, $port -PassThru
    try {
        Start-Sleep -Seconds 1
        function Rc { param([string[]] $a) $o = & $pwsh -NoProfile -File (Join-Path $PSScriptRoot 'room-check.ps1') rc1 -Target svc@lab1 -Ssh $fake -HubAddr "127.0.0.1:$port" -NoSmoke -Binary $atriumBin @a 2>&1; [pscustomobject]@{ Out = @($o | ForEach-Object { "$_" }); Code = $LASTEXITCODE } }
        Say "user=svc`nuid=1001`ngroups=svc`nhost=lab1`nsudo.rc=1"
        $clean = Rc @()
        Check 'room-check: a clean account is an account row that is ok' @(Line $clean '^room-check account ok svc: not elevated').Count 1
        Check 'room-check: the account row comes right after ssh' (($clean.Out | Where-Object { $_ -match '^room-check (ssh|account) ' } | ForEach-Object { ($_ -split ' ')[1] }) -join '|') 'ssh|account'
        Say "user=svc`nuid=1001`ngroups=svc,sudo`nhost=lab1`nsudo.rc=1"
        $warned = Rc @()
        Check 'room-check: an admin account is an account warn naming the reason and the doc' (@(Line $warned '^room-check account warn svc is in the group sudo\. .*see docs/room-accounts\.md').Count) 1
        Check 'room-check: the warn does not move the exit code' ($warned.Code -eq $clean.Code) $true
        Check 'room-check: nor the rest of the rows' ((@($warned.Out | Where-Object { $_ -notmatch '^room-check account ' }) -join "`n") -eq (@($clean.Out | Where-Object { $_ -notmatch '^room-check account ' }) -join "`n")) $true
        $acc = Rc @('-IAcceptRunningAsMe')
        Check 'room-check: -IAcceptRunningAsMe is an ok that still names the reason' @(Line $acc '^room-check account ok accepted by the operator \(-IAcceptRunningAsMe\): svc is in the group sudo').Count 1
        Say "user=svc`nuid=1001`ngroups=svc`nhost=lab1`nsudo.rc=1"
        $op = Rc @('-OperatorAccount', 'svc@lab1')
        Check 'room-check: -OperatorAccount name@host makes it the operator''s' @(Line $op '^room-check account warn svc is listed as an operator account').Count 1
        $env:FAKE_UNAME = 'Darwin arm64'
        Say "user=svc`nuid=502`ngroups=staff,sudo`nhost=lab1`nsudo.rc=1"
        Check 'room-check: on a Mac the group sudo is not an admin group' @(Line (Rc @()) '^room-check account ok svc: ').Count 1
        Say "user=svc`nuid=502`ngroups=staff,admin`nhost=lab1`nsudo.rc=1"
        Check 'room-check: on a Mac the group admin is' @(Line (Rc @()) '^room-check account warn svc is in the group admin\. ').Count 1
        Remove-Item Env:FAKE_UNAME
        $bad = Rc @('-OperatorAccount', 'a b')
        Check 'room-check: a bad -OperatorAccount is exit 1 before anything runs' @($bad.Code, @(Line $bad '^room-check args fail').Count, @(Line $bad '^room-check ssh').Count) @(1, 1, 0)
    } finally { Stop-Process -Id $hub.Id -Force -ErrorAction SilentlyContinue }
} else {
    Write-Host 'skip room-check end to end: needs python3 and an atrium with the requirements subcommand'
}

# ── the callers parse, and room-check carries the row ───────────────────────

foreach ($f in 'room-account.ps1', 'room-toolchain.ps1', 'provision-room.ps1', 'room-check.ps1', 'test-room-account.ps1') {
    $e = $null
    [System.Management.Automation.Language.Parser]::ParseFile((Join-Path $PSScriptRoot $f), [ref]$null, [ref]$e) | Out-Null
    Check "parse: $f" @($e).Count 0
}
$rc = Get-Content -LiteralPath (Join-Path $PSScriptRoot 'room-check.ps1') -Raw
Check 'room-check: dot-sources room-account.ps1, probes, and says the account row' @(($rc -match "room-account\.ps1"), ($rc -match "Row 'account' \`$av\.Status \`$av\.Detail"), ($rc -match 'Get-AccountResult')) @($true, $true, $true)
Check 'room-check: the row is advice, it is not counted as unmet' (($rc -split "`n" | Where-Object { $_ -match "Row 'account'" -and $_ -match 'Unmet' }).Count) 0
$sizes = & $pwsh -NoProfile -File (Join-Path $PSScriptRoot 'room-toolchain.ps1') x -PayloadSizes
Check 'script hook: the old payloads are listed as before and still under 7800' (@($sizes | Where-Object { $_ -match '^payload \w+ (\d+)$' -and [int]$Matches[1] -le 7800 }).Count) (4 + $script:CActs.Count)

} finally {
    Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
}

Write-Host ''
if ($script:failed) { Write-Host "$script:failed of $script:ran checks failed."; exit 1 }
Write-Host "all $script:ran checks pass."
