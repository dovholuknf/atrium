package ctl

import (
	"context"
	"errors"
	"strings"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// AuditErrMax bounds the outcome a failed call writes.
const AuditErrMax = 200

// AuditWhatMax bounds the "what" of a line, which carries caller text such as an
// alias or a branch name.
const AuditWhatMax = 200

// AuditDescribe turns one call into the two things a line needs: the target room
// and a short "what". record false writes nothing, for a call that turned out to
// be a read.
type AuditDescribe[In, Out any] func(req *mcp.CallToolRequest, in In, out Out) (room, what string, record bool)

// Audited wraps a tool handler so one line is recorded after it returns, whether
// it succeeded, was refused or failed.
func Audited[In, Out any](c Caller, kind string, describe AuditDescribe[In, Out],
	h func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error),
) func(context.Context, *mcp.CallToolRequest, In) (*mcp.CallToolResult, Out, error) {

	return func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		res, out, err := h(ctx, req, in)
		if room, what, ok := describe(req, in, out); ok {
			c.Audit(room, kind, AuditDetail(req, what, err))
		}
		return res, out, err
	}
}

// AuditDetail is `by <agent>@<caller room> (claimed): <what>, <outcome>`.
func AuditDetail(req *mcp.CallToolRequest, what string, err error) string {
	// The agent and room come from headers anybody on loopback can set, so they
	// are cleaned like the text is.
	by := AuditWhat(AgentOf(req))
	if by == "" {
		by = "unnamed"
	}
	if r := AuditWhat(RoomOf(req)); r != "" {
		by += "@" + r
	}
	return "by " + by + " (claimed): " + AuditWhat(what) + ", " + AuditOutcome(err)
}

// auditWhat strips control characters from what, so caller text cannot start a
// second line, and cuts it to AuditWhatMax characters. Besides C0, DEL and C1 it
// strips the line and paragraph separators and the bidi controls, which a log
// viewer uses to reorder the text around them.
func AuditWhat(what string) string {
	what = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r), r == '\u2028', r == '\u2029',
			r >= '\u202a' && r <= '\u202e', r >= '\u2066' && r <= '\u2069':
			return -1
		}
		return r
	}, what)
	if r := []rune(what); len(r) > AuditWhatMax {
		what = string(r[:AuditWhatMax])
	}
	return what
}

// AuditOutcome is `ok`, `refused: <why>` or the error's first line, cut to
// AuditErrMax characters.
func AuditOutcome(err error) string {
	if err == nil {
		return "ok"
	}
	msg := strings.TrimSpace(err.Error())
	if i := strings.IndexAny(msg, "\r\n"); i >= 0 {
		msg = strings.TrimSpace(msg[:i])
	}
	var ref *RefusedError
	if errors.As(err, &ref) {
		msg = "refused: " + msg
	}
	if r := []rune(msg); len(r) > AuditErrMax {
		msg = string(r[:AuditErrMax])
	}
	return msg
}
