# Issues assigned to you, as atrium intake items.
#
# A source is a command atrium runs on a timer whose stdout is a JSON array of
# intake items. Atrium holds this script's path and an interval. `gh` holds the
# token, in the keyring it already uses. Atrium never sees it, which is the rule
# in docs/intake-design.md and the reason a source is a command rather than an
# integration.
#
# Point a source row at this:
#
#   cmd:      pwsh
#   args:     ["-NoProfile", "-File", "<this file>", "-WorktreeRoot", "D:/worktrees"]
#   interval: 900
#
# Every field except source and external_id is optional. Those two are required
# because together they are the deduplication key: the same number is an issue
# in one tracker and a ticket in another.

[CmdletBinding()]
param(
    # Where worktrees live, used to suggest a directory. The directory does not
    # have to exist: an offered card can name somewhere nobody has made yet, and
    # that is most of why the inbox is separate from launching.
    [string] $WorktreeRoot = $env:WORKTREE_ROOT,

    # How many to report. A source reports what it can see and lets atrium work
    # out what is new, so this is a bound on one run, not a cursor.
    [int] $Limit = 30
)

$ErrorActionPreference = 'Stop'

# Anything on stdout that is not the JSON array is a parse failure atrium will
# report on the row, so progress chatter goes to stderr or nowhere.
$raw = & gh search issues --assignee '@me' --state open --limit $Limit `
    --json 'number,title,repository,url,body' 2>$null

if (-not $raw) {
    # Nothing to report is a normal answer, and an empty stdout is how to say
    # it. Atrium does not treat that as a failure.
    return
}

$items = foreach ($issue in ($raw | ConvertFrom-Json)) {
    $repo = $issue.repository.nameWithOwner
    $slug = "$repo#$($issue.number)"
    $dir  = if ($WorktreeRoot) {
        Join-Path $WorktreeRoot "github/$repo/issue-$($issue.number)"
    } else { '' }

    # An engineering item names a repo and therefore a directory, so the card
    # can be fully prepared and the prompt can be imperative. A support case
    # cannot, which is what the other script in here is about.
    [pscustomobject]@{
        source        = 'github'
        external_id   = $slug
        url           = $issue.url
        title         = "$($issue.number) $($issue.title)"
        why           = "assigned to me. $repo"
        tags          = @('issue', $repo)
        suggested_cwd = ($dir -replace '\\', '/')
        prompt        = "Work on $slug : $($issue.title). Read it with " +
                        "``gh issue view $($issue.number) --repo $repo``, then " +
                        "tell me what you think the fix is before you start writing it."
    }
}

# Always an array, even for one item, so the shape does not change with the
# count. Depth because the tags list nests.
@($items) | ConvertTo-Json -Depth 5 -AsArray
