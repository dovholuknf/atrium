package pulls

import (
	"encoding/json"
	"io"
	"time"
)

// ForkSpec is one claude call's command line. The daemon runs it, see pulls_fork.go.
type ForkSpec struct {
	Exe  string
	Args []string
	Dir  string
	Env  []string
	// Stdin is the prompt, too long for a command line.
	Stdin []byte
	// Timeout bounds the run.
	Timeout time.Duration
}

// Receipt is the part of `claude -p --output-format json` a run reads.
type Receipt struct {
	Subtype           string            `json:"subtype"`
	IsError           bool              `json:"is_error"`
	Result            string            `json:"result"`
	NumTurns          int               `json:"num_turns"`
	SessionID         string            `json:"session_id"`
	PermissionDenials []json.RawMessage `json:"permission_denials"`
	Usage             *struct {
		Input      int64 `json:"input_tokens"`
		CacheWrite int64 `json:"cache_creation_input_tokens"`
		CacheRead  int64 `json:"cache_read_input_tokens"`
		Output     int64 `json:"output_tokens"`
	} `json:"usage"`
}

// limitedWriter passes bytes through until a budget runs out, then discards
// the rest.
//
// Not an error on overflow: returning one makes the child's next write fail,
// which for a shell script means a broken pipe and a confusing exit status
// instead of the clear "printed too much" the caller wants to report. The
// caller checks how much arrived and decides.
type limitedWriter struct {
	w    io.Writer
	left int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.left <= 0 {
		return len(p), nil
	}
	take := p
	if len(take) > l.left {
		take = take[:l.left]
	}
	n, err := l.w.Write(take)
	l.left -= n
	if err != nil {
		return n, err
	}
	// The child is told everything landed, so it is not the one that finds out
	// about the limit.
	return len(p), nil
}
