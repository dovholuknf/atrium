package link

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The audit line for a mutating control call.
//
// ONE WRAPPER, APPLIED AT REGISTRATION. `server()` wraps each mutating tool in
// `audited`, so the list of what is audited reads in one place and a new
// mutating tool is covered by wrapping it there, not by remembering to call
// `c.audit` from inside it.
//
// THE LINE RECORDS A CLAIM, NEVER A PROOF. `X-Atrium-Agent` is a header anybody
// on loopback can set, so every line says `(claimed)`.
//
// THE ROOM IS THE TARGET, not the caller's. A cross-room call writes on the room
// it acted on and names the caller's room in the text, with no second copy.
//
// NEVER A PAYLOAD. The audit log is shown on the board and a prompt, a brief or a
// message can hold anything, so a line carries ids, handles, room names, branch
// names and the outcome. A describe function is where that is decided, and it
// never reads a text field.
//
// BEST EFFORT. A nil `audit` or a write that fails never changes the tool's
// answer: the wrapper returns exactly what the handler returned.

// auditErrMax bounds the outcome a failed call writes.
const auditErrMax = 200

// auditWhatMax bounds the "what" of a line, which carries caller text such as an
// alias or a branch name.
const auditWhatMax = 200

// refusedError is a call turned away by a rule rather than one that failed, such
// as the launch cap. The audit line says `refused:` for it, which is the line
// somebody looks for, and a plain error is written as it came.
type refusedError struct{ msg string }

func (e *refusedError) Error() string { return e.msg }

// auditDescribe turns one call into the two things a line needs: the target room
// and a short "what". record false writes nothing, for a call that turned out to
// be a read.
type auditDescribe[In, Out any] func(req *mcp.CallToolRequest, in In, out Out) (room, what string, record bool)

// audited wraps a tool handler so one line is recorded after it returns, whether
// it succeeded, was refused or failed.
func audited[In, Out any](c *controlMCP, kind string, describe auditDescribe[In, Out],
	h func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error),
) func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error) {

	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		res, out, err := h(ctx, req, in)
		if c.audit == nil {
			return res, out, err
		}
		if room, what, ok := describe(req, in, out); ok {
			c.audit(room, kind, auditDetail(req, what, err))
		}
		return res, out, err
	}
}

// auditDetail is `by <agent>@<caller room> (claimed): <what>, <outcome>`.
func auditDetail(req *mcp.CallToolRequest, what string, err error) string {
	// The agent and room come from headers anybody on loopback can set, so they
	// are cleaned like the text is.
	by := auditWhat(agentOf(req))
	if by == "" {
		by = "unnamed"
	}
	if r := auditWhat(roomOf(req)); r != "" {
		by += "@" + r
	}
	return "by " + by + " (claimed): " + auditWhat(what) + ", " + auditOutcome(err)
}

// auditWhat strips control characters from what, so caller text cannot start a
// second line, and cuts it to auditWhatMax characters. Besides C0, DEL and C1 it
// strips the line and paragraph separators and the bidi controls, which a log
// viewer uses to reorder the text around them.
func auditWhat(what string) string {
	what = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r), r == '\u2028', r == '\u2029',
			r >= '\u202a' && r <= '\u202e', r >= '\u2066' && r <= '\u2069':
			return -1
		}
		return r
	}, what)
	if r := []rune(what); len(r) > auditWhatMax {
		what = string(r[:auditWhatMax])
	}
	return what
}

// auditOutcome is `ok`, `refused: <why>` or the error's first line, cut to
// auditErrMax characters.
func auditOutcome(err error) string {
	if err == nil {
		return "ok"
	}
	msg := strings.TrimSpace(err.Error())
	if i := strings.IndexAny(msg, "\r\n"); i >= 0 {
		msg = strings.TrimSpace(msg[:i])
	}
	var ref *refusedError
	if errors.As(err, &ref) {
		msg = "refused: " + msg
	}
	if r := []rune(msg); len(r) > auditErrMax {
		msg = string(r[:auditErrMax])
	}
	return msg
}

// cardRoom is the room a card address acts on: the room it names when that is
// not the caller's own, else the caller's.
func cardRoom(req *mcp.CallToolRequest, addr string) string {
	own := roomOf(req)
	if _, target, err := SplitAddress(addr); err == nil {
		if other := otherRoom(target, own); other != "" {
			return other
		}
	}
	return own
}

// namedOr is the card as the answer named it, else as the caller asked for it.
func namedOr(got, asked string) string {
	if got != "" {
		return got
	}
	return strings.TrimSpace(asked)
}

func describeLaunch(req *mcp.CallToolRequest, in launchInput, out launchOutput) (string, string, bool) {
	room := roomOf(req)
	if r := strings.TrimSpace(in.Room); r != "" {
		room = r
	}
	harness := strings.TrimSpace(in.Runner)
	if harness == "" {
		harness = "claude"
	}
	what := "launch " + harness
	if out.Card != "" {
		what += " as " + out.Card
	}
	return room, what, true
}

func describeExit(req *mcp.CallToolRequest, in exitInput, out exitOutput) (string, string, bool) {
	return cardRoom(req, in.Card), "exit " + namedOr(out.Card, in.Card), true
}

// describeModel records the card and the model asked for. The model is a short
// identifier, never free text.
func describeModel(req *mcp.CallToolRequest, in modelInput, out modelOutput) (string, string, bool) {
	model := []rune(strings.TrimSpace(in.Model))
	if len(model) > 80 {
		model = model[:80]
	}
	return cardRoom(req, in.Card), "model " + namedOr(out.Card, in.Card) + " to " + string(model), true
}

func describeCull(req *mcp.CallToolRequest, in cullInput, out cullOutput) (string, string, bool) {
	verb := "cull "
	if in.Hold {
		verb = "hold "
	}
	what := verb + namedOr(out.Card, in.Card)
	if into := strings.TrimSpace(in.Into); into != "" && !in.Hold {
		what += " into " + into
	}
	return cardRoom(req, in.Card), what, true
}

func describeRestart(req *mcp.CallToolRequest, in restartInput, _ restartOutput) (string, string, bool) {
	what := "restart"
	if in.Force {
		what += " (force)"
	}
	return roomOf(req), what, true
}

func describeWake(req *mcp.CallToolRequest, in wakeInput, out wakeOutput) (string, string, bool) {
	what := "wake queued for own card"
	if in.Clear {
		what = "wake cleared for own card"
	}
	if out.Card != "" {
		what += " " + out.Card
	}
	return roomOf(req), what, true
}

// describeAlias records a set or a clear. A read writes nothing.
func describeAlias(req *mcp.CallToolRequest, in aliasInput, out aliasOutput) (string, string, bool) {
	set := strings.TrimSpace(in.Alias)
	if set == "" && !in.Clear {
		return "", "", false
	}
	who := namedOr(out.Card, in.Card)
	if who == "" {
		who = "own card"
	}
	what := "alias set to " + set + " on " + who
	if in.Clear {
		what = "alias cleared on " + who
	}
	// The room of the card it names, like exit and cull, so a cross-room alias
	// is written where the card is.
	room := roomOf(req)
	if strings.TrimSpace(in.Card) != "" {
		room = cardRoom(req, in.Card)
	}
	return room, what, true
}

// describeSay records only a say that wakes a parked session, which costs a cold
// start. A plain say is the busiest call there is and the room keeps its own log.
func describeSay(req *mcp.CallToolRequest, in sayInput, out sayOutput) (string, string, bool) {
	if !in.Wake {
		return "", "", false
	}
	return cardRoom(req, in.To), "say with wake to " + namedOr(out.To, in.To), true
}
