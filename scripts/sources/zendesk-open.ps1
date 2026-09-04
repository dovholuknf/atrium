# Open support tickets, as atrium intake items.
#
# READ THIS BEFORE USING IT. A support case is not an engineering item wearing a
# different badge, and this script is deliberately worse at describing its items
# than github-assigned.ps1 is.
#
# docs/intake-design.md, "Engineering and support are not the same shape", has
# the argument. The short version, and the two rules it produces:
#
# 1. A support case names a customer and a symptom. It does not name a repo, so
#    there is no directory to suggest and no branch to derive. An item from here
#    arrives with no suggested_cwd on purpose, and cannot be started until a
#    human answers the one question the ticket does not: where does this work
#    happen. That is what the inbox is for.
#
# 2. A support case carries somebody else's data. The fields it would naturally
#    fill are title and why, and those land in a database with no encryption,
#    behind a board with no login, reachable from another machine the moment a
#    share is up. None of those three is a flaw. Together they mean this is a
#    fine place for "what was I doing" and a poor place for a customer's
#    hostnames.
#
#    So: the identifier and the URL, and as little prose as gets the job done.
#    The subject line is NOT copied, even though it is right there and would
#    make a nicer card. The agent reads the ticket through its own tools four
#    seconds later, in a session whose transcript is subject to whatever you
#    already decided about transcripts, and the customer's words never enter
#    atrium's database at all.
#
# The prompt is interrogative rather than imperative, for the same reason: the
# first job on a support case is working out what the work is.

[CmdletBinding()]
param(
    [string] $ZendeskHost = $env:ZENDESK_HOST,
    [int]    $Limit = 25
)

$ErrorActionPreference = 'Stop'

if (-not $ZendeskHost) {
    # A source that cannot work says so on stderr and exits non-zero. Atrium
    # puts the reason on the row and switches it off after three tries.
    Write-Error 'ZENDESK_HOST is not set, so there is no instance to ask.'
    exit 1
}

# There is no `zd` CLI the way there is a `gh`, so this reads ids from wherever
# you already have them. Replace this block with whatever your instance gives
# you: a saved view export, a curl through a credential helper, or a call to
# mcp-gateway on loopback, which already has zendesk wired.
#
# Whatever it is, it holds the credential and atrium holds this path.
$ticketIds = @()
if ($env:ATRIUM_ZENDESK_IDS) {
    $ticketIds = $env:ATRIUM_ZENDESK_IDS -split '[,\s]+' | Where-Object { $_ }
}
if (-not $ticketIds) { return }

$items = foreach ($id in ($ticketIds | Select-Object -First $Limit)) {
    [pscustomobject]@{
        source      = 'zendesk'
        external_id = "$id"
        url         = "https://$ZendeskHost/agent/tickets/$id"
        # The identifier, not the subject. See the header.
        title       = "zendesk-$id"
        why         = 'support case, unread'
        # The shared tag is the whole relationship between this triage card and
        # whatever engineering cards come out of it. Neither zendesk nor github
        # models that well and atrium deliberately does not try.
        tags        = @('zendesk', 'support', "zendesk-$id")
        # No suggested_cwd. Nothing in the ticket says which repo.
        prompt      = "Read zendesk ticket $id at https://$ZendeskHost/agent/tickets/$id . " +
                      "Summarize it, list any attachments and tell me whether there are any, " +
                      "then tell me which repository you think this is about and why. " +
                      "Do not draft a reply to the customer yet."
    }
}

@($items) | ConvertTo-Json -Depth 5 -AsArray
