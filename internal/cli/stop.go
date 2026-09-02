package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// Stopping a daemon without killing it.
//
// The daemon owns a pseudo terminal per supervised runner, and closing one
// takes the attached process with it, so killing the daemon ends every runner
// at once. The wind-down gives each one ten seconds; this reaches it from
// somewhere other than the daemon's own terminal.

func newStop() *cobra.Command {
	var boardURL, token string
	c := &cobra.Command{
		Use:   "stop",
		Short: "Ask a running daemon to wind down.",
		Long: "Asks the daemon to shut down the way ctrl-c does: event streams released, supervised " +
			"runners given time to finish, listeners closed in order. Killing the process instead " +
			"ends every runner immediately.\n\n" +
			"Refused unless the request comes from this machine, or carries the token the daemon " +
			"was started with.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return stopDaemon(boardURL, token)
		},
	}
	c.Flags().StringVar(&boardURL, "url", "",
		"atrium board address (default: $ATRIUM_BOARD_URL or localhost:7778)")
	c.Flags().StringVar(&token, "token", "",
		"shutdown token, when the daemon was started with one (default: $ATRIUM_SHUTDOWN_TOKEN)")
	return c
}

func boardAddress(override string) string {
	if override != "" {
		return strings.TrimRight(override, "/")
	}
	if v := os.Getenv("ATRIUM_BOARD_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://localhost:7778"
}

func stopDaemon(boardURL, token string) error {
	if token == "" {
		token = os.Getenv("ATRIUM_SHUTDOWN_TOKEN")
	}
	url := boardAddress(boardURL) + "/v1/shutdown"

	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("X-Atrium-Token", token)
	}

	// The daemon answers before it winds down, so this does not wait out the
	// grace period.
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("no daemon answered at %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("the daemon refused: %s", msg)
	}

	fmt.Println("the daemon is winding down. supervised runners get ten seconds to finish.")
	return nil
}
