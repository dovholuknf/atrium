# Does the room's logon task command survive a path with a typographic quote in it?  pwsh -File scripts/test-autostart-quote.ps1
#
# PowerShell ends a single-quoted string at U+2018 to U+201B as well as at the ASCII quote, and macOS autocorrects ' to
# them, so a path like "Clint’s atrium" must reach the room whole and run nothing else. The functions are lifted out of
# scripts/atrium-autostart.ps1 by AST (the script registers a task when it runs, so it cannot be dot-sourced), and the
# text they build is parsed and, off Windows, run against a fake atrium that records its arguments.

$ErrorActionPreference = 'Stop'
$script:failed = 0
function Check {
    param([string] $name, $got, $want)
    if (($got -join "`n") -eq ($want -join "`n")) { Write-Host "ok   $name" }
    else { Write-Host "FAIL $name`n     got:  $($got -join ' | ')`n     want: $($want -join ' | ')"; $script:failed++ }
}

$src = Join-Path $PSScriptRoot 'atrium-autostart.ps1'
$ast = [System.Management.Automation.Language.Parser]::ParseFile($src, [ref] $null, [ref] $null)
foreach ($fn in 'ConvertTo-PsLiteral', 'Get-RoomTaskCommand') {
    $def = $ast.Find({ param($n) $n -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -eq $fn }, $true)
    if (-not $def) { Write-Host "FAIL $fn is not in atrium-autostart.ps1"; exit 1 }
    Invoke-Expression $def.Extent.Text
}

$q1 = [string][char]0x2018; $q2 = [string][char]0x2019
$weird = @(
    "C:\Users\Clint${q2}s work\atrium.exe",
    "C:\Users\a${q2}; Write-Output INJECTED; ${q2}\atrium.exe",
    "C:\Users\it's\atrium.exe",
    'C:\Users\$x\atrium.exe',
    "C:\Users\${q1}x${q1}\atrium.exe")

foreach ($exe in $weird) {
    $text = Get-RoomTaskCommand -Exe $exe -Db "C:\Users\me${q2}s\atrium.db" -Addr '127.0.0.1:7777' -Http '127.0.0.1:7781'
    $errs = $null
    $parsed = [System.Management.Automation.Language.Parser]::ParseInput($text, [ref] $null, [ref] $errs)
    # Three statements, as written: the Join-Path assignment, the Test-Path if, the call. An injected statement is a fourth.
    Check "parses: $exe" $errs.Count 0
    Check "three statements: $exe" $parsed.EndBlock.Statements.Count 3
    $call = $parsed.EndBlock.Statements[2]
    $cmd = $call.PipelineElements[0]
    Check "the call's first word is the exe, whole: $exe" $cmd.CommandElements[0].Value $exe
}

# Off Windows, run the text against a fake that records what it was given.
if ($IsWindows -or $env:OS -eq 'Windows_NT') {
    Write-Host 'skip run: the fake is a shell script'
} else {
    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("autostart-quote-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
    New-Item -ItemType Directory -Force $tmp | Out-Null
    try {
        $rec = Join-Path $tmp 'args.txt'
        $dir = Join-Path $tmp "Clint${q2}s dir"
        New-Item -ItemType Directory -Force $dir | Out-Null
        $fake = Join-Path $dir 'atrium'
        Set-Content -LiteralPath $fake -Value "#!/bin/sh`nfor a in `"`$@`"; do printf '%s\n' `"`$a`"; done > '$rec'`n" -NoNewline
        & chmod +x $fake
        $db = Join-Path $tmp "db${q2}; Write-Output INJECTED; ${q2}.db"
        $text = Get-RoomTaskCommand -Exe $fake -Db $db -Addr '127.0.0.1:7777' -Http '127.0.0.1:7781'
        $enc = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($text))
        $out = & (Get-Command pwsh).Source -NoProfile -NonInteractive -EncodedCommand $enc 2>&1
        Check 'run: nothing was injected' (@($out | ForEach-Object { "$_" }) | Where-Object { $_ -match 'INJECTED' }).Count 0
        Check 'run: the arguments arrive whole' (Get-Content -LiteralPath $rec) @('room', '--detach', '--db', $db, '--agent', '127.0.0.1:7777', '--http', '127.0.0.1:7781')
    } finally {
        Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }
}

if ($script:failed) { Write-Host "`n$script:failed checks failed."; exit 1 }
Write-Host "`nall checks pass."
