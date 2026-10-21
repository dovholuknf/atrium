package link

import "strings"

// KindReview is the `kind` of an atrium_launch that starts a slop review of the branch in its directory.
const KindReview = "review"

// ReviewTag is the card tag a review launch carries (daemon.ReviewTag, which a test keeps equal). The room reads it so
// the card is not nudged when its turn ends, and the hub reads it to serve atrium_review to this card alone.
const ReviewTag = "atrium:review"

// reviewRules is written at the top of a review's BRIEF.md. It is the judgment half of a slop review: what to look for
// and what to do with an answer. How a finding looks is atrium's, the board draws it from atrium_review.
const reviewRules = `# How to run this review

You are reviewing the branch checked out in this directory for slop, with a human who reads the review view on your
card. These rules come from atrium and are not up for editing. You never commit.

1. START with atrium_review action start and the base below. It answers the merge-base and the changed files with
   their line counts. Review the diff against that merge-base and nothing else.
2. FIND. Walk the whole diff and tag each candidate with a category:
   - SCOPE: a change the task did not ask for.
   - DEAD: code, files or exports nothing uses.
   - DRY: a copy of something that already exists, or a helper that should be shared.
   - CHURN: a rename, a reorder or a reformat that changes no behaviour.
   - HYGIENE: debug leftovers, stray files, generated output, secrets.
   - TEST SCOPE: a test that checks more than the change, or less than it claims.
   - COMMENT: a comment that narrates the change, repeats the code or lies.
   - SIMPLIFY: the same behaviour in fewer moving parts.
3. TRIAGE. A no-brainer is a change nobody would argue with, such as a debug print or an unused import. Send those
   together with atrium_review action batch, each with its edits. They are applied at once and the human may veto any.
   Order the rest: code before comments, a helper before its callers.
4. PRESENT ONE FINDING AT A TIME with action finding: cat, kind (code, comment or test), site (path:line), a short
   title, the edits, one or two lines of note on the trade-off, and a footer of two lines. Edits are {path, old, new}
   like an Edit call, and old must appear in the file exactly once. The footer is CHANGE <CAT> then SUMMARY, or
   SUMMARY <CAT> then QUESTION. Then stop and wait for the human's answer in chat. Never print a diff, a file or an
   explanation of the diff: atrium measures it and draws it.
5. DECIDE. The answer names a finding number. On yes call action apply with that number, to that finding only. On no
   call action close with state kept. When a finding turns out wrong, call close with state withdrawn and the reason.
   A yes that comes with a criticism is a new finding, not an apply of the old one.
6. NEVER apply a finding the human has not answered, never apply from memory, and never edit a file yourself.
7. WHEN THE FINDINGS ARE DONE build with the repo's own script and run its tests. Then end with atrium_done, saying
   what was applied, kept and withdrawn, and with drafted commit lines for the human. You do not commit.

# Base and context
`

// reviewBrief is the BRIEF.md text of a review: the rules, the base, then whatever the caller handed over.
func reviewBrief(base, callerBrief string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "claude/main"
	}
	out := reviewRules + "\nBase branch: " + base + "\n"
	if c := strings.TrimSpace(callerBrief); c != "" {
		out += "\n" + c + "\n"
	}
	return out
}

// ReviewBrief is reviewBrief for a caller outside this package, the launch command.
func ReviewBrief(base, callerBrief string) string { return reviewBrief(base, callerBrief) }

func isReview(kind string) bool {
	return strings.EqualFold(strings.TrimSpace(kind), KindReview)
}
