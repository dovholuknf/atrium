package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/daemon"
)

// ptyHostRestartMargin is what the call is given over the wrap-up wait: ending the runners, the new host and the reopen.
const ptyHostRestartMargin = 5 * time.Minute

// restartPtyHost asks this room to restart its pty host and reopen its cards, and waits for the answer. A zero wait
// takes the room's own setting.
func restartPtyHost(boardURL, token, why string, wait time.Duration, immediate bool) (daemon.PtyHostReport, error) {
	var rep daemon.PtyHostReport
	q := url.Values{"why": {why}}
	if wait > 0 {
		q.Set("wait", strconv.Itoa(int(wait/time.Second)))
	} else {
		wait = roomWrapWait(boardURL)
	}
	if immediate {
		q.Set("immediate", "1")
	}
	req, err := http.NewRequest(http.MethodPost, boardURL+"/v1/restart-pty-host?"+q.Encode(), nil)
	if err != nil {
		return rep, err
	}
	if token != "" {
		req.Header.Set("X-Atrium-Token", token)
	}
	res, err := (&http.Client{Timeout: wrapCallLimit(wait) + ptyHostRestartMargin}).Do(req)
	if err != nil {
		return rep, fmt.Errorf("no room answered at %s: %w", boardURL, err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return rep, fmt.Errorf("the room refused: %s", strings.TrimSpace(string(msg)))
	}
	return rep, json.NewDecoder(res.Body).Decode(&rep)
}

func newRestartPtyHost() *cobra.Command {
	var boardURL, token, why string
	var wait int
	var immediate bool
	c := &cobra.Command{
		Use:   "restart-pty-host",
		Short: "Restart this room's pty host and reopen its cards, with the room staying up.",
		Long: "Ends the runners the pty host holds, restarts the host from the installed binary, and reopens every " +
			"card that had a runner, so they pick up a new claude. The room, its hub link and the ssh route that " +
			"rides it are never stopped.\n\n" +
			"First each working session is asked to wrap up and gets a restart wake, as a room restart does, for " +
			"at most the room's restart_wrap_wait_s. --immediate skips that.\n\n" +
			"Refused unless the request comes from this machine, or carries the token the daemon was started with. " +
			"For a room on another machine, use the restart_pty_host tool of the control MCP.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if token == "" {
				token = os.Getenv("ATRIUM_SHUTDOWN_TOKEN")
			}
			rep, err := restartPtyHost(boardAddress(boardURL), token, why, time.Duration(wait)*time.Second, immediate)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "pty host restarted (pid %d, now %d): %d runner(s) ended, %d of %d card(s) "+
				"reopened, %d woken\n", rep.OldPid, rep.NewPid, rep.Ended, rep.Reopened, rep.Wanted, len(rep.Wrap.Woken))
			return nil
		},
	}
	c.Flags().StringVar(&boardURL, "url", "", "atrium board address (default: $ATRIUM_BOARD_URL or localhost:7778)")
	c.Flags().StringVar(&token, "token", "", "shutdown token, when the daemon was started with one (default: $ATRIUM_SHUTDOWN_TOKEN)")
	c.Flags().StringVar(&why, "why", "", "what this restart is for, told to the working sessions")
	c.Flags().IntVar(&wait, "wait", 0, "seconds to wait for working sessions to wrap up (default: the room's setting)")
	c.Flags().BoolVar(&immediate, "immediate", false, "skip the wrap-up and its wakes")
	return c
}
