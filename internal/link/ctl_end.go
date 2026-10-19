package link

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// atrium_done and atrium_blocked: how a worker ends its work, in a fixed format.
//
// SHORT AND NOTHING ELSE. `done` takes a commit, `blocked` takes a reason of up to 50 words, and both go to the
// launcher exactly as `atrium_report` goes, through the room's report endpoint with `ended` set. Atrium sends the card
// no nudge after either, because a told report ends what is owed (store.Task.OwesReport) and a blocked one moves its
// item to reported as a done one does. The room checks the commit against the card's own directory, then the hub, and
// records nothing when neither has it. See internal/daemon/finish.go. ONE CODE for the hub's control and the stdio one.

// EndReasonMax is the longest reason atrium_blocked takes, in words.
const EndReasonMax = 50

// EndLineMax is the longest line atrium_done takes after the sha, in words.
const EndLineMax = 20

// endSHAShape is what a commit id looks like: seven to forty hex characters.
var endSHAShape = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

const doneToolDesc = "End your work: it is finished. THIS IS HOW TO END. Nothing else is needed. Give exactly one of " +
	"`sha` or `artifact`: sha or artifact location. `sha` is the commit the work landed as (7 to 40 hex " +
	"characters, and it has to exist in your directory or on any hub branch, or the call is refused and nothing is " +
	"recorded). `artifact` is a path or URL to what you made, for work with no commit, and a path has to exist on " +
	"your own room. Details go in REPORT.md, not here. Never end a turn while a command you started is still running. " +
	"Stop after the call: say nothing more and do no more work. Write nothing else: the human sees the tool call. " +
	"Answer a launcher's stop with this and your last sha."

const blockedToolDesc = "End your work: something stops you and you cannot go on. THIS IS HOW TO END. " +
	"Nothing else is needed. Give `reason`, what stops you and what you need, in up to 50 words. A longer reason is " +
	"refused, not cut. Details go in REPORT.md, not here. Never end a turn while a command you started is still running. " +
	"Stop after the call: say nothing more and do no more work. Write nothing else: the human sees the tool call."

// What a resident card hears from the same two tools: a task is reported and the card stays open.
const residentDoneToolDesc = "Report that this task is finished. Your card stays open: after the call, say nothing more " +
	"and wait for the next message. Give exactly one of sha or artifact, and an optional line of up to 20 words. " +
	"Details go in REPORT.md. In a pool, `learned` is one paragraph for the next item: atrium appends it to LEARNINGS.md."

const residentBlockedToolDesc = "Report that something stops this task, in up to 50 words. Your card stays open: " +
	"after the call, say nothing more and wait for the next message."

// What an item worker hears from the same two tools: the same ending with no launcher in the words.
const itemDoneToolDesc = "End your work: it is finished. THIS IS HOW TO END. Nothing else is needed. Give exactly one of " +
	"`sha` or `artifact`: sha or artifact location. `sha` is the commit the work landed as (7 to 40 hex " +
	"characters, and it has to exist in your directory or on any hub branch, or the call is refused and nothing is " +
	"recorded). `artifact` is a path or URL to what you made, for work with no commit, and a path has to exist on " +
	"your own room. Details go in REPORT.md, not here. Never end a turn while a command you started is still running. " +
	"Stop after the call: say nothing more and do no more work. Write nothing else: the human sees the tool call. " +
	"If you are asked to stop, answer with this and your last sha."

const itemBlockedToolDesc = "End your work: something stops you and you cannot go on. THIS IS HOW TO END. " +
	"Nothing else is needed. Give `reason`, what stops you and what you need, in up to 50 words. A longer reason is " +
	"refused, not cut. Details go in REPORT.md, not here. Never end a turn while a command you started is still running. " +
	"Stop after the call: say nothing more and do no more work. Write nothing else: the human sees the tool call."

type doneInput struct {
	SHA      string `json:"sha,omitempty" jsonschema:"sha or artifact location. the commit the work landed as: 7 to 40 hex characters"`
	Artifact string `json:"artifact,omitempty" jsonschema:"sha or artifact location. a path or URL to what you made, when there is no commit"`
	Line     string `json:"line,omitempty" jsonschema:"optional: one line, up to 20 words, shown after the sha"`
	Learned  string `json:"learned,omitempty" jsonschema:"optional, for a card in a pool: one paragraph about the code and your own misses that the next item should know. atrium appends it to LEARNINGS.md"`
	Why      string `json:"why,omitempty" jsonschema:"optional, for a task of an item: why you did it the way you did, up to 1000 bytes. the owner reads it beside the result"`
}

// LearnedMax is the longest `learned` atrium_done takes, in bytes: one paragraph. The room's is the same, and cuts.
const LearnedMax = 1500

// WhyMax is the longest `why` atrium_done takes, in bytes.
const WhyMax = 1000

type blockedInput struct {
	Reason string `json:"reason" jsonschema:"what stops you, in up to 50 words"`
}

// HeldNote is what a worker is told when its report was kept on its launcher's card and not delivered.
const HeldNote = "held on your launcher's card, not delivered. it reads its notices when it asks, " +
	"so do not say it has been told."

// QueuedNote is what a worker is told when its ending is in its launcher's queue and not yet typed.
const QueuedNote = "queued for your launcher. it reads it at its next tool call or when its turn ends. stop now."

// Told is the room's launcher_told: "queued" when the report is in the launcher's queue and will be typed at its turn
// end, "relayed" when it went to another room's launcher, "held" when it was kept on the launcher's card for it to
// read, true as an older room answered for a queued one, and false when nobody was told. A worker that read `true` for a held report said
// "the orchestrator has been told", and nobody had. See daemon/finish.go.
type Told string

const (
	ToldNobody Told = ""
	ToldYes    Told = "true"
	ToldHeld   Told = "held"
	// ToldQueued and ToldRelayed are what finish answers now: true is never answered for a queue row.
	ToldQueued  Told = "queued"
	ToldRelayed Told = "relayed"
)

// UnmarshalJSON takes the bool older rooms answer as well as "held".
func (t *Told) UnmarshalJSON(b []byte) error {
	switch s := strings.Trim(string(b), `" `); s {
	case "true":
		*t = ToldYes
	case "held":
		*t = ToldHeld
	case "queued":
		*t = ToldQueued
	case "relayed":
		*t = ToldRelayed
	default:
		*t = ToldNobody
	}
	return nil
}

// Any is whether anybody was told, held or not.
func (t Told) Any() bool { return t != ToldNobody }

// Value is what a tool answers: "queued", "relayed", "held", true or false.
func (t Told) Value() any {
	switch t {
	case ToldHeld, ToldQueued, ToldRelayed:
		return string(t)
	}
	return t == ToldYes
}

// EndOutput is what either tool answers.
type EndOutput struct {
	Recorded     bool   `json:"recorded"`
	Status       string `json:"status"`
	LauncherTold any    `json:"launcher_told"`
	Note         string `json:"note,omitempty"`
}

// CheckDoneSHA is why a commit id is refused, or "".
func CheckDoneSHA(sha string) string {
	if !endSHAShape.MatchString(strings.TrimSpace(sha)) {
		return fmt.Sprintf("sha %q is not a commit id: 7 to 40 hex characters", strings.TrimSpace(sha))
	}
	return ""
}

// CheckBlockedReason is why a reason is refused, or "": 1 to 50 words, split on whitespace. Newlines are fine.
func CheckBlockedReason(reason string) string {
	n := len(strings.Fields(reason))
	switch {
	case n == 0:
		return "say why you are blocked, in up to 50 words"
	case n > EndReasonMax:
		return fmt.Sprintf("the reason is %d words, over %d. shorten it, details go in REPORT.md", n, EndReasonMax)
	}
	return ""
}

// EndDoor is the two tools for a server, given how each reaches the room's report endpoint.
type EndDoor struct {
	// Report files the body as the caller's report and answers what the room recorded.
	Report func(ctx context.Context, req *mcp.CallToolRequest, body map[string]any) (EndOutput, error)
}

// Done is atrium_done.
func (d EndDoor) Done(ctx context.Context, req *mcp.CallToolRequest, in doneInput) (
	*mcp.CallToolResult, EndOutput, error) {

	sha, artifact := strings.TrimSpace(in.SHA), strings.TrimSpace(in.Artifact)
	line := strings.Join(strings.Fields(in.Line), " ")
	if n := len(strings.Fields(line)); n > EndLineMax {
		return nil, EndOutput{}, fmt.Errorf("the line is %d words, over %d. details go in REPORT.md", n, EndLineMax)
	}
	learned := strings.Join(strings.Fields(in.Learned), " ")
	if len(learned) > LearnedMax {
		return nil, EndOutput{}, fmt.Errorf("learned is %d bytes, over %d. one paragraph", len(learned), LearnedMax)
	}
	why := strings.Join(strings.Fields(in.Why), " ")
	if len(why) > WhyMax {
		return nil, EndOutput{}, fmt.Errorf("why is %d bytes, over %d. shorten it, details go in REPORT.md", len(why), WhyMax)
	}
	tail := ""
	if line != "" {
		tail = ": " + line
	}
	switch {
	case sha != "" && artifact != "":
		return nil, EndOutput{}, fmt.Errorf("give sha or artifact, not both")
	case sha == "" && artifact == "":
		return nil, EndOutput{}, fmt.Errorf("give sha (the commit) or artifact (a path or URL to what you made)")
	case artifact != "":
		out, err := d.Report(ctx, req, map[string]any{
			"status": "done", "recap": "done " + artifact + tail, "artifact": artifact, "ended": true, "learned": learned, "why": why,
		})
		return nil, out, err
	}
	if why := CheckDoneSHA(sha); why != "" {
		return nil, EndOutput{}, fmt.Errorf("%s", why)
	}
	out, err := d.Report(ctx, req, map[string]any{
		"status": "done", "recap": "done " + sha + tail, "sha": sha, "ended": true, "learned": learned, "why": why,
	})
	return nil, out, err
}

// Blocked is atrium_blocked.
func (d EndDoor) Blocked(ctx context.Context, req *mcp.CallToolRequest, in blockedInput) (
	*mcp.CallToolResult, EndOutput, error) {

	if why := CheckBlockedReason(in.Reason); why != "" {
		return nil, EndOutput{}, fmt.Errorf("%s", why)
	}
	out, err := d.Report(ctx, req, map[string]any{
		"status": "blocked", "ask": strings.TrimSpace(in.Reason), "ended": true,
	})
	return nil, out, err
}

// AddEndTools puts both tools on a server that has no class of its own (the stdio one).
func AddEndTools(s *mcp.Server, d EndDoor) {
	mcp.AddTool(s, &mcp.Tool{Name: "atrium_done", Description: doneToolDesc}, d.Done)
	mcp.AddTool(s, &mcp.Tool{Name: "atrium_blocked", Description: blockedToolDesc}, d.Blocked)
}
