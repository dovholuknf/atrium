package cli

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// `atrium backlog import <dir>`: a one-shot refresh of the hub's backlog from the files under docs/backlog. THE FILES
// IN GIT STAY THE SOURCE OF TRUTH and the hub's copy is a mirror this refreshes, until the orchestrator says otherwise.
// It is idempotent: an item is upserted by its id, and one that has not moved changes nothing.
//
// Everything under <dir>/<dept>/*.md is an item unless it is one of the files that only look like one: QUEUE.md, the
// NIGHT-* and HANDOFF* notes, and the README, HISTORY and REVIEWER-NOTES files. The id is the file name, the title is
// the first heading, the status is the `Status:` line, the department is the folder and the whole file is the body.

// backlogFile is one item as the files say it.
type backlogFile struct {
	ID, Dept, Title, Body, Status string
	Path                          string
}

var (
	notAnItem   = regexp.MustCompile(`(?i)^(QUEUE|NIGHT-.*|HANDOFF.*|README.*|HISTORY.*|REVIEWER-NOTES.*)$`)
	statusLine  = regexp.MustCompile(`(?mi)^status:\s*(.*)$`)
	headingLine = regexp.MustCompile(`(?m)^#{1,6}\s+(.+?)\s*$`)
	deptFolder  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}$`)
)

// statusOf is the hub status a `Status:` line means. Its first word decides, and the rest is the file's own prose:
// held and parked are held, built is built, done and fixed are done, dropped and superseded are dropped, in progress
// is in-progress, and anything else (open, not started, designed) is open. No line is open.
func statusOf(line string) string {
	w := strings.ToLower(strings.TrimSpace(line))
	w = strings.TrimLeft(w, "*_` ")
	switch {
	case strings.HasPrefix(w, "in progress"), strings.HasPrefix(w, "in-progress"):
		return hubstore.ItemInProgress
	case w == "":
		return hubstore.ItemOpen
	}
	first := strings.FieldsFunc(w, func(r rune) bool { return !unicode.IsLetter(r) })
	if len(first) == 0 {
		return hubstore.ItemOpen
	}
	switch first[0] {
	case "held", "hold", "parked", "paused":
		return hubstore.ItemHeld
	case "built":
		return hubstore.ItemBuilt
	case "done", "fixed", "shipped", "merged", "closed":
		return hubstore.ItemDone
	case "dropped", "superseded", "wontfix", "obsolete":
		return hubstore.ItemDropped
	case "blocked":
		return hubstore.ItemBlocked
	}
	return hubstore.ItemOpen
}

// cleanBody is the file as the hub will hold it: LF line ends, no control characters but the newline and the tab,
// and no longer than the hub allows.
func cleanBody(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case unicode.IsControl(r), r == ' ', r == ' ', r >= '‪' && r <= '‮', r >= '⁦' && r <= '⁩':
			return ' '
		}
		return r
	}, s)
	if rs := []rune(s); len(rs) > hubstore.ItemBodyMax {
		const cut = "\n\n[cut here, the file in git has the rest]\n"
		s = string(rs[:hubstore.ItemBodyMax-len([]rune(cut))]) + cut
	}
	return s
}

// parseBacklogFile reads one markdown file as an item.
func parseBacklogFile(path, dept string) (backlogFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return backlogFile{}, err
	}
	body := cleanBody(string(raw))
	name := filepath.Base(path)
	id := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
	f := backlogFile{ID: id, Dept: dept, Body: body, Path: path, Status: hubstore.ItemOpen}
	if m := statusLine.FindStringSubmatch(body); m != nil {
		f.Status = statusOf(m[1])
	}
	title := id
	if m := headingLine.FindStringSubmatch(body); m != nil {
		title = m[1]
		// `# f-003. An inventory` is the id and then the words.
		if rest := regexp.MustCompile(`(?i)^`+regexp.QuoteMeta(id)+`[.:]\s+`).ReplaceAllString(title, ""); rest != "" {
			title = rest
		}
	}
	if rs := []rune(title); len(rs) > hubstore.ItemTitleMax {
		title = string(rs[:hubstore.ItemTitleMax-3]) + "..."
	}
	f.Title = title
	return f, nil
}

// readBacklogDir is every item under dir/<dept>/*.md, in a stable order, and the paths it left out as not items.
func readBacklogDir(dir string) (items []backlogFile, skipped []string, err error) {
	depts, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	for _, d := range depts {
		if !d.IsDir() || !deptFolder.MatchString(d.Name()) {
			continue
		}
		files, err := os.ReadDir(filepath.Join(dir, d.Name()))
		if err != nil {
			return nil, nil, err
		}
		for _, f := range files {
			p := filepath.Join(dir, d.Name(), f.Name())
			if f.IsDir() || !strings.EqualFold(filepath.Ext(f.Name()), ".md") {
				continue
			}
			if notAnItem.MatchString(strings.TrimSuffix(f.Name(), filepath.Ext(f.Name()))) {
				skipped = append(skipped, p)
				continue
			}
			it, err := parseBacklogFile(p, d.Name())
			if err != nil {
				return nil, nil, err
			}
			items = append(items, it)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Dept != items[j].Dept {
			return items[i].Dept < items[j].Dept
		}
		return items[i].ID < items[j].ID
	})
	return items, skipped, nil
}

func backlogImportCmd(board *string) *cobra.Command {
	var dry bool
	c := &cobra.Command{
		Use:   "import <dir>",
		Short: "Refresh the hub's backlog from docs/backlog, once",
		Long: "Reads <dir>/<dept>/*.md and upserts each by its id: the id is the file name, the title the first heading,\n" +
			"the status the `Status:` line, the department the folder and the body the whole file. QUEUE.md, NIGHT-*,\n" +
			"HANDOFF* and the README, HISTORY and REVIEWER-NOTES files are skipped. Safe to run again: an item that has\n" +
			"not moved is left alone. The files in git stay the source of truth. This only refreshes the hub's mirror.\n" +
			"Writes only from the machine the hub runs on, and --board-addr says which hub.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			items, skipped, err := readBacklogDir(args[0])
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if dry {
				for _, it := range items {
					fmt.Fprintf(w, "%-12s %-10s %-40s %s\n", it.Status, it.Dept, it.ID, it.Title)
				}
				fmt.Fprintf(w, "%d items, %d skipped, nothing sent\n", len(items), len(skipped))
				return nil
			}
			by, room := backlogFiler()
			if by == "" {
				by = "backlog-import"
			}
			count := map[string]int{}
			var failed []string
			for _, it := range items {
				var out struct {
					Result string `json:"result"`
				}
				err := backlogCall(*board, http.MethodPost, "/_hub/backlog", map[string]any{
					"id": it.ID, "dept": it.Dept, "title": it.Title, "body": it.Body, "status": it.Status,
					"upsert": true, "by": by, "room": room,
				}, &out)
				if err != nil {
					failed = append(failed, fmt.Sprintf("%s: %v", it.ID, err))
					continue
				}
				count[out.Result]++
			}
			fmt.Fprintf(w, "%d added, %d updated, %d unchanged, %d failed, %d skipped as not items\n",
				count["added"], count["updated"], count["unchanged"], len(failed), len(skipped))
			for _, f := range failed {
				fmt.Fprintln(w, "  failed "+f)
			}
			if len(failed) > 0 {
				return fmt.Errorf("%d items were not imported", len(failed))
			}
			return nil
		},
	}
	c.Flags().BoolVar(&dry, "dry-run", false, "read the files and say what would be sent, sending nothing")
	return c
}
