package daemon

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dovholuknf/atrium/internal/api"
)

// InterviewTag marks a card launched with `kind: "interview"`. The interviewer's turn ends are its QUESTIONS to a
// human, so atrium never nudges it for stopping without a report, never calls it stuck for that, and never raises the
// launch-idle notice for it. It also gets the interviewer's system prompt, in place of the worker's. The hub puts it on
// (link.InterviewTag), and a test keeps the two equal.
const InterviewTag = "atrium:interview"

// ReviewTag marks a card launched with `kind: "review"`. Its turn ends are the human's to answer in the review view or
// in chat, so atrium never nudges it for stopping without a report and never raises the launch-idle notice for it.
const ReviewTag = api.ReviewTag

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
	_, why := agentCheckoutBranch(cwd)
	return why
}

// agentCheckoutBranch is the claude/* branch cwd is on, or why it is not on one.
func agentCheckoutBranch(cwd string) (branch, why string) {
	branch, err := gitIn(cwd, "symbolic-ref", "--short", "-q", "HEAD")
	switch {
	case err != nil && !isGitCheckout(cwd):
		return "", fmt.Sprintf("%s is not a git checkout, so what the agent writes there is not kept in git", cwd)
	case err != nil || branch == "":
		return "", fmt.Sprintf("%s is a git checkout that is not on a branch, so what the agent commits there is not on a claude/* branch", cwd)
	case !strings.HasPrefix(branch, "claude/"):
		return "", fmt.Sprintf("%s is on %s, not a claude/* branch, so the agent's work would not be kept on one", cwd, branch)
	}
	return branch, ""
}

func isGitCheckout(dir string) bool {
	_, err := gitIn(dir, "rev-parse", "--git-dir")
	return err == nil
}

// PromptToolNames is every atrium_* tool the lean worker prompt and the interviewer prompt name, sorted. A test on each
// control path keeps the set a lean worker is served a superset of it.
func PromptToolNames() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range []string{leanSystemPrompt, interviewSystemPrompt} {
		for _, w := range strings.FieldsFunc(p, func(r rune) bool {
			return !(r == '_' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
		}) {
			if strings.HasPrefix(w, "atrium_") && !seen[w] {
				seen[w] = true
				out = append(out, w)
			}
		}
	}
	sort.Strings(out)
	return out
}
