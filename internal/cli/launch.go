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

// Starting an atrium-managed agent from a script.
//
// The inversion that makes this worth having: atrium does not need to learn how
// to make a git worktree. Whatever already does that can make the directory the
// way it likes and then hand it over, and atrium supervises from there.
//
// `POST /v1/launch` was already the whole mechanism. This exists because a shell
// script should not have to build JSON and parse a response to use it.

func newLaunch() *cobra.Command {
	var (
		boardURL, harness, cwd, title, why, resume string
		quiet                                      bool
	)
	c := &cobra.Command{
		Use:   "launch",
		Short: "Start a runner under atrium, in a directory you have already prepared.",
		Long: "Starts a runner and puts it on the board, supervised, gated and attachable from the " +
			"browser. Atrium does not create the directory: make it however you like, then point " +
			"this at it.\n\n" +
			"Prints the new card's id, so a script can hold on to it.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return launchAgent(boardURL, harness, cwd, title, why, resume, quiet)
		},
	}
	c.Flags().StringVar(&harness, "runner", "claude", "which configured runner to start")
	c.Flags().StringVar(&cwd, "cwd", "", "where to run it (default: the current directory)")
	c.Flags().StringVar(&title, "title", "", "what to show on the card")
	c.Flags().StringVar(&why, "why", "", "what this is for, read back later")
	c.Flags().StringVar(&resume, "resume", "",
		"a session id to pick up instead of starting a new conversation")
	c.Flags().BoolVar(&quiet, "quiet", false, "print only the card id")
	c.Flags().StringVar(&boardURL, "url", "",
		"atrium board address (default: $ATRIUM_BOARD_URL or localhost:7778)")
	return c
}

func launchAgent(boardURL, harness, cwd, title, why, resume string, quiet bool) error {
	if strings.TrimSpace(cwd) == "" {
		got, err := os.Getwd()
		if err != nil {
			return err
		}
		cwd = got
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return err
	}
	// Checked here so the error names the directory the caller passed, rather
	// than coming back from the daemon about a path it resolved differently.
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", abs)
	}

	body, err := json.Marshal(map[string]string{
		"harness": harness,
		"cwd":     filepath.ToSlash(abs),
		"title":   title,
		"why":     why,
		"resume":  resume,
	})
	if err != nil {
		return err
	}

	url := boardAddress(boardURL) + "/v1/launch"
	// Long enough to cover the daemon waiting for the runner to prove it
	// started, which is two seconds plus whatever the runner takes to load.
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("no daemon answered at %s: %w", url, err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		var e struct{ Error string }
		if json.Unmarshal(raw, &e) == nil && e.Error != "" {
			return fmt.Errorf("atrium refused: %s", e.Error)
		}
		return fmt.Errorf("atrium refused: %s", resp.Status)
	}

	var task struct {
		ID           string `json:"id"`
		DisplayTitle string `json:"display_title"`
		PID          int    `json:"pid"`
	}
	if err := json.Unmarshal(raw, &task); err != nil {
		return fmt.Errorf("could not read the response: %w", err)
	}

	if quiet {
		fmt.Println(task.ID)
		return nil
	}
	fmt.Printf("started %s as %s\n", harness, task.DisplayTitle)
	if task.PID > 0 {
		fmt.Printf("  pid   %d\n", task.PID)
	}
	fmt.Printf("  card  %s\n", task.ID)
	fmt.Printf("  board %s\n", boardAddress(boardURL))
	return nil
}
