// Package ctl holds the vocabulary the control tools share, below the feature packages that use it.
package ctl

import (
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/link/wire"
)

// AgentHeader names the calling session. The room header is `RoomHeader`, shared
// with the board, so a control call scopes exactly the way a board request does.
//
// LANDMINE THAT PICKED THESE NAMES: Claude Code blanks any header whose name
// contains TOKEN, SECRET, KEY, PASSWORD or AUTH. `X-Atrium-Agent` and
// `X-Atrium-Room` avoid all five.
const AgentHeader = "X-Atrium-Agent"

// BoardError is a refusal from the board, with its status kept. The sentence
// is the whole of what a tool shows. The code is what the relay reads to tell
// a room that is not answering from a refusal that will never succeed.
type BoardError struct {
	Code int
	Msg  string
	// NoSentence is an answer with no sentence in it, which is what a route the room
	// does not have looks like. A room older than an endpoint answers a bare 404.
	NoSentence bool
}

func (e *BoardError) Error() string { return e.Msg }

// HTTPStatus is what the board answered, for a caller that reads a 404 as "no such thing".
func (e *BoardError) HTTPStatus() int { return e.Code }

// Bare is whether the answer carried no sentence, which is a route that is not there rather than a thing that is not.
func (e *BoardError) Bare() bool { return e.NoSentence }

// RefusedError is a call turned away by a rule rather than one that failed, such
// as the launch cap. The audit line says `refused:` for it, which is the line
// somebody looks for, and a plain error is written as it came.
type RefusedError struct{ Msg string }

func (e *RefusedError) Error() string { return e.Msg }

// AgentOf and RoomOf read the per-request identity headers the go-sdk hands a
// tool on `Extra`. Absent when the caller is not one of atrium's sessions,
// which is an ordinary way to use these tools and not an error.
func AgentOf(req *mcp.CallToolRequest) string {
	if req == nil || req.Extra == nil || req.Extra.Header == nil {
		return ""
	}
	return strings.TrimSpace(req.Extra.Header.Get(AgentHeader))
}

func RoomOf(req *mcp.CallToolRequest) string {
	if req == nil || req.Extra == nil || req.Extra.Header == nil {
		return ""
	}
	return strings.TrimSpace(req.Extra.Header.Get(wire.RoomHeader))
}

// Class is which set of control tools a caller gets.
type Class int

const (
	// ClassFull is every tool. The default, and what any doubt resolves to.
	ClassFull Class = iota
	// ClassWorker is the tools a worker uses to talk and to report.
	ClassWorker
	// ClassWorkerResident and ClassFullResident are those two with the done and blocked words of a card that stays open.
	ClassWorkerResident
	ClassFullResident
	// ClassWorkerItem is the worker set of an item: no say, no peers, no task and no report, and the done and blocked
	// words that name no launcher. See ItemTag.
	ClassWorkerItem
	// ClassWorkerReview is the worker set and atrium_review, for a card tagged ReviewTag.
	ClassWorkerReview
)
