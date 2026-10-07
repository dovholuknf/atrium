package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

// Closing a card: the operator's word that the work on a link is over, which frees what the card's inventory lists.
// The room does the work (internal/api/close.go). This is its face in a terminal, and atrium_close is its face for an
// agent.
//
// "close", never "finish": `atrium finish` is an agent reporting its own work over.
//
// ONE PRESS SHOWS, A SECOND CLOSES. With no --yes the command prints what would go, what is kept and what it has to
// ask, and changes nothing. A worktree holding work nowhere else has needs an answer: --keep, --stash or --delete,
// each taking the worktree's seq or path, or `all`.

type closeOpts struct {
	boardURL              string
	yes, asJSON           bool
	keep, stash, deleteIt []string
}

func newClose() *cobra.Command {
	var o closeOpts
	c := &cobra.Command{
		Use:   "close <card>",
		Short: "Close a card: stop it, and free the worktree, branch, refs and review its inventory lists.",
		Long: "Shows what closing the card frees and keeps, and with --yes closes it.\n\n" +
			"The session is stopped, the review is archived (its findings and walk stay, the copy of the code " +
			"goes), and the worktree, its branch and the fetched PR ref are removed. The card goes to done and " +
			"stays in history. A re-paste of the link afterwards starts a fresh card and review.\n\n" +
			"A worktree with uncommitted changes, or commits on no remote and not on the hub, is asked about. " +
			"Say --keep (it stays on disk), --stash (pushed to the hub as stash/<card>/<branch>, then removed) " +
			"or --delete (removed as it is), with the worktree's seq or path, or all.",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return closeCardCmd(strings.TrimSpace(args[0]), o)
		},
	}
	c.Flags().BoolVarP(&o.yes, "yes", "y", false, "close it. without this only the preview is printed")
	c.Flags().StringSliceVar(&o.keep, "keep", nil, "keep this worktree on disk (seq, path or all)")
	c.Flags().StringSliceVar(&o.stash, "stash", nil, "push this worktree's work to the hub, then remove it (seq, path or all)")
	c.Flags().StringSliceVar(&o.deleteIt, "delete", nil, "remove this worktree with its work (seq, path or all)")
	c.Flags().BoolVar(&o.asJSON, "json", false, "print the answer as json")
	c.Flags().StringVar(&o.boardURL, "url", "",
		"atrium board address (default: $ATRIUM_BOARD_URL or localhost:7778)")
	return c
}

// closeItemOut, closeAskOut and closePreviewOut are the halves of the room's answer this reads.
type closeItemOut struct {
	Seq    int    `json:"seq"`
	Kind   string `json:"kind"`
	Ref    string `json:"ref"`
	Action string `json:"action"`
	Note   string `json:"note"`
	Bytes  int64  `json:"bytes"`
	Err    string `json:"error"`
}

type closeAskOut struct {
	Seq      int    `json:"seq"`
	Path     string `json:"path"`
	Branch   string `json:"branch"`
	Dirty    int    `json:"dirty"`
	Unpushed int    `json:"unpushed"`
}

type closePreviewOut struct {
	Card      string         `json:"card"`
	Items     []closeItemOut `json:"items"`
	Asks      []closeAskOut  `json:"asks"`
	Warnings  []string       `json:"warnings"`
	Kept      []string       `json:"kept"`
	DiskBytes int64          `json:"disk_bytes"`
	CanStash  bool           `json:"can_stash"`
}

type closeResultOut struct {
	Card    string         `json:"card"`
	Closed  bool           `json:"closed"`
	Items   []closeItemOut `json:"items"`
	Stashes []struct {
		Branch string `json:"branch"`
		Repo   string `json:"repo"`
	} `json:"stashes"`
	Left     int      `json:"left"`
	Warnings []string `json:"warnings"`
}

// closeAnswers matches the --keep, --stash and --delete flags to the asked worktrees. missing is every asked one with
// no answer, and an error is a flag that names no asked worktree or one named twice.
func closeAnswers(asks []closeAskOut, keep, stash, del []string) (answers map[string]string, missing []string, err error) {
	answers = map[string]string{}
	for _, f := range []struct {
		ans  string
		vals []string
	}{{"keep", keep}, {"stash", stash}, {"delete", del}} {
		for _, v := range f.vals {
			v = strings.TrimSpace(v)
			hit := false
			for _, a := range asks {
				if v == "all" || v == strconv.Itoa(a.Seq) || samePath(v, a.Path) {
					hit = true
					k := strconv.Itoa(a.Seq)
					if prev, ok := answers[k]; ok && prev != f.ans {
						return nil, nil, fmt.Errorf("%s is told both %s and %s", a.Path, prev, f.ans)
					}
					answers[k] = f.ans
				}
			}
			if !hit && v != "all" {
				return nil, nil, fmt.Errorf("--%s %s names no worktree the close asks about", f.ans, v)
			}
		}
	}
	for _, a := range asks {
		if answers[strconv.Itoa(a.Seq)] == "" {
			missing = append(missing, a.Path)
		}
	}
	return answers, missing, nil
}

func samePath(a, b string) bool {
	norm := func(p string) string { return strings.TrimRight(strings.ReplaceAll(p, `\`, "/"), "/") }
	return strings.EqualFold(norm(a), norm(b))
}

func closeCardCmd(card string, o closeOpts) error {
	board := boardAddress(o.boardURL)
	path := "/v1/tasks/" + url.PathEscape(card) + "/close"
	var pv closePreviewOut
	if _, err := closeAsk(board, http.MethodGet, path, nil, &pv); err != nil {
		return err
	}
	if !o.yes {
		if o.asJSON {
			return printJSON(pv)
		}
		printClosePreview(pv)
		fmt.Println()
		if len(pv.Asks) > 0 {
			fmt.Println("nothing was changed. run again with --yes and --keep, --stash or --delete for each worktree above")
		} else {
			fmt.Println("nothing was changed. run again with --yes to close it")
		}
		return nil
	}
	answers, missing, err := closeAnswers(pv.Asks, o.keep, o.stash, o.deleteIt)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		printClosePreview(pv)
		return fmt.Errorf("say --keep, --stash or --delete for %s", strings.Join(missing, ", "))
	}
	var res closeResultOut
	if _, err := closeAsk(board, http.MethodPost, path, map[string]any{"confirm": true, "answers": answers}, &res); err != nil {
		return err
	}
	if o.asJSON {
		return printJSON(res)
	}
	fmt.Printf("closed %s\n", res.Card)
	for _, s := range res.Stashes {
		fmt.Printf("  stashed on the hub: %s %s\n", s.Repo, s.Branch)
	}
	for _, w := range res.Warnings {
		fmt.Printf("  note: %s\n", w)
	}
	if res.Left > 0 {
		fmt.Printf("  %d left on the card:\n", res.Left)
		for _, it := range res.Items {
			if it.Action == "keep" {
				line := "    " + it.Kind + " " + it.Ref
				if it.Err != "" {
					line += ": " + it.Err
				}
				fmt.Println(line)
			}
		}
	}
	return nil
}

func printJSON(v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	os.Stdout.Write(raw)
	fmt.Println()
	return nil
}

func printClosePreview(pv closePreviewOut) {
	fmt.Printf("closing %s frees %s\n", pv.Card, humanBytes(pv.DiskBytes))
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, it := range pv.Items {
		if it.Action == "freed" {
			continue
		}
		note := it.Note
		if it.Err != "" {
			note = strings.TrimSpace(note + " (last try: " + it.Err + ")")
		}
		fmt.Fprintf(w, "  %d\t%s\t%s\t%s\t%s\n", it.Seq, it.Action, it.Kind, it.Ref, note)
	}
	w.Flush()
	if len(pv.Asks) > 0 {
		fmt.Println("asks, per worktree:")
		for _, a := range pv.Asks {
			fmt.Printf("  %d %s on %s: %d changed files, %d commits nowhere else\n", a.Seq, a.Path, a.Branch,
				a.Dirty, a.Unpushed)
		}
		if !pv.CanStash {
			fmt.Println("  this room cannot stash to a hub, so keep or delete")
		}
	}
	for _, s := range pv.Warnings {
		fmt.Printf("note: %s\n", s)
	}
	if len(pv.Kept) > 0 {
		fmt.Printf("kept: %s\n", strings.Join(pv.Kept, ", "))
	}
}

func humanBytes(n int64) string {
	switch {
	case n <= 0:
		return "nothing measured"
	case n < 1<<20:
		return fmt.Sprintf("%d KB", (n+1023)>>10)
	case n < 1<<30:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
}

// closeAsk is one call to the close verb. A refusal answers the room's sentence.
func closeAsk(board, method, path string, body any, out any) (int, error) {
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, board+path, rdr)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	// A stash pushes, and a big worktree takes a while to remove.
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Do(req)
	if err != nil {
		return 0, fmt.Errorf("no daemon answered at %s: %w", board, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		if resp.StatusCode == http.StatusNotFound && e.Error == "" {
			return resp.StatusCode, fmt.Errorf("the board at %s cannot close cards yet. it needs a newer build", board)
		}
		if e.Error != "" {
			return resp.StatusCode, fmt.Errorf("atrium refused: %s", e.Error)
		}
		return resp.StatusCode, fmt.Errorf("atrium refused: %s", resp.Status)
	}
	return resp.StatusCode, json.Unmarshal(raw, out)
}

// ── atrium_close ────────────────────────────────────────────────────────────

type CloseInput struct {
	Card    string            `json:"card" jsonschema:"the card to close, by id"`
	Confirm bool              `json:"confirm,omitempty" jsonschema:"false (the default) only answers what a close would do. true closes"`
	Answers map[string]string `json:"answers,omitempty" jsonschema:"keep, stash or delete for each asked worktree, by its seq"`
}

const closeToolDesc = "Close a card the way the operator does: stop its session, archive its review and remove the " +
	"worktree, branch and fetched refs its inventory lists. The card goes to done and stays in history.\n\n" +
	"WITHOUT confirm it changes nothing and answers the preview: `items`, `warnings`, and `asks`, the " +
	"worktrees holding work that is nowhere else. Each ask needs an answer in `answers` by its seq: keep " +
	"(stays on disk), stash (pushed to the hub as stash/<card>/<branch>, then removed) or delete (the work " +
	"is lost). Never pick delete unless whoever you work for said so. Do not close a card you were not asked to."

func addCloseTool(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "atrium_close", Description: closeToolDesc}, closeHandler)
}

func closeHandler(ctx context.Context, _ *mcp.CallToolRequest, in CloseInput) (*mcp.CallToolResult, map[string]any, error) {
	out := map[string]any{}
	if strings.TrimSpace(in.Card) == "" {
		return nil, out, fmt.Errorf("say which card to close")
	}
	path := "/v1/tasks/" + url.PathEscape(strings.TrimSpace(in.Card)) + "/close"
	if !in.Confirm {
		err := askFor(ctx, 30*time.Second, http.MethodGet, path, nil, &out)
		return nil, out, err
	}
	err := askFor(ctx, 5*time.Minute, http.MethodPost, path, map[string]any{"confirm": true, "answers": in.Answers}, &out)
	return nil, out, err
}
