package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// A turn ending, reported to the board.
//
// This is Claude Code's Stop hook, and it is the only way to reach a session
// that is sitting idle. A message queued for a busy session rides its next
// tool call, because a busy session makes them constantly. An idle session
// makes none at all, which is exactly when you most want to reach it.
//
// It is also what makes the `needs input` column mean anything. Without it a
// session that finished its turn stays in `running`, indistinguishable from
// one halfway through a build, and the column that answers "which of these
// wants me" stays empty.
//
// Unlike every other atrium hook this one WRITES to stdout, because that is
// how a Stop hook says anything. Which makes it the one hook that can do real
// damage, and the reason it was left unwired for so long: a Stop hook that
// blocks tells the model to keep going, so a broken one is a session that will
// not stop. Three things hold that line, and none of them may be removed:
//
//   - Every failure prints `{"continue":true}` and exits 0. Unreachable
//     daemon, timeout, garbage response, unparseable stdin: all of them end
//     the turn normally. Silence or a non-zero exit is the failure mode that
//     hangs a session.
//   - `stop_hook_active` is honoured. Claude Code sets it when the turn is
//     already running because a Stop hook blocked it. Blocking again on that
//     pass is an infinite loop, so this refuses to.
//   - The daemon's answer is passed through only when it parses and only when
//     it is one of the two shapes a Stop hook may return. Anything else is
//     treated as nothing to say.

// turnTimeout is the budget. Longer than the activity hook, which runs before
// every tool call, and shorter than the session hook, which is allowed to be
// slow because it fires twice in a session's life. This one fires at the end
// of every turn and the human is sitting there waiting for the answer.
const turnTimeout = 2 * time.Second

// turnInput is the part of Claude Code's Stop payload this reads.
type turnInput struct {
	CWD       string `json:"cwd"`
	SessionID string `json:"session_id"`
	// StopHookActive is set when this turn is only still running because a
	// Stop hook blocked the last one. Blocking again would never terminate.
	StopHookActive bool `json:"stop_hook_active"`
	// TranscriptPath is where this conversation is being written, which is
	// what makes its id worth storing. See hasTranscript in session.go.
	TranscriptPath string `json:"transcript_path"`
}

// keepGoing is what a Stop hook says when it has nothing to say: NOTHING.
//
// Empty output with exit 0 is the documented way to let a turn end. This used
// to print `{"continue":true}`, which is not documented for a Stop hook and
// was inferred from the shape of the other hooks. It appeared to work, which
// is the problem with inferring: the failure mode of a Stop hook getting this
// wrong is a session that will not stop, and that is not a thing to be
// approximately right about.
//
// Written on every path that is not a deliberate block, including every error
// path.
const keepGoing = ``

func newTurn() *cobra.Command {
	var event, name, hubURL string
	c := &cobra.Command{
		Use:   "turn",
		Short: "Report that a turn ended. Run by Claude Code, not by hand.",
		Long: "Claude Code's Stop hook. Posts to the daemon and writes the answer to stdout, " +
			"which is how a queued message reaches a session that is sitting idle.\n\n" +
			"It never fails a session: whatever goes wrong, it prints a plain continue and exits 0.\n\n" +
			"Register it as a Stop hook in settings.json. The board does not offer to install this " +
			"one, because a Stop hook that blocks makes a session keep working and a wrong one is a " +
			"session that will not stop.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprint(cmd.OutOrStdout(), turnEnded(hubURL, event, name))
			return nil
		},
	}
	c.Flags().StringVar(&event, "event", "end", "end (the only event today)")
	c.Flags().StringVar(&name, "name", "", "what this session calls itself (default: the directory name)")
	c.Flags().StringVar(&hubURL, "url", "", "atrium agent address (default: $ATRIUM_HUB_URL or localhost:7777)")
	return c
}

// turnEnded posts the end of a turn and returns exactly what should go to
// stdout. It has no error return on purpose: there is no failure this may
// report, only one it must absorb.
func turnEnded(hubURL, event, name string) string {
	if strings.EqualFold(os.Getenv("ATRIUM_PERM_GATE"), "off") {
		return keepGoing
	}
	switch strings.ToLower(strings.TrimSpace(event)) {
	case "end", "stop", "turn-end":
	default:
		return keepGoing
	}

	var in turnInput
	if raw := readStdin(); len(raw) > 0 {
		// Unparseable stdin is not fatal for the other hooks, which still know
		// which event fired. Here it is: without the payload there is no way
		// to know whether this pass is already inside a blocked turn, and
		// guessing wrong is the loop.
		if err := json.Unmarshal(raw, &in); err != nil {
			return keepGoing
		}
	}
	// Already going round once. Whatever the daemon would say, saying it again
	// starts a turn that ends by asking for another turn.
	if in.StopHookActive {
		return keepGoing
	}

	cwd := in.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	// The same name the permission and session hooks use. All three have to
	// agree or one session appears on the board three times.
	agent := name
	if agent == "" {
		agent = os.Getenv("ATRIUM_AGENT_NAME")
	}
	if agent == "" && cwd != "" {
		agent = filepath.Base(cwd)
	}
	if agent == "" {
		return keepGoing
	}

	body, err := json.Marshal(map[string]any{
		"agent":     agent,
		"cwd":       filepath.ToSlash(cwd),
		"resume":    in.SessionID,
		"resumable": hasTranscript(in.TranscriptPath),
	})
	if err != nil {
		return keepGoing
	}

	client := &http.Client{Timeout: turnTimeout}
	resp, err := client.Post(hubAddress(hubURL)+"/stop", "application/json", bytes.NewReader(body))
	if err != nil {
		return keepGoing
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return keepGoing
	}
	return turnAnswer(resp.Body)
}

// turnAnswer reads the daemon's reply and returns what may safely be printed.
//
// Passed through rather than re-marshalled, so the daemon stays the one place
// that decides what a Stop hook says. Validated rather than trusted, because
// the thing being handed to Claude Code here is the instruction to keep
// working: a truncated read or a proxy's error page must not become one.
func turnAnswer(r io.Reader) string {
	raw, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil || len(raw) == 0 {
		return keepGoing
	}
	var out struct {
		Continue *bool  `json:"continue"`
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return keepGoing
	}
	// A block with nothing to say is not a block. It would stop the turn from
	// ending and give the model no reason it should not have.
	if out.Decision == "block" && strings.TrimSpace(out.Reason) != "" {
		return string(raw)
	}
	return keepGoing
}
