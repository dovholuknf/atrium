# Runners with a newer version published, as atrium intake items.
#
# A source is a command atrium runs on a timer whose stdout is a JSON array of
# intake items. Atrium holds this script's path and an interval. The knowledge
# of WHICH REGISTRY a runner comes from lives here, in the script, which is the
# rule the whole source mechanism exists to keep: atrium drives claude, codex,
# ollama and bare shells as peers and learns nothing about any of them.
#
# Point a source row at this:
#
#   cmd:      pwsh
#   args:     ["-NoProfile", "-File", "<this file>"]
#   interval: 600
#
# THE DEDUPLICATION KEY IS THE POINT OF THIS ONE. `external_id` is the package
# and the version that is available, so a single card is raised for
# `@openai/codex@0.154.0` and every tick after that raises nothing. When 0.155.0
# is published the key changes and a new card appears. There is no state file
# and nothing to reset: the inbox already knows what it has been told.
#
# WHY AN INBOX CARD RATHER THAN A NOTIFICATION. Updating a runner is work. It
# takes a command, it takes the runner being idle, and on Windows it takes the
# running process to exit first, because an installer cannot overwrite a binary
# that is held open. That is a small job with a directory and a command, which
# is what a card is. The upgrade command is in the card's prompt, so pressing
# start gets an agent that already knows what to do, and leaving it in the inbox
# is a note that says it is out there.

[CmdletBinding()]
param(
    # Where the board is. Read rather than assumed, because the runners this
    # reports on are the ones this daemon is configured for, not a list written
    # in here that drifts the moment somebody adds a row.
    [string] $Board = $(if ($env:ATRIUM_BOARD_URL) { $env:ATRIUM_BOARD_URL } else { 'http://localhost:7778' })
)

$ErrorActionPreference = 'Stop'

# Which registry a runner's command comes from.
#
# KEYED BY COMMAND, not by harness id. An operator can name a row anything, can
# have three rows pointing at claude, and the thing that decides what is
# installed is the command. A runner that is not in here is skipped in silence:
# ollama ships as a platform installer and a shell has no version at all, and a
# source that reported "I cannot check this" every ten minutes would be worse
# than one that checked less.
$npmPackages = @{
    'claude' = '@anthropic-ai/claude-code'
    'codex'  = '@openai/codex'
}

# The first thing in the output that looks like a version.
#
# Every one of these prints its version differently and all of them print it
# somewhere: `2.1.270 (Claude Code)` and `codex-cli 0.153.2` are the two in
# front of us. Matching the number rather than parsing the sentence is what
# keeps this working when one of them rewords its banner.
function Get-Version([string] $text) {
    if ($text -match '(\d+\.\d+\.\d+)') { return $Matches[1] }
    return $null
}

# Compare two dotted versions as numbers.
#
# Not string comparison, which says 0.9.0 is newer than 0.10.0 and would raise
# a card telling you to downgrade. Anything that will not parse is treated as
# "no opinion" and reports nothing, because a source that guesses is a source
# that raises work nobody asked for.
function Test-Newer([string] $latest, [string] $installed) {
    try {
        return ([version] $latest) -gt ([version] $installed)
    } catch {
        return $false
    }
}

try {
    $harnesses = (Invoke-RestMethod -Uri "$Board/v1/harnesses" -TimeoutSec 10).harnesses
} catch {
    # The daemon being unreachable is a real failure and should land on the
    # source row rather than read as "nothing to report". Three of these in a
    # row switches the source off, which is correct: a source that cannot ask
    # the board what to check has nothing to do.
    Write-Error "could not read $Board/v1/harnesses: $($_.Exception.Message)"
    exit 1
}

$items = @()
$seen = @{}

foreach ($h in $harnesses) {
    if (-not $h.enabled) { continue }
    # Empty when nothing answered on PATH. A runner that is not installed has
    # no update to offer.
    if (-not $h.found) { continue }

    $cmd = ($h.cmd | Out-String).Trim().ToLower()
    $pkg = $npmPackages[$cmd]
    if (-not $pkg) { continue }
    # Three rows pointing at claude are one program, and one card.
    if ($seen.ContainsKey($pkg)) { continue }
    $seen[$pkg] = $true

    try {
        $installed = Get-Version (& $h.found --version 2>&1 | Out-String)
        $latest = (& npm view $pkg version 2>$null | Out-String).Trim()
    } catch {
        # One runner that cannot be asked does not sink the run. The others are
        # still worth reporting, and this one gets asked again in ten minutes.
        continue
    }

    if (-not $installed -or -not $latest) { continue }
    if (-not (Test-Newer $latest $installed)) { continue }

    $items += [ordered] @{
        source      = 'runner-update'
        external_id = "$pkg@$latest"
        title       = "$($h.label): $installed to $latest"
        why         = "a newer $($h.label) is published. updating replaces the binary, so " +
                      "the runner has to exit first and any card running it will go dead."
        tags        = @('runner-update', $cmd)
        # Pressing start gets an agent that already knows the command. Leaving
        # it in the inbox is the notification, which is the common case.
        prompt      = "run ``npm install -g $pkg`` and then report what ``$cmd --version`` says. " +
                      "nothing else."
    }
}

# Nothing to report is a normal answer and an empty stdout is how to say it.
# Atrium does not treat that as a failure.
if ($items.Count -eq 0) { return }

# `-AsArray` so one item is still a list. A bare object is accepted too, and
# emitting whichever shape happens to fall out is how a source works until the
# day it has exactly one thing to say.
$items | ConvertTo-Json -Depth 5 -AsArray
