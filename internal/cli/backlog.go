package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// `atrium backlog` and `atrium reports`: the hub's backlog and director reports from a shell, over /_hub/backlog and
// /_hub/reports. Reads work from anywhere the hub's board address reaches, writes only from the hub's machine, as the
// routes say. See internal/link/backlog.go and docs/rnd/reports-channel-design.md.

// backlogCall is one request to the hub, answering the hub's own sentence on a refusal.
func backlogCall(board, method, path string, body, out any) error {
	addr := orDefault(board, defaultBoardAddr())
	var rdr io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, "http://"+addr+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("the hub is not answering at %s (is it running?): %w", addr, err)
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(res.Body).Decode(&e)
		switch {
		case res.StatusCode == http.StatusNotFound && e.Error == "":
			e.Error = "this hub has no backlog (its build predates it)"
		case e.Error == "":
			e.Error = res.Status
		}
		return fmt.Errorf("%s", e.Error)
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func boardFlag(c *cobra.Command, board *string) {
	c.PersistentFlags().StringVar(board, "board-addr", "",
		"where the running hub's board listens (default "+defaultBoardAddr()+")")
}

// backlogFiler is who a filing is recorded as, from the session's own environment when there is one.
func backlogFiler() (by, room string) {
	return strings.TrimSpace(os.Getenv("ATRIUM_AGENT_NAME")), strings.TrimSpace(os.Getenv("ATRIUM_ROOM"))
}

// stdinBody reads the body from stdin when it was given as a dash.
func stdinBody(cmd *cobra.Command, body string) (string, error) {
	if body != "-" {
		return body, nil
	}
	raw, err := io.ReadAll(cmd.InOrStdin())
	return string(raw), err
}

func backlogCmd() *cobra.Command {
	var board string
	c := &cobra.Command{
		Use:   "backlog",
		Short: "The hub's backlog: list, show, file, status, import",
		Long: "The backlog items every room reads and writes through the hub. Filing and changing a status work only\n" +
			"from the machine the hub runs on.",
	}
	boardFlag(c, &board)
	c.AddCommand(backlogListCmd(&board), backlogShowCmd(&board), backlogFileCmd(&board), backlogStatusCmd(&board),
		backlogImportCmd(&board))
	return c
}

func backlogListCmd(board *string) *cobra.Command {
	var dept, status string
	var all bool
	c := &cobra.Command{
		Use:   "list",
		Short: "The items, open ones unless --all or --status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			q := url.Values{}
			if dept != "" {
				q.Set("dept", dept)
			}
			if status != "" {
				q.Set("status", status)
			} else if !all {
				q.Set("open", "1")
			}
			var out struct {
				Items []hubstore.BacklogItem `json:"items"`
			}
			if err := backlogCall(*board, http.MethodGet, "/_hub/backlog?"+q.Encode(), nil, &out); err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if len(out.Items) == 0 {
				fmt.Fprintln(w, "no items")
			}
			for _, b := range out.Items {
				fmt.Fprintf(w, "%-12s %-10s %-6s %-40s %s\n", b.Status, b.Dept, b.Priority, b.ID, b.Title)
			}
			return nil
		},
	}
	c.Flags().StringVar(&dept, "dept", "", "only this department")
	c.Flags().StringVar(&status, "status", "", "only this status: open, held, in-progress, built, blocked, incomplete, done or dropped")
	c.Flags().BoolVar(&all, "all", false, "every status, not just the open ones")
	return c
}

func backlogShowCmd(board *string) *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "One item, with its body",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var b hubstore.BacklogItem
			if err := backlogCall(*board, http.MethodGet, "/_hub/backlog/"+url.PathEscape(args[0]), nil, &b); err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "%s  [%s]  %s\n", b.ID, b.Status, b.Title)
			fmt.Fprintf(w, "dept %s, priority %s, filed %s by %s on %s\n", b.Dept, orDefault(b.Priority, "-"), b.CreatedAt,
				orDefault(b.FiledBy, "?"), orDefault(b.FiledRoom, "?"))
			if b.Body != "" {
				fmt.Fprintf(w, "\n%s\n", b.Body)
			}
			return nil
		},
	}
}

func backlogFileCmd(board *string) *cobra.Command {
	var dept, title, body, priority string
	c := &cobra.Command{
		Use:   "file [id]",
		Short: "File an item, under an id you choose or the next one of its department",
		Long: "An id that is taken is refused. With no id the hub gives the next <prefix>-<n> of the department\n" +
			"(f, r, u, m, rnd, t, review). --body - reads the body from stdin.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text, err := stdinBody(cmd, body)
			if err != nil {
				return err
			}
			by, room := backlogFiler()
			id := ""
			if len(args) > 0 {
				id = args[0]
			}
			var b hubstore.BacklogItem
			err = backlogCall(*board, http.MethodPost, "/_hub/backlog", map[string]string{
				"id": id, "dept": dept, "title": title, "body": text, "priority": priority, "by": by, "room": room,
			}, &b)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "filed %s [%s] %s\n", b.ID, b.Status, b.Title)
			return nil
		},
	}
	c.Flags().StringVar(&dept, "dept", "", "the department that owns it, like fabric")
	c.Flags().StringVar(&title, "title", "", "one line")
	c.Flags().StringVar(&body, "body", "", "the words, or - for stdin")
	c.Flags().StringVar(&priority, "priority", "", "optional, like p2")
	_ = c.MarkFlagRequired("dept")
	_ = c.MarkFlagRequired("title")
	return c
}

func backlogStatusCmd(board *string) *cobra.Command {
	return &cobra.Command{
		Use:   "status <id> <open|held|in-progress|built|blocked|incomplete|done|dropped>",
		Short: "Change an item's status",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			by, _ := backlogFiler()
			var b hubstore.BacklogItem
			err := backlogCall(*board, http.MethodPost, "/_hub/backlog/"+url.PathEscape(args[0]),
				map[string]string{"status": args[1], "by": by}, &b)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s is now %s\n", b.ID, b.Status)
			return nil
		},
	}
}

func reportsCmd() *cobra.Command {
	var board string
	c := &cobra.Command{
		Use:   "reports",
		Short: "Director reports on the hub: list, read, add",
		Long: "Reports a director left for another director or the orchestrator, readable from every room. Adding and\n" +
			"marking one read work only from the machine the hub runs on.",
	}
	boardFlag(c, &board)
	c.AddCommand(reportsListCmd(&board), reportsReadCmd(&board), reportsAddCmd(&board))
	return c
}

func reportsListCmd(board *string) *cobra.Command {
	var to string
	var unread bool
	var limit int
	c := &cobra.Command{
		Use:   "list",
		Short: "The reports, newest first",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			q := url.Values{}
			if to != "" {
				q.Set("to", to)
			}
			if unread {
				q.Set("unread", "1")
			}
			if limit > 0 {
				q.Set("limit", fmt.Sprint(limit))
			}
			var out struct {
				Reports []hubstore.DirectorReport `json:"reports"`
			}
			if err := backlogCall(*board, http.MethodGet, "/_hub/reports?"+q.Encode(), nil, &out); err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if len(out.Reports) == 0 {
				fmt.Fprintln(w, "no reports")
			}
			for _, r := range out.Reports {
				state := "unread"
				if r.ReadAt != nil {
					state = "read"
				}
				fmt.Fprintf(w, "%-8s %-7s %-12s %-24s %s\n", r.ID, state, orDefault(r.ToDept, "-"), orDefault(r.FromBy, "?"), r.Subject)
			}
			return nil
		},
	}
	c.Flags().StringVar(&to, "to", "", "only the ones for this director")
	c.Flags().BoolVar(&unread, "unread", false, "only the unread ones")
	c.Flags().IntVar(&limit, "limit", 0, "at most this many")
	return c
}

func reportsReadCmd(board *string) *cobra.Command {
	var peek bool
	c := &cobra.Command{
		Use:   "read <id>",
		Short: "Show a report and mark it read",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := "/_hub/reports/" + url.PathEscape(args[0])
			var r hubstore.DirectorReport
			var err error
			if peek {
				err = backlogCall(*board, http.MethodGet, path, nil, &r)
			} else {
				by, _ := backlogFiler()
				err = backlogCall(*board, http.MethodPost, path, map[string]string{"do": "read", "by": by}, &r)
			}
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			fmt.Fprintf(w, "%s  to %s from %s on %s, %s\n%s\n", r.ID, orDefault(r.ToDept, "-"), orDefault(r.FromBy, "?"),
				orDefault(r.FromRoom, "?"), r.At, r.Subject)
			if strings.TrimSpace(r.Body) != "" {
				fmt.Fprintf(w, "\n%s\n", r.Body)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&peek, "peek", false, "show it without marking it read")
	return c
}

func reportsAddCmd(board *string) *cobra.Command {
	var to, subject, body string
	c := &cobra.Command{
		Use:   "add",
		Short: "Leave a report",
		Long:  "--to is the director it is for, and empty is the orchestrator. --body - reads the body from stdin.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			text, err := stdinBody(cmd, body)
			if err != nil {
				return err
			}
			by, room := backlogFiler()
			var r hubstore.DirectorReport
			err = backlogCall(*board, http.MethodPost, "/_hub/reports", map[string]string{
				"to": to, "subject": subject, "body": text, "by": by, "room": room,
			}, &r)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "left report %s\n", r.ID)
			return nil
		},
	}
	c.Flags().StringVar(&to, "to", "", "the director it is for, empty for the orchestrator")
	c.Flags().StringVar(&subject, "subject", "", "one line")
	c.Flags().StringVar(&body, "body", "", "the words, or - for stdin")
	_ = c.MarkFlagRequired("subject")
	return c
}
