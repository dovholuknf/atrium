package deployready

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// THE DEPLOY QUEUE: every landed commit that is not yet live, and which kind of deploy puts it live. Live is what each
// room and the hub report as their running commit, so a hub deploy that left a room behind shows the room's half still
// waiting. Read-only, like Check.

// Needs values of a QueueEntry.
const (
	NeedsHub  = "hub"
	NeedsRoom = "room"
	NeedsBoth = "both"
)

const (
	recSep  = "\x1e"
	unitSep = "\x1f"
)

// Live is one process's reported running commit. Commit is empty for one that does not say.
type Live struct {
	Name   string
	Commit string
}

// QueueEntry is one landed commit still waiting.
type QueueEntry struct {
	SHA     string `json:"sha"`
	Short   string `json:"short"`
	Subject string `json:"subject"`
	// Item is the backlog item the commit's changelog entry names. Empty when the commit has none.
	Item  string `json:"item,omitempty"`
	Needs string `json:"needs"`
	// Rooms are the rooms whose running commit lacks this one. Empty when the commit needs the hub only.
	Rooms []string `json:"rooms,omitempty"`
}

// Queue is the whole answer.
type Queue struct {
	Branch  string       `json:"branch"`
	Tip     string       `json:"tip,omitempty"`
	Hub     string       `json:"hub,omitempty"`
	Entries []QueueEntry `json:"entries"`
	// Unreported are the rooms that did not say a commit. Each is taken as exactly as far behind as the hub, so the
	// list over-states what it needs and never hides a commit.
	Unreported []string `json:"unreported,omitempty"`
	Error      string   `json:"error,omitempty"`
	Line       string   `json:"line"`
}

var changelogRe = regexp.MustCompile(`^changelog/[^/]+/\d{4}-\d{2}-\d{2}-(.+)\.md$`)

// itemOf is the item a changelog path names, or "".
func itemOf(paths []string) string {
	for _, p := range paths {
		p = path.Clean(strings.ReplaceAll(p, "\\", "/"))
		if m := changelogRe.FindStringSubmatch(p); m != nil {
			return m[1]
		}
	}
	return ""
}

// Queue lists the commits on the branch that are not in the hub's running commit, or not in a room's, and that
// change something the deploy ships. A commit that touches only hub-only code needs the hub. Any other shipped code
// needs both, because the room restarts on the same binary.
func (c *Checker) Queue(ctx context.Context, hub string, rooms []Live) Queue {
	q := Queue{Branch: c.Branch, Entries: []QueueEntry{}}
	fail := func(format string, a ...any) Queue {
		q.Error = fmt.Sprintf(format, a...)
		q.Line = "deploy queue unknown: " + q.Error
		return q
	}
	tip, err := c.rev(ctx, "refs/heads/"+c.Branch)
	if err != nil {
		return fail("could not read %s in %s: %v", c.Branch, c.Dir, err)
	}
	q.Tip = tip
	hub = strings.ToLower(strings.TrimSpace(hub))
	if !shaRe.MatchString(hub) {
		return fail("the hub does not report a commit (%q)", hub)
	}
	hubFull, err := c.rev(ctx, hub)
	if err != nil {
		return fail("the hub's commit %s is not in this checkout", short(hub))
	}
	q.Hub = hubFull

	missing := func(live string) ([]string, error) {
		out, err := c.Git.Git(ctx, c.Dir, "rev-list", "--no-merges", fmt.Sprintf("--max-count=%d", c.maxCommits()+1),
			live+".."+tip)
		if err != nil {
			return nil, err
		}
		return strings.Fields(out), nil
	}
	hubList, err := missing(hubFull)
	if err != nil {
		return fail("could not read the commits since %s: %v", short(hubFull), err)
	}
	behindHub := map[string]bool{}
	behindRoom := map[string][]string{}
	pending := map[string]bool{}
	for _, s := range hubList {
		behindHub[s], pending[s] = true, true
	}
	for _, r := range rooms {
		list := hubList
		full := ""
		if sha := strings.ToLower(strings.TrimSpace(r.Commit)); shaRe.MatchString(sha) {
			full, _ = c.rev(ctx, sha)
		}
		if full == "" {
			q.Unreported = append(q.Unreported, r.Name)
		} else if list, err = missing(full); err != nil {
			return fail("could not read the commits since %s: %v", short(full), err)
		}
		for _, s := range list {
			behindRoom[s] = append(behindRoom[s], r.Name)
			pending[s] = true
		}
	}
	if len(pending) > c.maxCommits() {
		return fail("more than the %d commits that are read are not live", c.maxCommits())
	}

	// One read of every pending commit's subject and paths, oldest first.
	var shas []string
	for s := range pending {
		shas = append(shas, s)
	}
	sort.Strings(shas)
	type info struct {
		subject string
		paths   []string
	}
	infos := map[string]info{}
	var order []string
	for len(shas) > 0 {
		n := min(len(shas), 100)
		args := append([]string{"log", "--no-walk", "--reverse", "--no-renames", "--name-only",
			"--format=%x1e%H%x1f%s"}, shas[:n]...)
		shas = shas[n:]
		out, err := c.Git.Git(ctx, c.Dir, args...)
		if err != nil {
			return fail("could not read the pending commits: %v", err)
		}
		for _, chunk := range strings.Split(out, recSep) {
			lines := strings.Split(strings.TrimSpace(chunk), "\n")
			sha, subj, ok := strings.Cut(strings.TrimSpace(lines[0]), unitSep)
			if !ok {
				continue
			}
			in := info{subject: subj}
			for _, l := range lines[1:] {
				if l = strings.TrimSpace(l); l != "" {
					in.paths = append(in.paths, l)
				}
			}
			infos[sha] = in
			order = append(order, sha)
		}
	}
	for _, sha := range order {
		in := infos[sha]
		var shipped, roomSide bool
		for _, p := range in.paths {
			if Needs(p) {
				shipped = true
				if RoomSide(p) {
					roomSide = true
				}
			}
		}
		if !shipped {
			continue
		}
		e := QueueEntry{SHA: sha, Short: short(sha), Subject: in.subject, Item: itemOf(in.paths)}
		hubWaits := behindHub[sha]
		if roomSide {
			e.Rooms = behindRoom[sha]
		}
		switch {
		case hubWaits && len(e.Rooms) > 0:
			e.Needs = NeedsBoth
		case hubWaits:
			e.Needs = NeedsHub
		case len(e.Rooms) > 0:
			e.Needs = NeedsRoom
		default:
			continue
		}
		q.Entries = append(q.Entries, e)
	}
	q.Line = queueLine(q)
	return q
}

func queueLine(q Queue) string {
	var hub, room, both int
	for _, e := range q.Entries {
		switch e.Needs {
		case NeedsHub:
			hub++
		case NeedsRoom:
			room++
		default:
			both++
		}
	}
	if len(q.Entries) == 0 {
		return fmt.Sprintf("deploy queue empty: nothing landed on %s that the hub (%s) and the rooms are missing",
			q.Branch, short(q.Hub))
	}
	return fmt.Sprintf("deploy queue: %d landed, %d hub, %d room, %d both", len(q.Entries), hub, room, both)
}

// Markdown is the queue as the table HANDOFF carries, one row per commit, oldest first.
func (q Queue) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", q.Line)
	if len(q.Unreported) > 0 {
		fmt.Fprintf(&b, "Rooms that did not report a commit, taken as behind: %s\n\n", strings.Join(q.Unreported, ", "))
	}
	if len(q.Entries) == 0 {
		return b.String()
	}
	b.WriteString("| sha | item | needs | subject |\n|---|---|---|---|\n")
	for _, e := range q.Entries {
		needs := e.Needs
		if len(e.Rooms) > 0 {
			needs += " (" + strings.Join(e.Rooms, ", ") + ")"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", e.Short, e.Item, needs, strings.ReplaceAll(e.Subject, "|", "/"))
	}
	return b.String()
}
