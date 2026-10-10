## Test plan

## @LETTER@. A ticket card carries no ticket text

### @LETTER@1. Title, branch and tags come from the link

1. Edit the zendesk recogniser row so its title, branch and tags templates name a fetched fact, such as `{subject}`,
   and give it a fetch that prints one.
2. Run `atrium open https://netfoundry.zendesk.com/agent/tickets/<N>`.

**Expected:** the card is titled `zendesk-<N>`, its worktree is on branch `zendesk-<N>`, and its tags are `zendesk`,
`support`, `zendesk-<N>` and the link tag. None of the ticket's words appear on the card.
