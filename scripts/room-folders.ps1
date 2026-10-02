# The allowed-folders helpers that provision-room.ps1 and room-check.ps1 share. DOT-SOURCED, runs nothing by itself:
#
#   . (Join-Path $PSScriptRoot 'room-folders.ps1')
#
# A room has a list of folders atrium may launch in (the `browse_roots` setting), and claude's folder trust is written
# for each, so no launch meets the "do you trust this folder" dialog. Both are done on the room by one verb, which these
# scripts call over ssh and never reimplement:
#
#   atrium room folders allow <dir>...   appends each dir to the list, writes claude's trust for it, refuses home and a
#                                        filesystem root. One line per dir: `allowed <dir>`, `trusted <dir>` or
#                                        `skipped <dir>: <reason>`. Exit 0 when every dir ended allowed, 1 otherwise.
#   atrium room folders list --json      {"roots":[...],"enforced":true|false}
#
# Everything here is pure text in and text out, so scripts/test-room-folders.ps1 can run it without ssh. The one thing
# that touches the remote is the script Get-FolderScript BUILDS, and the caller runs it with its own Invoke-Remote.

# ConvertTo-PsLiteral and ConvertTo-ShLiteral put a value inside a remote script as a literal, the way Quote-Ps and
# Quote-Sh do in the scripts that dot-source this.
function ConvertTo-PsLiteral { param([string] $s) "'" + ($s -replace "['\u2018\u2019\u201A\u201B]", '$0$0') + "'" }
function ConvertTo-ShLiteral { param([string] $s) "'" + ($s -replace "'", "'\''") + "'" }

# Test-FolderArg is why a folder argument cannot be used, or $null when it can. It looks at the text alone: whether
# the folder exists, and whether it is home, are the verb's to say on the room.
#
# ABSOLUTE ONLY, because the remote's working directory is whatever ssh gave it. A leading ~ is accepted and is
# expanded by Expand-FolderArg. A semicolon and a newline are how the setting separates its entries, so a folder holding one
# would become two.
function Test-FolderArg {
    param([string] $dir)
    if (-not $dir) { return 'is empty' }
    if ($dir -match '[;\r\n\0]') { return 'holds a semicolon or a newline, which the folder list uses to separate entries' }
    if ($dir -notmatch '^(/|[A-Za-z]:[\\/]|~([\\/]|$))') { return 'is not an absolute path (start it with / or a drive letter, or with ~/)' }
    if ($dir -match '^(/+|[A-Za-z]:[\\/]*)$') { return 'is a filesystem root, which would allow everything on the machine' }
    $null
}

# Expand-FolderArg turns a leading ~ into the remote home, which is how it is known on this side.
function Expand-FolderArg {
    param([string] $dir, [string] $remoteHome)
    if ($dir -eq '~') { return $remoteHome }
    if ($dir -match '^~[\\/](.*)$') { return ($remoteHome.TrimEnd('/', '\') + '/' + $Matches[1]) }
    $dir
}

# Get-DefaultFolders is the list a NEW provision allows: the room's clone, the folder its worktrees go under
# (room-git.ps1 makes them at <clone>-worktrees), and WORKTREE_ROOT when the room has one. Not the folder above the
# clone: that is the owner's folder and would allow every other repository in it.
function Get-DefaultFolders {
    param([string] $clone, [string] $worktreeRoot)
    $out = @()
    if ($clone) { $c = $clone.TrimEnd('/', '\'); $out += $c; $out += "$c-worktrees" }
    if ($worktreeRoot) { $out += $worktreeRoot.TrimEnd('/', '\') }
    @($out | Select-Object -Unique)
}

# Get-FolderScript is the script that runs the verb on the remote, for Invoke-Remote. Windows gets PowerShell, Unix
# gets sh, so the remote needs no pwsh. Each argument is quoted as a literal. $Bin is set here, to the place provision
# puts the binary, unless -Bin names another, so room-check (which has no preamble) can use it too. It prints what the
# verb prints and exits with the verb's code, or with 127 and a line saying atrium is not there.
function Get-FolderScript {
    param([ValidateSet('windows', 'linux', 'darwin', 'unix')] [string] $os, [string[]] $verbArgs, [string] $bin)
    # A TRAILING SLASH IS TRIMMED, because Windows PowerShell 5.1 passes a native argument that ends in a backslash
    # and holds a space (`C:\a b\`) with the quote eaten. A drive root keeps its slash, and those are refused anyway.
    $verbArgs = @($verbArgs | ForEach-Object {
        if ("$_" -match '^(/|[A-Za-z]:[\\/]|~)') { $t = "$_".TrimEnd('/', '\'); if ($t -and $t -notmatch '^[A-Za-z]:$') { $t } else { "$_" } } else { "$_" }
    })
    if ($os -eq 'windows') {
        $b = if ($bin) { "`$Bin = $(ConvertTo-PsLiteral $bin)" } else { "`$Bin = Join-Path `$HOME '.atrium\bin\atrium.exe'" }
        $q = ($verbArgs | ForEach-Object { ConvertTo-PsLiteral $_ }) -join ' '
        return "$b`n`$ErrorActionPreference = 'Continue'`n" +
            "if (-not (Test-Path -LiteralPath `$Bin)) { `"atrium is not at `$Bin`"; exit 127 }`n" +
            "& `$Bin room folders $q 2>&1 | ForEach-Object { `"`$_`" }`nexit `$LASTEXITCODE"
    }
    $b = if ($bin) { "Bin=$(ConvertTo-ShLiteral $bin)" } else { 'Bin="$HOME/.local/bin/atrium"' }
    $q = ($verbArgs | ForEach-Object { ConvertTo-ShLiteral $_ }) -join ' '
    "$b`nif [ ! -x `"`$Bin`" ]; then echo `"atrium is not at `$Bin`"; exit 127; fi`n" +
        "`"`$Bin`" room folders $q 2>&1`nexit `$?"
}

# Test-FolderVerbMissing says whether a failed run means THIS ATRIUM HAS NO `room folders` VERB: an older binary says
# unknown command, or unknown flag for `list --json`, and no binary at all is 127. It is not a failure of the room,
# it is an atrium too old to take a folder list. Anything else that failed (the room not answering) is not "missing".
function Test-FolderVerbMissing {
    param([int] $code, $lines)
    if ($code -eq 0) { return $false }
    if ($code -eq 127) { return $true }
    (@($lines) -join "`n") -match '(?i)unknown (command|subcommand|flag|shorthand flag)|no such command'
}

# ConvertFrom-FolderAllow reads what `folders allow` printed. A dir that ended allowed has an `allowed` or a `trusted`
# line, and a skipped one has its reason. Lines that are neither (a warning, a note) are left out here and the caller
# prints every line as it came.
function ConvertFrom-FolderAllow {
    param($lines)
    $r = foreach ($l in @($lines)) {
        $s = "$l".Trim()
        if ($s -match '^(allowed|trusted)\s+(.+)$') { [pscustomobject]@{ Kind = $Matches[1]; Dir = $Matches[2].Trim(); Reason = $null } }
        elseif ($s -match '^skipped\s+(.+?):\s*(.*)$') { [pscustomobject]@{ Kind = 'skipped'; Dir = $Matches[1].Trim(); Reason = $Matches[2].Trim() } }
    }
    @($r)
}

# ConvertFrom-FolderList reads `folders list --json`. Ok is false when the lines hold no such JSON.
function ConvertFrom-FolderList {
    param($lines)
    $text = (@($lines) -join "`n")
    $m = [regex]::Match($text, '\{.*\}', 'Singleline')
    $none = [pscustomobject]@{ Ok = $false; Roots = @(); Enforced = $false }
    if (-not $m.Success) { return $none }
    try { $j = $m.Value | ConvertFrom-Json } catch { return $none }
    if ($null -eq $j.enforced -and $null -eq $j.roots) { return $none }
    [pscustomobject]@{ Ok = $true; Roots = @($j.roots | Where-Object { $_ }); Enforced = [bool] $j.enforced }
}

# Format-FolderPath is a folder in the form two paths are compared in: forward slashes, no trailing slash.
function Format-FolderPath { param([string] $p) ($p -replace '\\', '/').TrimEnd('/') }

# Test-UnderFolders says whether a path is one of the roots or inside one. Windows and macOS compare without regard to
# case, as their file systems do by default. The room resolves symlinks before it compares, which this cannot, so a
# `false` here is a warning to the person and never a refusal.
function Test-UnderFolders {
    param([string] $path, $roots, [bool] $ignoreCase = $false)
    $p = Format-FolderPath $path
    foreach ($r in @($roots)) {
        $q = Format-FolderPath "$r"
        if (-not $q) { continue }
        $cmp = if ($ignoreCase) { [StringComparison]::OrdinalIgnoreCase } else { [StringComparison]::Ordinal }
        if ($p.Equals($q, $cmp) -or $p.StartsWith("$q/", $cmp)) { return $true }
    }
    $false
}

# Test-FolderRefusal reads the error a room gave for a launch outside its list: it has to name the allowed folders, or
# one of the roots. A refusal for some other reason (no such directory) is not the gate holding.
function Test-FolderRefusal {
    param([string] $text, $roots)
    if ($text -match '(?i)allowed folders|not in this room') { return $true }
    foreach ($r in @($roots)) { if ($r -and $text.Contains("$r")) { return $true } }
    $false
}
