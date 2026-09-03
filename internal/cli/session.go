package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// A session opening and closing, reported to the board.
//
// SessionStart is what puts a card up before the session has done anything. A
// session sitting at its prompt has made no tool call, so the permission hook
// has nothing to report and the session is invisible. Asking the model to
// register itself would work and would cost a turn every time one opens.
//
// SessionEnd is the more valuable half: the only reliable signal that a session
// is over, which is what lets a card go dead on its own rather than sitting in
// running forever.
//
// Same rules as `atrium hook`: exit 0 whatever happens, one attempt, and a
// bound on everything. A session must never fail to start because atrium was
// not listening.

// sessionTimeout is longer than the activity budget. This fires twice per
// session rather than on every tool call, and a card that never appears is
// worse than a session start that took a moment.
const sessionTimeout = 3 * time.Second

// sessionInput is the part of Claude Code's payload this reads.
type sessionInput struct {
	CWD       string `json:"cwd"`
	SessionID string `json:"session_id"`
	Source    string `json:"source"`
	// TranscriptPath is where the runner is writing this conversation.
	//
	// It is what makes a session id worth storing. An id is only resumable
	// once there is a conversation behind it, and a session that opened and
	// was never typed into has none: `--resume` on that id answers "no
	// conversation found". Sent so the daemon can tell the two apart instead
	// of storing every id it is offered and losing the real one.
	TranscriptPath string `json:"transcript_path"`
}

func newSession() *cobra.Command {
	var event, name, hubURL string
	c := &cobra.Command{
		Use:   "session",
		Short: "Report that this session started or ended. Run by Claude Code, not by hand.",
		Long: "Posts one session event to the daemon and exits. Meant to be registered in " +
			"Claude Code's settings.json as SessionStart and SessionEnd.\n\n" +
			"It never fails a session: whatever goes wrong, it exits 0.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			reported := reportSession(hubURL, event, name)
			if interactive() {
				addr, source := hubAddressFrom(hubURL)
				fmt.Fprintf(cmd.OutOrStdout(),
					"posted %q to %s/session as %q, runner pid %d\naddress came from %s\n",
					event, addr, reported.agent, reported.pid, source)
			}
			return nil
		},
	}
	c.Flags().StringVar(&event, "event", "start", "start or end")
	c.Flags().StringVar(&name, "name", "", "what this session calls itself (default: the directory name)")
	c.Flags().StringVar(&hubURL, "url", "", "atrium agent address (default: $ATRIUM_HUB_URL or localhost:7777)")
	return c
}

// sessionReport is what was sent, for the interactive line. Nothing on the hook
// path reads it.
type sessionReport struct {
	agent string
	pid   int
}

func reportSession(hubURL, event, name string) sessionReport {
	var out sessionReport
	if strings.EqualFold(os.Getenv("ATRIUM_PERM_GATE"), "off") {
		return out
	}
	// `session-start` is what the board's hook list calls this, and somebody
	// reading that list will type it. Both spellings mean the same thing.
	switch strings.ToLower(strings.TrimSpace(event)) {
	case "start", "session-start":
		event = "start"
	case "end", "session-end":
		event = "end"
	default:
		return out
	}

	var in sessionInput
	if raw := readStdin(); len(raw) > 0 {
		_ = json.Unmarshal(raw, &in)
	}

	cwd := in.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	// A session atrium launched is told which card it belongs to. Anything
	// else identifies itself by directory, matching the permission hook. Both
	// have to agree or a session would appear twice under two names.
	agent := name
	if agent == "" {
		agent = os.Getenv("ATRIUM_AGENT_NAME")
	}
	if agent == "" && cwd != "" {
		agent = filepath.Base(cwd)
	}
	if agent == "" {
		return out
	}
	out.agent = agent
	out.pid = runnerPID()

	body, err := json.Marshal(map[string]any{
		"agent":   agent,
		"event":   event,
		"runner":  "claude",
		"cwd":     filepath.ToSlash(cwd),
		"pid":     out.pid,
		"task_id": os.Getenv("ATRIUM_TASK_ID"),
		"resume":  in.SessionID,
		"source":  in.Source,
		// Whether there is a conversation behind that id yet. Answered here
		// rather than in the daemon, because the file is on this machine and
		// the daemon may not be.
		"resumable": hasTranscript(in.TranscriptPath),
	})
	if err != nil {
		return out
	}

	client := &http.Client{Timeout: sessionTimeout}
	resp, err := client.Post(hubAddress(hubURL)+"/session", "application/json", bytes.NewReader(body))
	if err != nil {
		return out
	}
	resp.Body.Close()
	return out
}

// hasTranscript reports whether a conversation has actually been written.
//
// A session id is only resumable once there is something behind it. A session
// that opened and was never typed into has no transcript, and resuming its id
// answers "no conversation found". Storing such an id replaces a real
// conversation with one that can never work, and the thread is lost on the
// next restart.
//
// Unknown answers true. A runner that reports no transcript path is not making
// a claim either way, and storing whatever was offered is the right fallback
// there: a resume that might fail beats never recording one at all.
func hasTranscript(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return true
	}
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return fi.Size() > 0
}
