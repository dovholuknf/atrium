package daemon

import (
	"fmt"
	"strings"
)

// InterviewTag marks a card launched with `kind: "interview"`. The interviewer's turn ends are its QUESTIONS to a
// human, so atrium never nudges it for stopping without a report, never calls it stuck for that, and never raises the
// launch-idle notice for it. It also gets the interviewer's system prompt, in place of the worker's. The hub puts it on
// (link.InterviewTag), and a test keeps the two equal.
const InterviewTag = "atrium:interview"

// DefaultInterviewModel is the model an interview gets when the launch names none and worker_policy sets no
// interview_model.
const DefaultInterviewModel = "opus"

// interviewSystemPrompt is what an interviewer is told in place of leanSystemPrompt, which says a plain reply reaches
// nobody. For an interviewer the plain reply IS the question: the human reads the terminal.
const interviewSystemPrompt = `You are an interviewer launched through atrium. Your BRIEF.md holds the rules and the topic. It is your whole task.
- Your reply IS the question. The human reads your terminal, so print the question in full as the reply. Never put a question in a file, in atrium_say or in atrium_blocked: they reach your launcher, not the human.
- One question per reply. Then stop and wait for the answer.
- Make no atrium tool calls between questions.
- At the end write the result into the repository, commit it on your own branch (never claude/main or main), push it to the hub, call atrium_publish, then atrium_done with the commit sha.
- Commit messages: one short subject line. No body unless asked, no Co-Authored-By or other trailer.
- Never restart atrium, the hub or a room, and never deploy.
- Do not edit CLAUDE.md files.`

// agentCheckoutRefusal is why an agent launch may not start in cwd, or "" when it may. Agent output belongs in git: a
// directory that is not a checkout on a claude/* branch is where an interview writeup was lost, so the launch is
// refused unless the caller says `scratch: true`.
func agentCheckoutRefusal(cwd string) string {
	branch, err := gitIn(cwd, "symbolic-ref", "--short", "-q", "HEAD")
	switch {
	case err != nil && !isGitCheckout(cwd):
		return fmt.Sprintf("%s is not a git checkout, so what the agent writes there is not kept in git", cwd)
	case err != nil || branch == "":
		return fmt.Sprintf("%s is a git checkout that is not on a branch, so what the agent commits there is not on a claude/* branch", cwd)
	case !strings.HasPrefix(branch, "claude/"):
		return fmt.Sprintf("%s is on %s, not a claude/* branch, so the agent's work would not be kept on one", cwd, branch)
	}
	return ""
}

func isGitCheckout(dir string) bool {
	_, err := gitIn(dir, "rev-parse", "--git-dir")
	return err == nil
}
