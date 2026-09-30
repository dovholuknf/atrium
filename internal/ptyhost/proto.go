// Package ptyhost is the pty host of docs/rnd/rolling-restart-design.md: a small long-lived process that owns the
// pseudo terminals and the runners under them, so the daemon can restart without ending any of them. The wire
// protocol is written out in docs/terminal/ptyhost-protocol.md. This file is the frame shapes.
package ptyhost

// Proto is the protocol version this build speaks. A daemon that needs a newer one does not use this host for new
// launches.
const Proto = 1

// Verbs.
const (
	VProbe   = "probe"
	VHello   = "hello"
	VSpawn   = "spawn"
	VList    = "list"
	VAttach  = "attach"
	VWrite   = "write"
	VResize  = "resize"
	VSignal  = "signal"
	VCollect = "collect"
)

// Kinds of pty. Stored and reported, never acted on by the host.
const (
	KindRunner = "runner"
	KindShell  = "shell"
)

// Signals.
const (
	SigTerm = "term"
	SigKill = "kill"
)

// Cut says the terminal was cols x rows from absolute output offset Off onward. It is the shape the supervisor's
// sizeCut has, with an int64 offset.
type Cut struct {
	Off  int64 `json:"off"`
	Cols int   `json:"cols"`
	Rows int   `json:"rows"`
}

// Request is one frame from a client. Seq is echoed on the reply so a client can have several in flight.
type Request struct {
	V   string `json:"v"`
	Seq int64  `json:"seq,omitempty"`

	// hello
	Proto    int    `json:"proto,omitempty"`
	Build    string `json:"build,omitempty"`
	Takeover bool   `json:"takeover,omitempty"`

	// spawn
	ID   string   `json:"id,omitempty"`
	Kind string   `json:"kind,omitempty"`
	Argv []string `json:"argv,omitempty"`
	Env  []string `json:"env,omitempty"`
	Cwd  string   `json:"cwd,omitempty"`
	Ring int      `json:"ring,omitempty"`

	// spawn and resize
	Cols int `json:"cols,omitempty"`
	Rows int `json:"rows,omitempty"`

	// every verb after spawn
	RunID string `json:"run_id,omitempty"`

	// attach
	From int64 `json:"from,omitempty"`

	// write
	Data []byte `json:"data,omitempty"`

	// signal
	Sig string `json:"sig,omitempty"`
}

// PtyInfo is one pty as `list` and `attach` report it. Exited and ExitCode are never omitted: an exit code of 0 and
// a live runner must both be visible, and omitempty made them the same thing.
type PtyInfo struct {
	RunID   string `json:"run_id"`
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Pid     int    `json:"pid"`
	Cols    int    `json:"cols"`
	Rows    int    `json:"rows"`
	Started string `json:"started"`
	Exited  bool   `json:"exited"`
	// ExitedAt is when the runner ended (RFC3339Nano), empty while it lives. A daemon that was not connected when it
	// happened has no other way to say how long the runner lived, which decides whether it died starting.
	ExitedAt  string `json:"exited_at,omitempty"`
	ExitCode  int    `json:"exit_code"`
	RingStart int64  `json:"ring_start"`
	OutOffset int64  `json:"out_offset"`
}

// Reply answers a Request, matched by Seq. Only the fields of the verb are set.
type Reply struct {
	Seq int64  `json:"seq"`
	OK  bool   `json:"ok"`
	Err string `json:"err,omitempty"`

	// probe and hello
	Proto  int    `json:"proto,omitempty"`
	Build  string `json:"build,omitempty"`
	Pid    int    `json:"pid,omitempty"`
	Daemon bool   `json:"daemon,omitempty"`
	InJob  bool   `json:"in_job,omitempty"`
	Ptys   int    `json:"ptys,omitempty"`
	Took   bool   `json:"took_over,omitempty"`

	// spawn
	RunID string `json:"run_id,omitempty"`

	// list
	List []PtyInfo `json:"list,omitempty"`

	// attach: Info is the pty as of the snapshot, From the effective offset the replay starts at
	Info      *PtyInfo `json:"info,omitempty"`
	From      int64    `json:"from,omitempty"`
	Truncated bool     `json:"truncated,omitempty"`
	Cuts      []Cut    `json:"cuts,omitempty"`
	Data      []byte   `json:"data,omitempty"`

	// resize
	Cut *Cut `json:"cut,omitempty"`
}

// Event kinds pushed after an attach. They carry no seq.
const (
	EvOut  = "out"
	EvExit = "exit"
)

// Event is a pushed frame: live output at an absolute offset, or the exit. Exit never reaches a client before the
// last output bytes.
type Event struct {
	Ev       string `json:"ev"`
	RunID    string `json:"run_id"`
	Off      int64  `json:"off,omitempty"`
	Data     []byte `json:"data,omitempty"`
	Exited   bool   `json:"exited,omitempty"`
	ExitCode int    `json:"exit_code"`
}
