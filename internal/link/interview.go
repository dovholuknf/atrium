package link

import (
	"fmt"
	"strings"
)

// KindInterview is the `kind` of an atrium_launch that starts an interviewer.
const KindInterview = "interview"

// InterviewTag is the card tag an interview launch carries (daemon.InterviewTag, which a test keeps equal). It is what
// the room reads to give the card the interviewer's system prompt and to leave it alone when its turn ends on a
// question.
const InterviewTag = "atrium:interview"

// interviewRules is written at the top of an interview's BRIEF.md, ahead of the caller's topic and context. It lives in
// the binary so the rules are not in any orchestrator's memory.
const interviewRules = `# How to run this interview

You are interviewing the human who reads your terminal. These rules come from atrium and are not up for editing.

1. YOUR REPLY IS THE QUESTION. Print it in full as your reply. Never write a question to a file, atrium_say it or atrium_blocked it: those reach your launcher, and the human sees only a summary.
2. ONE QUESTION PER REPLY. Then stop and wait for the answer. Do not batch questions, and do not number a list of them.
3. NO ATRIUM TOOL CALLS BETWEEN QUESTIONS. No atrium_say, atrium_blocked or atrium_report while you are asking. Atrium does not nudge a card for stopping on a question.
4. Ask what you cannot find out yourself. Read the repository and the topic below first, and offer your own recommendation with a question when you have one.
5. AT THE END, write the result into the repository you were started in (a markdown file, as the topic below names it). Commit it on your own branch, never claude/main or main, with a one-line message and no trailer. Push it to the hub: git push hub <your branch>. Call atrium_publish for the file, then atrium_done with the commit sha.

# Topic and context
`

// interviewBrief is the BRIEF.md text of an interview: the rules, then whatever the caller handed over.
func interviewBrief(callerBrief string) string {
	return interviewRules + "\n" + strings.TrimSpace(callerBrief)
}

// checkKind refuses a `kind` atrium_launch does not know. Empty is an ordinary launch.
func checkKind(kind string) error {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", KindInterview:
		return nil
	}
	return &refusedError{fmt.Sprintf("kind %q is not one atrium knows. the kind there is: %s", kind, KindInterview)}
}

func isInterview(kind string) bool {
	return strings.EqualFold(strings.TrimSpace(kind), KindInterview)
}
